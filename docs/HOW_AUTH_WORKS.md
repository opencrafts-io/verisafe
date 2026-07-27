# How auth works

A walkthrough of authentication and authorization in Verisafe. Start here — it explains the whole
picture in plain language. Once you need exact request/response shapes, go to
[AUTHENTICATION.md](AUTHENTICATION.md), [OAUTH_SCOPES.md](OAUTH_SCOPES.md), [RBAC.md](RBAC.md) and
[SERVICE_TOKENS.md](SERVICE_TOKENS.md).

---

## The one thing to understand first

There are **two completely separate kinds of token** in this codebase, and almost every point of
confusion comes from mixing them up.

```
┌──────────────────────────────────┐   ┌──────────────────────────────────┐
│  Verisafe tokens                 │   │  Provider tokens                 │
│                                  │   │                                  │
│  "Who is this caller?"           │   │  "What may we do at Google       │
│                                  │   │   on this user's behalf?"        │
│                                  │   │                                  │
│  Issued BY Verisafe              │   │  Issued BY Google / Spotify      │
│  Given TO apps and services      │   │  Kept INSIDE Verisafe, never     │
│                                  │   │    handed out                    │
│                                  │   │                                  │
│  Tables: issued_tokens,          │   │  Table: oauth_grants             │
│          refresh_tokens          │   │                                  │
└──────────────────────────────────┘   └──────────────────────────────────┘
```

Google appears on both sides, which is what makes this confusing at first:

- At **sign-in**, Google tells us *who you are*. We then forget Google's token and issue our own.
- Later, Google is a **thing we want access to** (your calendar). That's a separate permission the
  user grants separately.

Signing in with Google does **not** mean we can read your calendar. Those are two different questions
with two different answers, stored in two different places.

---

## Part 1: Signing in

### The flow

```
  App                    Verisafe                  Google
   │                        │                        │
   │  GET /auth/google      │                        │
   │───────────────────────>│                        │
   │                        │                        │
   │   302 to Google ───────────────────────────────>│
   │                        │                        │
   │                        │      user logs in      │
   │                        │                        │
   │                        │<─── 302 with ?code ────│
   │                        │                        │
   │                        │  swap code for profile │
   │                        │───────────────────────>│
   │                        │<───────────────────────│
   │                        │
   │                        │  ┌──────────────────────────────┐
   │                        │  │ ONE DATABASE TRANSACTION:    │
   │                        │  │  1. find/create account      │
   │                        │  │  2. save social profile      │
   │                        │  │  3. save provider tokens     │
   │                        │  │  4. register the device      │
   │                        │  │  5. issue OUR token pair     │
   │                        │  └──────────────────────────────┘
   │                        │
   │<── cookies (web) or deep link with a code (mobile)
```

All five steps are in one transaction, so a login either fully happens or doesn't happen at all. You
never end up with an account row but no tokens.

Code: `LoginHandler` and `CallbackHandler` in `internal/auth/auth_handler.go`.

### Web vs mobile

The last step differs by platform:

**Web** — the tokens are set as cookies and the browser is redirected back to your app:

```go
http.SetCookie(w, &http.Cookie{
    Name:     "access_token",
    Value:    pair.AccessToken,
    HttpOnly: true,                    // JavaScript cannot read it
    Secure:   true,                    // HTTPS only
    SameSite: http.SameSiteStrictMode, // not sent from other sites
})
```

**Mobile** — cookies don't work well in a mobile browser handoff, and putting tokens in a URL is
unsafe (URLs land in logs and browser history). So we hand over a **one-time code** instead:

```
Verisafe stores the tokens in Redis for 60 seconds under a random code,
then redirects to:  myapp://auth/callback?code=8f3a...

The app then swaps that code for the real tokens:

    POST /auth/token/exchange
    { "code": "8f3a..." }

    → { "access_token": "...", "refresh_token": "...", ... }
```

The code works exactly once and expires after 60 seconds.

---

## Part 2: The two tokens you get

### Access token — short-lived, proves who you are

A JWT, signed with a shared secret. Here's literally how it's built
(`internal/tokens/token_service.go`):

```go
claims := jwt.RegisteredClaims{
    ID:        jti.String(),       // unique id for THIS token
    Subject:   userID.String(),    // which account
    Issuer:    "https://verisafe.opencrafts.io/",
    Audience:  []string{"https://academia.opencrafts.io/"},
    IssuedAt:  jwt.NewNumericDate(time.Now()),
    ExpiresAt: jwt.NewNumericDate(expiry),
}
token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
return token.SignedString([]byte(ts.config.JWTConfig.ApiSecret))
```

Notice what is **not** in there: no roles, no permissions, no email, no scopes. Just "this is account
X, and this token is valid until Y".

> **Why so empty?** Because the middleware already reads roles and permissions from the database on
> every request (see Part 3). Putting them in the token would save nothing and would go stale the
> moment someone's permissions changed.

Lifetime: `EXPIRE_DELTA` minutes. Short, typically minutes — because a JWT can't be un-issued.

### Refresh token — long-lived, used to get a new access token

Not a JWT. Just 32 random bytes. Two things matter:

**1. We store only a hash of it.** Same idea as passwords — if the database leaks, the stored value
is useless:

```go
rawRefreshToken, _ := generateOpaqueToken()  // 32 random bytes
tokenHash := hashToken(rawRefreshToken)      // SHA-256

// The hash goes in the database. The raw value goes to the user and
// is never stored anywhere.
```

**2. Every login starts a "family".** All refresh tokens descended from one login share a
`family_id`. This is what makes theft detectable — see below.

Lifetime: `REFRESH_EXPIRE_DELTA` days.

### Refreshing

```
POST /auth/token/refresh
{ "refresh_token": "abc..." }

→ { "access_token": "new...", "refresh_token": "new...", ... }
```

The old refresh token is **consumed** — it stops working. You always get a fresh pair back.

### Why families exist: catching a stolen token

Suppose an attacker steals a refresh token.

```
  Real user                     Attacker
      │                             │
      │                             │  uses stolen token
      │                             │─────────────────────> ✓ works, gets new pair
      │                             │
      │  uses their token           │
      │  (now already consumed)     │
      │──────────────────────────────────────────────────> ✗ already used!
      │
      │        Verisafe: "this token was already consumed — something is wrong"
      │        → revokes the ENTIRE family
      │        → both the user and the attacker are logged out
```

The alternative — silently ignoring reuse — would let the attacker keep rotating forever without
anyone noticing. Logging both parties out is deliberately noisy: the real user re-logs in, the
attacker can't.

```go
existing, err := ts.repo.ClaimRefreshToken(ctx, tokenHash)
if err != nil {
    if errors.Is(err, pgx.ErrNoRows) {
        // Already used, revoked, or never existed — kill the whole family.
        token, err := ts.repo.GetRefreshTokenByHash(ctx, tokenHash)
        if err == nil {
            _ = ts.RevokeFamily(ctx, token.FamilyID)
        }
        return nil, errors.New("refresh token reuse detected: please re-login")
    }
    ...
}
```

> **Heads up:** an *expired* token takes the same path and gives the same message. A user coming back
> after a long holiday sees "reuse detected" and it usually just means "log in again" — not a breach.

### Logging out

Two different things, often confused:

| Endpoint | What it does |
|---|---|
| `POST /auth/token/revoke` | Kills Verisafe's tokens. **This is the real logout.** |
| `GET /auth/{provider}/logout` | Only clears the Google/Spotify session cookie. Does **not** touch our tokens. |

A full sign-out calls both.

Access tokens are stateless, so revoking one means remembering to reject it. Its `jti` goes into
Redis for however long the token had left:

```go
key := fmt.Sprintf("blocklist:%s", jti.String())
return ts.cacher.Set(ctx, key, "revoked", remainingTTL)
```

Once the token would have expired anyway, the entry disappears on its own.

---

## Part 3: How a request gets authenticated

Every protected endpoint runs `middleware.IsAuthenticated` first. It accepts **two** kinds of caller.

```
  Incoming request
        │
        ├── Authorization: Bearer <jwt>   ──> a HUMAN
        │      • check signature + expiry
        │      • check the Redis blocklist
        │
        └── X-API-Key: vst_...            ──> a SERVICE (bot)
               • look up by hash
               • not revoked? not expired? under max uses?
               • IP allowed? user-agent matches?
               • account type must be `bot`
        │
        ▼
  Load roles + permissions from the database
        │
        ▼
  Put them on the request context
```

```go
switch {
case strings.HasPrefix(authHeader, "Bearer "):
    rawToken := strings.TrimPrefix(authHeader, "Bearer ")
    parsedClaims, err := tokenSvc.ValidateAccessToken(r.Context(), rawToken)
    ...

case apiKey != "":
    serviceToken, err := repo.GetServiceTokenByHash(r.Context(), tokens.HashToken(apiKey))
    ...
}

// Both paths end up here:
ctx = context.WithValue(ctx, AuthUserClaims, claims)
ctx = context.WithValue(ctx, AuthUserRoles, roles)
ctx = context.WithValue(ctx, AuthUserPerms, perms)
```

### Reading the result in a handler

Always use the helpers. A raw type assertion panics if the middleware didn't run:

```go
// Good
claims, ok := middleware.ClaimsFromContext(r.Context())
if !ok || claims == nil {
    return fmt.Errorf("%w: missing claims", core.ErrUnauthorized)
}
accountID, err := uuid.Parse(claims.Subject)   // who is calling

perms := middleware.PermissionsFromContext(r.Context())
isService := middleware.IsServiceToken(r.Context())

// Bad — panics when IsAuthenticated hasn't run
perms := r.Context().Value(middleware.AuthUserPerms).([]string)
```

---

## Part 4: Permissions

Permissions are plain strings shaped `verb:resource:scope`:

```
read:account:own      read your own account
read:account:any      read anybody's account
create:role           create a role (no scope — creation has no owner yet)
```

You attach them to a route:

```go
router.Handle("GET /socials/me",
    middleware.CreateStack(
        middleware.IsAuthenticated(sh.Cfg, sh.DB, sh.Cacher, sh.Logger),
        middleware.HasPermission([]string{"read:account:own"}),
    )(http.HandlerFunc(sh.GetAllUserSocials)),
)
```

Order matters — `IsAuthenticated` must come first, because `HasPermission` reads what it put on the
context.

The check itself is unforgiving on purpose: exact string match, and you need **all** listed
permissions, not any:

```go
for _, required := range permissions {
    if !slices.Contains(perms, required) {
        w.WriteHeader(http.StatusForbidden)
        ...
        return
    }
}
next.ServeHTTP(w, r)
```

### `own` vs `any`

The route requires the `:own` version. Handlers that let admins act on anyone check for `:any`
themselves:

```go
perms := middleware.PermissionsFromContext(r.Context())
isAdmin := slices.Contains(perms, "read:account:any")

if !isAdmin && targetID.String() != claims.Subject {
    return fmt.Errorf("%w: you can only read your own account", core.ErrForbidden)
}
```

### Adding a permission

Permissions live in the database, added by a migration. Two things trip people up:

1. A trigger auto-grants every new permission to `system` and `Administrator` — **and nobody else**.
2. If regular users need it, grant it to the `user` role explicitly. Every account has that role
   automatically (another trigger does it at signup), so no backfill is needed:

```sql
INSERT INTO permissions (name, description)
VALUES ('read:widget:own', 'Permission to read your own widgets')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p
WHERE r.name = 'user' AND p.name = 'read:widget:own'
ON CONFLICT DO NOTHING;
```

Forget the second block and you've locked every normal user out of your new endpoint.

---

## Part 5: Getting at Google on a user's behalf

Now the other half. Say the calendar service wants to read a user's Google Calendar.

> If you're actually building that service, [SERVICE_INTEGRATION.md](SERVICE_INTEGRATION.md) has the
> setup steps and a working client. This section explains the idea.

### Capabilities, not scopes

You ask for `"calendar"`, not `https://www.googleapis.com/auth/calendar`. The mapping lives in one
place, `internal/providers/descriptors.go`:

```go
Capabilities: map[Capability][]string{
    CapabilityIdentity: {googleScopeEmail, googleScopeProfile},
    CapabilityCalendar: {googleScopeCalendar},
    CapabilityTasks:    {googleScopeTasks},
},
```

This is also why adding Microsoft later is one block of config rather than a refactor — nothing else
in the codebase knows what a Google scope string looks like.

| Provider | Capabilities |
|---|---|
| `google` | `identity`, `calendar`, `tasks` |
| `spotify` | `identity`, `playback`, `playlist`, `library` |
| `apple` | `identity` |

### Asking for a token

```
POST /oauth/google/token
X-API-Key: vst_...
{ "account_id": "9f1c...", "capabilities": ["calendar"] }
```

Two possible answers:

**The user granted it:**

```json
{
  "access_token": "ya29...",
  "expires_at": "2026-07-27T13:31:00Z",
  "granted_scopes": ["https://www.googleapis.com/auth/calendar"],
  "scopes_verified": true,
  "refreshed": false
}
```

**They didn't:**

```json
HTTP/1.1 403
{
  "error": "insufficient_scope",
  "missing_capabilities": ["calendar"],
  "authorization_url": "https://verisafe.opencrafts.io/oauth/google/authorize",
  "authorization_method": "POST",
  "authorization_body": { "capabilities": ["calendar"] }
}
```

That second response is a set of instructions: relay it to your client, which asks the user to
authorize, then retry.

Note you get an **access token only**. The refresh token stays inside Verisafe. If your service is
ever compromised, the attacker loses access within the hour instead of holding permanent reach into
someone's Google account.

### Refresh happens automatically

Google access tokens die after about an hour. You never handle that:

```
broker receives a request
    │
    ├─ token still fresh?  ──> hand it over
    │
    └─ stale or unknown?   ──> refresh against Google
                                store the new one
                                hand it over
```

Refresh is **lazy** — only when someone actually asks. Refreshing everyone's tokens on a timer would
mean tens of thousands of pointless calls to Google per day to save a fraction of a second on the one
request that needed it.

### Asking the user for more access

```
  App                       Verisafe                   Google
   │                            │                         │
   │ POST /oauth/google/authorize                         │
   │ Authorization: Bearer <user jwt>                     │
   │ { "capabilities": ["calendar"] }                     │
   │───────────────────────────>│                         │
   │<── { authorization_url } ──│                         │
   │                            │                         │
   │  open that URL ─────────────────────────────────────>│
   │                            │      user approves      │
   │                            │<── GET /oauth/google/callback
   │                            │                         │
   │                            │  save what was granted  │
   │                            │                         │
   │<─ back to your app: ?scope_upgrade=success&granted=calendar
```

**The user's login is untouched by this.** No new session, no new device, no new tokens. They were
signed in before and they're signed in after — they just granted one extra thing. That's why it's a
separate endpoint from the login callback, which *does* create a session.

Why a URL instead of a redirect? Because this endpoint needs the user's `Authorization` header, and a
browser navigating to a URL can't send one. Mobile apps also need the URL themselves to open it in a
Custom Tab.

### "verified" vs "presumed" scopes

You'll see `scopes_verified` in responses. Here's what it means.

No provider offers an API that answers "what did this user grant me?". You only learn scopes when you
exchange or refresh a token. So for accounts that existed before this system was built, we recorded
what logins *used to ask for* — an educated guess, marked **presumed**.

The rule this leads to:

> **Never refuse based on a guess.** If the grant is verified and a scope is missing, that's a real
> refusal. If it's only presumed, try the refresh anyway and let Google decide.

Worst case a wrong guess costs one extra round trip. It can never wrongly refuse someone who does
have access. And because those old records have no known expiry, the first real request refreshes
them — which is exactly when Google tells us the truth. The guesses fix themselves through normal
use.

---

## Common gotchas

**"reuse detected" on a user who did nothing wrong** — their refresh token probably just expired.
Same code path, same message. Check the age before assuming an attack.

**403 from a brand-new endpoint, for everyone** — you added a permission but didn't grant it to the
`user` role. See Part 4.

**403 from the broker for a service** — service accounts don't inherit new permissions. Assign the
`oauth-token-broker` role to that specific account: `GET /roles/assign/{account_id}/{role_id}`.

**Panic reading permissions** — you used `.([]string)` instead of
`middleware.PermissionsFromContext`.

**Logout "didn't work"** — you called the provider logout, which only clears Google's session. Call
`POST /auth/token/revoke`.

**Broker says `insufficient_scope` for a user who definitely has access** — check `scopes_verified`.
If false, call again; the first call converts the guess into fact.

---

## Where things live

| I want to... | Look at |
|---|---|
| Change the login flow | `internal/auth/auth_handler.go` |
| Change token issuing, refresh, revocation | `internal/tokens/token_service.go` |
| Change who can call an endpoint | that handler's `RegisterHandlers` |
| Change how requests are authenticated | `internal/middleware/auth_middleware.go` |
| Add an OAuth provider | `internal/providers/descriptors.go` |
| Change provider token storage or refresh | `internal/service/oauth_grant_service.go` |
| Change the token broker API | `internal/handlers/oauth_broker_handler.go` |
| Change the "grant more access" flow | `internal/handlers/oauth_scope_handler.go` |

## Next

- [SERVICE_INTEGRATION.md](SERVICE_INTEGRATION.md) — building a service that needs a Google or Spotify token
- [AUTHENTICATION.md](AUTHENTICATION.md) — exact endpoints and payloads for login and tokens
- [OAUTH_SCOPES.md](OAUTH_SCOPES.md) — the `/oauth/*` API and how to operate it
- [RBAC.md](RBAC.md) — the full permission list
- [SERVICE_TOKENS.md](SERVICE_TOKENS.md) — creating and managing service tokens
- [ADR 0001](adrs/0001-verisafe-authentication-and-token-strategy.md) — why tokens work this way
- [ADR 0008](adrs/0008-incremental-oauth-scopes-and-token-brokering.md) — why provider access works this way
