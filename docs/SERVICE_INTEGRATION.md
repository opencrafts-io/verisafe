# Integrating a service with the token broker

How another service — a calendar service, a music service — obtains a working Google or Spotify
access token for one of our users.

This is a build guide. For the endpoint reference see [OAUTH_SCOPES.md](OAUTH_SCOPES.md); for the
concepts see [HOW_AUTH_WORKS.md](HOW_AUTH_WORKS.md).

---

## What you're building

Your service never stores Google credentials. Each time it needs to call Google, it asks Verisafe for
a token:

```
your-svc ──── POST /oauth/google/token ────> Verisafe
              X-API-Key: vst_...                │
              { account_id, capabilities }      │ refreshes against Google
                                                │ if the stored one is stale
         <──── { access_token, expires_at } ────┘

your-svc ──── Authorization: Bearer ya29... ──> Google Calendar API
```

Verisafe owns expiry, refresh, encryption, and revocation. You own four response cases.

**You get an access token and nothing else.** Refresh tokens never leave Verisafe, so if your service
is ever compromised the attacker loses access within the hour instead of holding permanent reach into
a user's Google account.

---

## One-time setup

### 1. Create a bot account and service token

Needs an admin JWT. This creates both in one call:

```bash
curl -X POST https://verisafe.opencrafts.io/accounts/bot/create \
  -H "Authorization: Bearer <admin_jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "account": {
      "email": "calendar-svc@opencrafts.io",
      "name": "Calendar Service"
    },
    "service_token": {
      "name": "calendar-svc production",
      "description": "Brokers Google Calendar tokens on behalf of users",
      "expires_in_days": 365
    }
  }'
```

```json
{
  "account": { "id": "550e8400-...", "type": "bot", "...": "..." },
  "service_token": { "token": "vst_abc123...", "expires_at": "2027-07-27T00:00:00Z", "...": "..." }
}
```

Keep both: `account.id` for the next step, `service_token.token` for your secrets. **The raw token is
shown once and never again.** Losing it means rotating.

### 2. Grant it broker access

This is the step people skip, and it produces a 403 on the first real call.

```bash
# Find the role id
curl https://verisafe.opencrafts.io/roles \
  -H "Authorization: Bearer <admin_jwt>" \
  | jq '.[] | select(.name=="oauth-token-broker") | .id'

# Assign it to the bot account from step 1
curl "https://verisafe.opencrafts.io/roles/assign/<bot_account_id>/<role_id>" \
  -H "Authorization: Bearer <admin_jwt>"
```

New permissions auto-grant to `system` and `Administrator` only — deliberately **not** to `bot`.
Granting `read:provider_token:any` to the whole `bot` role would let every bot on the platform read
every user's Google token, so each consuming service is authorized individually.

### 3. Store the token in your secrets

That's the whole setup. Your service now holds one long-lived API key and no user credentials.

---

## Making the call

```bash
curl -X POST https://verisafe.opencrafts.io/oauth/google/token \
  -H "X-API-Key: vst_abc123..." \
  -H "Content-Type: application/json" \
  -d '{
    "account_id": "9f1c8b2e-0000-4000-8000-000000000001",
    "capabilities": ["calendar"]
  }'
```

Ask for a **capability** (`calendar`), never a raw scope URL. The mapping lives in
`internal/providers/descriptors.go`. If you need something unmapped, add it there — there is no
raw-scope escape hatch, which keeps that file the single description of a provider.

| Provider | Capabilities |
|---|---|
| `google` | `identity`, `calendar`, `tasks` |
| `spotify` | `identity`, `playback`, `playlist`, `library` |

Ask for the narrowest set you need. Requesting `["calendar","tasks"]` when you only use calendar
turns a working request into a consent prompt for half your users.

---

## The four responses

### 200 — you have a token

```json
{
  "provider": "google",
  "account_id": "9f1c8b2e-...",
  "access_token": "ya29...",
  "token_type": "Bearer",
  "expires_at": "2026-07-27T13:31:00Z",
  "expires_in": 3413,
  "granted_scopes": ["https://www.googleapis.com/auth/calendar"],
  "scopes_verified": true,
  "refreshed": false,
  "from_cache": true
}
```

Use it immediately. `refreshed` and `from_cache` are diagnostics, not something to branch on.

### 403 `insufficient_scope` — the user hasn't granted this

```json
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

Relay `authorization_url` to your own client and let it prompt the user.

**Your service cannot start the authorization itself.** That call needs the *user's* JWT, which you
don't have and shouldn't. Otherwise a service could kick off an OAuth consent flow for an arbitrary
user without them asking.

### 403 `reauthorization_required` / 404 `no_grant`

The user revoked us at Google (`reauthorization_required`), or never connected Google at all
(`no_grant`). Same remedy as above — send them through the authorize flow.

```json
{
  "error": "reauthorization_required",
  "reason": "invalid_grant",
  "provider": "google",
  "authorization_url": "https://verisafe.opencrafts.io/oauth/google/authorize",
  "authorization_method": "POST"
}
```

### 503 — the provider is having a bad minute

```json
{ "error": "an upstream dependency is temporarily unavailable" }
```

**Retry.** The grant is untouched — a provider outage is explicitly never treated as a revocation, so
don't disconnect anything on your side or prompt the user.

### Everything else

| Status | Means |
|---|---|
| 400 | Malformed body, bad `account_id`, or an unknown capability |
| 401 | Bad or missing `X-API-Key` |
| 403 without an `error` field | Your bot lacks the `oauth-token-broker` role — see setup step 2 |
| 404 unknown provider | Typo in the path, or the provider isn't registered |
| 409 | Provider can't refresh (Apple) and the stored token expired |

---

## A Go client

```go
package verisafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrNeedsAuthorization means the user must grant access before this will
// work. Retrying without user action will not help.
type ErrNeedsAuthorization struct {
	Provider            string
	MissingCapabilities []string
	AuthorizationURL    string
}

func (e *ErrNeedsAuthorization) Error() string {
	return fmt.Sprintf(
		"user must authorize %v at %s", e.MissingCapabilities, e.Provider,
	)
}

// ErrProviderDown is transient. Retry with backoff.
var ErrProviderDown = errors.New("oauth provider temporarily unavailable")

type TokenClient struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func NewTokenClient(baseURL, apiKey string) *TokenClient {
	return &TokenClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Token returns a provider access token that is valid right now and covers the
// requested capabilities.
func (c *TokenClient) Token(
	ctx context.Context,
	provider, accountID string,
	capabilities ...string,
) (string, error) {
	body, err := json.Marshal(map[string]any{
		"account_id":   accountID,
		"capabilities": capabilities,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.BaseURL+"/oauth/"+provider+"/token",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Network failure reaching Verisafe is the same class of problem as
		// Verisafe failing to reach Google: transient, retry.
		return "", fmt.Errorf("%w: %v", ErrProviderDown, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var ok struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&ok); err != nil {
			return "", fmt.Errorf("decode broker response: %w", err)
		}
		return ok.AccessToken, nil

	case http.StatusForbidden, http.StatusNotFound:
		var denied struct {
			Error               string   `json:"error"`
			MissingCapabilities []string `json:"missing_capabilities"`
			AuthorizationURL    string   `json:"authorization_url"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&denied)

		switch denied.Error {
		case "insufficient_scope", "reauthorization_required", "no_grant":
			missing := denied.MissingCapabilities
			if len(missing) == 0 {
				missing = capabilities
			}
			return "", &ErrNeedsAuthorization{
				Provider:            provider,
				MissingCapabilities: missing,
				AuthorizationURL:    denied.AuthorizationURL,
			}
		}
		// No error field means the rejection is about OUR credentials, not the
		// user's: wrong API key, or the oauth-token-broker role was never
		// assigned to this bot account.
		return "", fmt.Errorf(
			"broker rejected our credentials (%d) — is the oauth-token-broker role assigned?",
			resp.StatusCode,
		)

	case http.StatusServiceUnavailable:
		return "", ErrProviderDown

	default:
		return "", fmt.Errorf("unexpected broker status %d", resp.StatusCode)
	}
}
```

### Using it

```go
token, err := client.Token(ctx, "google", accountID, "calendar")

var needsAuth *verisafe.ErrNeedsAuthorization
switch {
case errors.As(err, &needsAuth):
	// Hand the URL to your own client so it can prompt the user.
	core.WriteJSON(w, http.StatusPreconditionRequired, map[string]any{
		"error":             "google_authorization_required",
		"capabilities":      needsAuth.MissingCapabilities,
		"authorization_url": needsAuth.AuthorizationURL,
	})
	return

case errors.Is(err, verisafe.ErrProviderDown):
	core.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":       "google_unavailable",
		"retry_after": 30,
	})
	return

case err != nil:
	return err
}

srv, err := calendar.NewService(ctx, option.WithTokenSource(
	oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
))
```

---

## Not writing Go?

The contract is small enough to implement anywhere.

```
POST {VERISAFE}/oauth/{provider}/token
  X-API-Key: {your service token}
  Content-Type: application/json
  { "account_id": "<uuid>", "capabilities": ["calendar"] }

200  → body.access_token          use it
403  → body.error in
         insufficient_scope
         reauthorization_required → relay body.authorization_url to your client
404  → body.error == "no_grant"  → same
503  → retry with backoff
```

Python:

```python
import requests

class NeedsAuthorization(Exception):
    def __init__(self, capabilities, authorization_url):
        self.capabilities = capabilities
        self.authorization_url = authorization_url

class ProviderDown(Exception):
    pass

def provider_token(base_url, api_key, provider, account_id, capabilities):
    resp = requests.post(
        f"{base_url}/oauth/{provider}/token",
        headers={"X-API-Key": api_key},
        json={"account_id": account_id, "capabilities": capabilities},
        timeout=10,
    )

    if resp.status_code == 200:
        return resp.json()["access_token"]

    if resp.status_code == 503:
        raise ProviderDown()

    if resp.status_code in (403, 404):
        body = resp.json()
        if body.get("error") in (
            "insufficient_scope", "reauthorization_required", "no_grant",
        ):
            raise NeedsAuthorization(
                body.get("missing_capabilities") or capabilities,
                body.get("authorization_url"),
            )
        raise RuntimeError(
            "broker rejected our credentials — is the "
            "oauth-token-broker role assigned?"
        )

    raise RuntimeError(f"unexpected broker status {resp.status_code}")
```

---

## The full loop when the user hasn't granted access

Four parties, but only one step is yours:

```
1. app       → your-svc       "show my calendar"
2. your-svc  → Verisafe       403 insufficient_scope + authorization_url
3. your-svc  → app            428 + authorization_url          ← you relay
4. app       → Verisafe       POST /oauth/google/authorize
                              Authorization: Bearer <USER's jwt>
             ← { authorization_url: "https://accounts.google.com/..." }
5. app opens it → user consents → Google redirects to Verisafe's callback
6. app       → your-svc       retry step 1
7. your-svc  → Verisafe       200, token
```

Step 4 uses the **user's** token, not yours. Your only job is step 3.

The user's login is untouched by any of this — no new session, no re-login, no logout. They grant one
extra thing and carry on.

---

## Rules of thumb

**Don't cache the access token.** Verisafe already caches it in Redis; `from_cache: true` tells you
when you got a cached one. Ask every time. Caching it yourself puts a Google credential in another
process for no benefit and risks serving one past revocation.

**Never persist it.** Not in a database, not in a session, not in a log line.

**Retry 503, don't retry 403.** A 403 will keep failing until the user acts; looping on it just
hammers Google's token endpoint through us.

**`scopes_verified: false` is not an error.** It means the scope list is Verisafe's reasonable
inference about a grant that predates scope recording, rather than something the provider confirmed.
Use the token normally. If the provider rejects it anyway, call the broker once more — that failed
attempt converts the record to verified, so the second call gives you a definitive answer.

**Pre-flight if you want to hide UI** rather than fail into it:

```bash
curl "https://verisafe.opencrafts.io/oauth/grants?account_id=9f1c8b2e-..." \
  -H "X-API-Key: vst_..."
```

Returns what's connected and what each grant covers. Issues no token.

---

## Testing your integration

Against a staging Verisafe:

1. **Wrong key** — expect 401. Confirms you're reading the header your deploy actually sets.
2. **Right key, role not yet assigned** — expect 403 with no `error` field. Do this deliberately once
   so you recognise it in production; it's the most common first-deploy failure.
3. **User who has not granted calendar** — expect 403 `insufficient_scope`. Check that your service
   surfaces `authorization_url` rather than swallowing it.
4. **User who has granted calendar** — expect 200, and `refreshed: true` on the first call for an
   account migrated from the old system.
5. **Same user immediately again** — expect `from_cache: true`.

---

## Troubleshooting

**403 with no `error` field, on every call.** The `oauth-token-broker` role isn't assigned to your bot
account. Setup step 2.

**401 on every call.** Wrong header (`X-API-Key`, not `Authorization`), or the token was revoked,
expired, or hit its `max_uses`. Service tokens can also carry an IP whitelist and a user-agent
pattern — check those if the key works locally but not from your cluster.

**403 `insufficient_scope` for a user you're sure has calendar access.** Look at `scopes_verified`. If
false, call again — the first call converts the inference into fact. If it's still 403 afterwards, the
user genuinely revoked that scope in their Google account settings, and `granted_capabilities` will
show what's actually left.

**"service tokens can only be used by bot accounts".** The account behind your token isn't
`type: bot`. Create it through `/accounts/bot/create` rather than converting a human account.

**Everything worked, then every user needs re-authorization at once.** Check `revoked_reason` on
`oauth_grants`. Mass `invalid_grant` usually means the OAuth app's client secret changed at the
provider. A provider outage cannot cause this — it records a failure count and leaves grants alone.

---

## See also

- [OAUTH_SCOPES.md](OAUTH_SCOPES.md) — endpoint reference and operations
- [HOW_AUTH_WORKS.md](HOW_AUTH_WORKS.md) — how the whole auth system fits together
- [SERVICE_TOKENS.md](SERVICE_TOKENS.md) — rotating, revoking, and restricting service tokens
- [BOT_ACCOUNT_CREATION.md](BOT_ACCOUNT_CREATION.md) — all bot account options
- [ADR 0008](adrs/0008-incremental-oauth-scopes-and-token-brokering.md) — why access is brokered rather than distributed
