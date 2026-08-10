# Authentication

This document describes Verisafe's OAuth2 login flow and JWT/refresh-token lifecycle: the `/auth/*`
endpoints in `internal/auth`, and token issuance/rotation/revocation in `internal/tokens`. See
[ADR 0001](adrs/0001-verisafe-authentication-and-token-strategy.md) for the original strategy
decision and [ADR 0004](adrs/0004-apple-client-secret-lifecycle.md) for the Apple-specific
operational caveat referenced below.

> **Third-party scopes are a separate concern.** What a user granted at Google or Spotify, how another
> service obtains a token on their behalf, and how a user grants more without signing in again are all
> covered in [OAUTH_SCOPES.md](OAUTH_SCOPES.md) — the `/oauth/*` endpoints. This document is only about
> signing in to Verisafe and the tokens Verisafe itself issues.

## Overview

Verisafe supports three OAuth2 providers via [goth](https://github.com/markbates/goth): **Google**,
**Spotify**, and **Apple**. Login is a single redirect-based flow shared by all three, with a
platform split at the end:

- **Web** clients get the issued access/refresh token pair set directly as `HttpOnly` cookies.
- **Mobile** clients get a one-time opaque code appended to a deep link, then exchange that code for
  the token pair via a separate endpoint — tokens never appear in a redirect URL.

Every login (regardless of provider or platform) upserts the account and social-connection rows,
registers the requesting device, and issues a fresh access/refresh token pair inside one database
transaction — see `CallbackHandler` in `internal/auth/auth_handler.go`.

## API Endpoints

### Start login
```http
GET /auth/{provider}?platform=web&redirect_uri=https://app.example.com/callback
```
`{provider}` is one of `google`, `spotify`, `apple`. Omit `platform` (or pass anything other than
`web`) for the mobile flow. `redirect_uri` is **required** for `platform=web` and must match an
entry in the configured `AllowedRedirectURIs` allowlist (`JWTConfig.AllowedRedirectURIs`) — an
unlisted URI is rejected with 400 before any redirect happens.

### OAuth callback
```http
GET /auth/{provider}/callback?state=...&code=...
```
Apple additionally posts here via `application/x-www-form-urlencoded` (form_post response mode).
Not called directly by clients — the provider redirects here after the user authorizes. On success:
web platform gets `Set-Cookie: access_token=...; refresh_token=...` and a redirect to
`redirect_uri`; mobile platform gets a redirect to `{deep_link}?code={opaque_code}`.

### Exchange mobile auth code
```http
POST /auth/token/exchange
Content-Type: application/json

{ "code": "<opaque code from the deep link>" }
```
Response:
```json
{
  "access_token": "...",
  "refresh_token": "...",
  "access_expires_at": "2026-07-12T15:00:00Z",
  "refresh_expires_at": "2026-08-11T15:00:00Z"
}
```
The code is single-use with a 60-second TTL (`authCodeTTL`, `internal/auth/auth_handler.go`) and is
deleted from the cache on first use.

### Refresh a token pair
```http
POST /auth/token/refresh
Content-Type: application/json

{ "refresh_token": "..." }
```
Rotates the refresh token (old one is invalidated, a new one is issued in the same family) and
returns a fresh access/refresh pair. See "Refresh token reuse detection" below for what happens to
an already-used or expired refresh token.

### Revoke a token
```http
POST /auth/token/revoke
Authorization: Bearer <access_token>
Content-Type: application/json

{ "refresh_token": "..." }
```
Requires authentication. Blocklists the presented access token for its remaining lifetime and, if a
`refresh_token` is also supplied, revokes its entire token family. `refresh_token` is optional —
omitting it revokes only the access token. Returns `204 No Content` on success. Refresh-family
revocation failure is logged but non-fatal — the response is still 204 as long as the access token
itself was blocklisted.

### Logout
```http
GET /auth/{provider}/logout
```
Requires authentication. Clears the goth/gothic OAuth session only — this is **not** the same as
token revocation above. A client should call both if it wants to fully end a session: logout to
clear the provider session, and revoke to invalidate the JWT/refresh token.

## Token lifecycle

Access tokens are short-lived JWTs (`JWTConfig.ExpireDelta` minutes); refresh tokens are long-lived
opaque random values (`JWTConfig.RefreshExpireDelta` days), stored hashed, grouped into a "family"
per login. See `internal/tokens/service.go`'s package doc comment for the full strategy.

**Refresh token reuse detection**: rotating a refresh token that's already been used (or was
revoked, or never existed) revokes the *entire* token family and returns "refresh token reuse
detected: please re-login" — including for a token that's simply expired. A client seeing this error
after a long period of inactivity should treat it as "please log in again," not necessarily as a
security incident.

## Security notes and known limitations

- **State encoding**: login state (platform, redirect URI, deep link, device name/token) is
  pipe-delimited and base64-encoded (`encodeState`/`decodeState`,
  `internal/auth/auth_handler.go`). There is currently no escaping — a device name or deep link
  containing a literal `|` would decode incorrectly. Low practical risk today (these values
  originate from the client's own app, not arbitrary user input), but worth knowing if a decode
  error ever shows up in logs for a specific client.
- **Redirect URI allowlist**: `isRedirectAllowed` does an exact, case-insensitive string match
  against `AllowedRedirectURIs` — no prefix or wildcard matching, so every valid redirect target
  must be listed verbatim.
- **Apple client secret expiry**: Apple requires a signed JWT client secret, regenerated on every
  server start, valid for up to 6 months (`appleClientSecretValidity`,
  `internal/auth/auth.go`). A server that runs unrestarted past that window will start failing Apple
  logins with no advance warning — see [ADR 0004](adrs/0004-apple-client-secret-lifecycle.md).

## Troubleshooting

- **Apple login suddenly fails, nothing else changed**: check how long the server process has been
  running against the 6-month Apple client secret window above.
- **"refresh token reuse detected" on a client that should still be logged in**: check whether the
  refresh token simply expired (see "Refresh token reuse detection" above) before assuming a replay
  attack or a client-side bug that's re-using stale tokens.
- **400 on `/auth/{provider}?platform=web`**: confirm `redirect_uri` is both present and listed
  exactly (including scheme and trailing slash) in `AllowedRedirectURIs`.

## Future enhancements

- Proactive Apple client secret rotation/alerting before the 6-month expiry, instead of relying on
  incidental server restarts (tracked in ADR 0004).
- Move the login state payload onto the opaque server-side handle the incremental-scope flow already
  uses (`internal/handlers/oauth_scope_state.go`), which removes the pipe-delimiting problem, the
  replayability, and the dependency on gothic's cookie session in one step. Larger blast radius than
  it looks — this is the path every login takes — so do it deliberately, not as a rider on another
  change.
