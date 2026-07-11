# 3. Standardize HTTP error responses

Date: 2026-07-12

## Status

accepted

## Context

Error responses across the HTTP surface are written in two incompatible ways today:

- `internal/handlers/app_handler.go` defines `AppHandler` — a `func(w, r) error` adapter whose
  `ServeHTTP` centralizes error-to-status mapping via `handleError`, which switches on the sentinel
  errors in `internal/core/errors.go` (`ErrInvalidInput` → 400, `ErrNotFound` → 404,
  `ErrUnauthorized` → 401, `ErrInternal`/default → 500) and writes a JSON body
  (`{"error": "..."}`) via unexported `writeJSON`/`writeError` helpers.
- Everywhere else — including three of the six handlers in `internal/auth/auth_handler.go`
  (`LoginHandler`, `CallbackHandler`, `LogoutHandler`) and the majority of `internal/handlers/*.go`
  (`account_handler.go`, `permission_handler.go`, `role_handler.go`, `social_handler.go`,
  `leaderboard_handler.go`, `activity_handler.go`, `institution_handler.go`, `streak_handler.go`,
  `service_token_handler.go`) — handlers write responses by hand: either
  `http.Error(w, message, status)` (plain text, `Content-Type: text/plain`) or inline
  `w.WriteHeader(status); json.NewEncoder(w).Encode(map[string]string{"error": message})`
  duplicated at well over 100 call sites.

Consequences already observed:
- `auth_handler.go`'s six endpoints return two different content types for errors depending on
  which of the six you hit, with no functional reason for the split — it's an artifact of when each
  handler was written, not a deliberate design.
- The `AppHandler`/`handleError` path is unreachable from `internal/auth` today, since `writeJSON`/
  `writeError`/`handleError` are unexported in package `handlers` — a different package can't reuse
  them even where the shape is identical.
- At least one existing handler (`institution_handler.go:130`) passes a raw JSON-looking string to
  `http.Error`, which still sets `Content-Type: text/plain` — an inconsistency that's easy to
  introduce precisely because there's no single call site enforcing the contract.

## Decision

Promote the error-response logic out of `internal/handlers` into `internal/core`, which already owns
the sentinel errors (`core.ErrInvalidInput`, etc.) and the typed response body (`core.APIError`) —
the natural single owner of "how a domain error becomes an HTTP response":

- `core.WriteJSON(w, status, body any)` — generic JSON response writer.
- `core.WriteError(w, status, message string)` — writes `core.APIError{Error: message}`.
- `core.HandleError(w, err error)` — the sentinel-to-status switch, moved verbatim from
  `handlers.handleError`.

`internal/handlers/app_handler.go`'s `AppHandler.ServeHTTP` delegates to `core.HandleError` instead
of a local copy. `internal/auth/auth_handler.go`'s three `AppHandler`-wrapped handlers
(`RefreshTokenHandler`, `RevokeTokenHandler`, `ExchangeAuthCodeHandler`) need no changes — they
inherit the shared behavior automatically. `LoginHandler`, `CallbackHandler`, and `LogoutHandler`
have their `http.Error(w, message, status)` call sites mechanically replaced with
`core.WriteError(w, status, message)` — same status codes, same messages, only the serialization
changes from plain text to JSON.

This is a deliberate wire-format change for those three handlers' **error** responses only (success
responses — redirects, cookies, JSON token pairs — are unaffected). It was evaluated against the
risk of breaking live clients and accepted: these are OAuth redirect endpoints hit by a browser or
mobile deep-link, not machine clients parsing error bodies as an API contract.

Rollout to the rest of `internal/handlers/*.go` (the ~100+ other duplicated call sites) is
explicitly out of scope for this decision — `internal/auth` is the first application of the
pattern; the remaining handlers are tracked as follow-up work.

## Consequences

**What becomes easier:**
- One place (`internal/core`) owns the mapping from domain error to HTTP response, reusable from
  any package, not just `internal/handlers`.
- New endpoints get consistent error responses for free by returning a wrapped sentinel error and
  going through `AppHandler`, instead of hand-writing a response.
- Removes the plain-text/JSON split in `auth_handler.go` — all six endpoints now behave identically
  under error conditions.

**What becomes harder or requires attention:**
- `LoginHandler`/`CallbackHandler`/`LogoutHandler` error bodies change shape for any caller that
  happens to parse them (accepted risk, see Context).
- The ~100+ remaining inline error-response blocks across `internal/handlers/*.go` are now visibly
  inconsistent with the new `internal/auth` pattern until they're migrated in a follow-up pass —
  this ADR does not resolve that debt, only stops it from growing in `internal/auth`.
