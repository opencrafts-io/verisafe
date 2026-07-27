# Third-party OAuth: scopes, grants and token brokering

How Verisafe stores what a user granted at Google or Spotify, how another service obtains a usable token on
their behalf, and how a user grants more without signing in again.

For Verisafe's own login and JWT lifecycle see [AUTHENTICATION.md](AUTHENTICATION.md). For the design
rationale see [ADR 0008](adrs/0008-incremental-oauth-scopes-and-token-brokering.md).

## Concepts

**Capability, not scope.** Callers ask for `"calendar"`, never
`https://www.googleapis.com/auth/calendar`. `internal/providers` maps capabilities onto each provider's wire
scopes, so API contracts stay free of provider vocabulary and one capability can expand to several scopes.

| Provider | Capabilities |
|---|---|
| `google` | `identity`, `calendar`, `tasks` |
| `spotify` | `identity`, `playback`, `playlist`, `library` |
| `apple` | `identity` |

There is no raw-scope escape hatch. A caller needing something unmapped adds it to
`internal/providers/descriptors.go`, which keeps that file the single description of a provider.

**Presumed vs verified scopes.** No provider offers a "what did user X grant" API — scope is reported only in a
token response. Grants migrated from the old `socials` table were seeded from what logins *historically
requested*, and are marked presumed (`scopes_verified_at IS NULL`). The broker never denies on a
presumed-missing scope; it refreshes and lets the provider adjudicate, then re-checks. So a wrong presumption
costs one extra round trip, never a wrong refusal. Every migrated row has a NULL expiry, which counts as stale,
so the first broker call for an account verifies it automatically.

`scopes_verified: false` in a response means "this is our best understanding, not the provider's word". A caller
that gets a 403 from the provider despite it should simply call the broker again — the failed attempt will have
converted the grant.

Apple never reports scopes, so its grants stay presumed permanently. It exposes only `identity`, so nothing
depends on it.

## For other services

> Building a service against this? [SERVICE_INTEGRATION.md](SERVICE_INTEGRATION.md) is the step-by-step
> version — setup, a working client, error handling and testing. This section is the reference.

### Get a usable provider token

```http
POST /oauth/google/token
X-API-Key: vst_...
Content-Type: application/json

{ "account_id": "9f1c8b2e-...", "capabilities": ["calendar"] }
```

```json
{
  "provider": "google",
  "account_id": "9f1c8b2e-...",
  "access_token": "ya29...",
  "token_type": "Bearer",
  "expires_at": "2026-07-26T13:31:00Z",
  "expires_in": 3413,
  "granted_scopes": ["https://www.googleapis.com/auth/calendar", "..."],
  "scopes_verified": true,
  "refreshed": false,
  "from_cache": true
}
```

Verisafe refreshes against the provider when the stored token is stale. **Refresh tokens are never returned** —
a compromised caller loses access when the access token expires rather than retaining indefinite reach into the
user's account.

Requires a service token (`X-API-Key`) and `read:provider_token:any`. A human Bearer JWT is rejected even if it
holds the permission, so nobody can read another user's provider token from their own session.

### When the user has not granted it

```json
HTTP/1.1 403 Forbidden
{
  "error": "insufficient_scope",
  "provider": "google",
  "account_id": "9f1c8b2e-...",
  "missing_scopes": ["https://www.googleapis.com/auth/calendar"],
  "missing_capabilities": ["calendar"],
  "granted_capabilities": ["identity"],
  "authorization_url": "https://verisafe.opencrafts.io/oauth/google/authorize",
  "authorization_method": "POST",
  "authorization_body": { "capabilities": ["calendar"] }
}
```

`authorization_url` points at **Verisafe's** authorize endpoint, not at Google. It cannot be a directly openable
provider URL: starting an authorization requires the *user's* JWT, which a service-token holder does not have.
Relay this to your own client, which calls the endpoint with the user's token and opens the URL it returns.

These keys are a contract; a golden-body test pins them.

### Other error responses

| Condition | Status | `error` |
|---|---|---|
| Account has never connected this provider | 404 | `no_grant` |
| Provider rejected the refresh token (user revoked, password change, long idle) | 403 | `reauthorization_required` |
| Provider cannot refresh and the token has expired (Apple) | 409 | — |
| Provider 5xx, timeout, or rate limit | 503 | — |

A 503 leaves the grant untouched — a provider outage must never look like a revocation.

### Pre-flight

```http
GET /oauth/grants?account_id=9f1c8b2e-...
X-API-Key: vst_...
```

Returns the same grant views as `/oauth/scopes` below. Never includes tokens.

## For user-facing clients

### See what is connected

```http
GET /oauth/scopes
Authorization: Bearer <access_token>
```

```json
{
  "grants": [{
    "provider": "google",
    "granted_capabilities": ["identity", "calendar"],
    "granted_scopes": ["..."],
    "scopes_verified": true,
    "refresh_available": true,
    "supports_incremental": true,
    "connected_at": "2025-11-02T08:14:00Z",
    "revoked": false,
    "available_capabilities": ["calendar", "identity", "tasks"]
  }],
  "available_capabilities": {
    "google": ["calendar", "identity", "tasks"],
    "spotify": ["identity", "library", "playback", "playlist"],
    "apple": ["identity"]
  }
}
```

`available_capabilities` lets a settings screen render connect-this toggles generically — adding a provider
grows the list with no client change.

### Grant an additional capability

```http
POST /oauth/google/authorize
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "capabilities": ["calendar"],
  "platform": "web",
  "redirect_uri": "https://app.example.com/settings/integrations"
}
```

```json
{
  "authorization_url": "https://accounts.google.com/o/oauth2/auth?...",
  "state": "8f3...",
  "expires_at": "2026-07-26T12:10:00Z",
  "already_granted": false,
  "requested_scopes": ["https://www.googleapis.com/auth/calendar"]
}
```

Open `authorization_url`. When the user finishes, the provider redirects to
`GET /oauth/{provider}/callback`, which records the grant and returns them to your `redirect_uri` (or
`deep_link` on mobile) with:

```
?scope_upgrade=success&provider=google&granted=calendar
```

`scope_upgrade` is `success`, `denied` (with `reason=access_denied` or `reason=account_mismatch`), or `failed`.

A URL is returned rather than a redirect because this endpoint needs the caller's Bearer token, which a browser
navigation cannot supply and which mobile clients must hand to a Custom Tab or `ASWebAuthenticationSession`
themselves.

`already_granted: true` (with no URL) means the user has already granted everything asked for and the provider
has confirmed it — skip the round trip.

**The user's session is untouched.** No new token family, no device registration, no cookies. They return to
exactly the app state they left. This is why the flow does not reuse the login callback.

Apple returns 400: it does not support additive consent.

### Requirements

- `redirect_uri` (web) and `deep_link` (mobile) are **both** validated against `ALLOWED_REDIRECT_URIS`, with
  exact case-insensitive matching. The login flow does not currently validate `deep_link`; this flow does.
- State is a single-use opaque handle with a 10-minute TTL. Replaying a captured callback URL fails.
- Requires `manage:oauth_grant:own`, held by the default `user` role.

### Disconnect

```http
DELETE /oauth/google/grant
Authorization: Bearer <access_token>
```

Revokes the grant and destroys the stored credentials. Does not sign the user out.

## Operations

### Configuration

| Variable | Required | Purpose |
|---|---|---|
| `PROVIDER_TOKEN_ENC_KEYS` | **yes** | Versioned AES-256 keys, `1:<base64-32-bytes>[,2:...]`. Generate with `openssl rand -base64 32`. |
| `PROVIDER_TOKEN_ENC_ACTIVE_KEY` | if >1 key | Which version seals new writes. |
| `PROVIDER_TOKEN_REFRESH_SKEW_SECONDS` | no (120) | Treat a token expiring within this window as stale. |
| `PROVIDER_TOKEN_CACHE_TTL_SECONDS` | no (300) | Cap on brokered token caching. |
| `OAUTH_SCOPE_UPGRADE_STATE_TTL_SECONDS` | no (600) | Incremental authorization window. |
| `OAUTH_MINIMAL_LOGIN_SCOPES` | no (false) | Request only identity at sign-in. |
| `OAUTH_RECONCILE_ENABLED` | no (false) | Run the reconciliation worker. |
| `OAUTH_RECONCILE_RATE_PER_MINUTE` | no (60) | Reconciler budget. |

**The service will not start without `PROVIDER_TOKEN_ENC_KEYS`.** That is deliberate: silently storing
long-lived replayable credentials in plaintext because a variable was unset is worse than a failed deploy.

### Granting a service access to the broker

New permissions are auto-granted to `system` and `Administrator` only, so a bot does **not** inherit
`read:provider_token:any`. Assign the dedicated role to each consuming service account:

```http
GET /roles/assign/{service_account_id}/{oauth-token-broker-role-id}
```

Forgetting this is the most likely cause of a day-one 403. Granting `read:provider_token:any` to the whole
`bot` role instead would let every bot read every user's Google token — don't.

### Provider console setup

Register `https://<AUTH_ADDRESS>/oauth/{provider}/callback` as an additional authorized redirect URI for Google
and Spotify. This is additive and cannot break the existing login callback.

### Key rotation

Add the new key alongside the old and bump the active version:

```
PROVIDER_TOKEN_ENC_KEYS=1:<old>,2:<new>
PROVIDER_TOKEN_ENC_ACTIVE_KEY=2
```

Rows migrate as they are used — every write re-seals under the active key. Retire the old key only once nothing
still references it.

### Reconciliation

The worker converts presumed grants to verified ones for accounts nobody brokers a token for. It is off by
default, rate limited, and leader-elected across replicas. Once this reaches zero the transitional plaintext
columns can be dropped and the worker switched off permanently:

```sql
SELECT count(*) FROM oauth_grants WHERE scopes_verified_at IS NULL AND revoked_at IS NULL;
SELECT count(*) FROM oauth_grants WHERE refresh_token_plain IS NOT NULL;
```

It is **not** a token-warming job. Refresh is lazy by design — warming every user's token on a schedule would
generate tens of thousands of pointless provider calls a day to save a few hundred milliseconds on the one
request that needs a token.

## Troubleshooting

**Broker returns `insufficient_scope` for a user who definitely has calendar access.** Check `scopes_verified`.
If false, the first call converts the grant — call again. If it still fails, the user genuinely revoked the
scope in their provider account settings; `granted_scopes` will have shrunk to match.

**Every user suddenly needs re-authorization.** Check `revoked_reason` on `oauth_grants`. `invalid_grant` means
the provider rejected the refresh token. A provider outage records `refresh_failure_count` and leaves the grant
alone, so mass revocation should not be possible — if you see it, look for a credential change on the app
registration itself.

**Users who had calendar lose it after enabling `OAUTH_MINIMAL_LOGIN_SCOPES`.** Confirm
`include_granted_scopes=true` is reaching Google — without it a post-cutover login completes a narrower
authorization. Roll the flag back with a restart and investigate before re-enabling.

**Scope upgrade returns `reason=account_mismatch`.** The user signed into a different provider account than the
one linked. Intentional: it stops one provider account's data being grafted onto another user's record.
