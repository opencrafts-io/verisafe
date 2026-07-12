---
title: "Tests: add coverage for auth, account, and permission handlers (currently untested)"
labels: [tests, high-priority, tech-debt, in-progress]
---

## Status
- [x] `internal/auth` (was zero tests) — covered on `test/auth-coverage`, stacked on `refactor/auth-error-responses`
- [x] `internal/tokens` gaps (`RevokeFamily`, `RevokeAccessToken`, `IsAccessTokenRevoked`, `RevokeByRawToken`, `ValidateAccessToken`, `verisafe_claims.go`) — same branch
- [ ] `account_handler.go`, `permission_handler.go`/`role_handler.go`, `institution_handler.go`, `service_token_handler.go`, `activity_handler.go` — not started

## Summary
Only 4 of 65 hand-written `.go` files in this project have a companion test file: `geo_test.go`, `device_service_test.go`, `token_service_test.go`, and `ping_handler_test.go` (a 14-line health-check handler). The `internal/handlers` package — 6,131 lines, the bulk of the HTTP API surface — has exactly one tested file, and it's the trivial one.

## Untested, high blast-radius code
- ~~`internal/auth/auth_handler.go` (758 lines) — OAuth login/callback flow~~ — covered, see below
- `internal/handlers/account_handler.go` (1,334 lines) — account creation, deletion, **recovery**, bot accounts
- `internal/handlers/service_token_handler.go` (1,157 lines) — service-to-service auth tokens
- `internal/handlers/institution_handler.go` (995 lines) — institution connections/permissions
- `internal/handlers/activity_handler.go`, `role_handler.go`, `permission_handler.go` (523-636 lines) — RBAC and permission assignment

## Impact
A regression in any of these files (auth bypass, wrong account deleted or recovered, privilege escalation via a role/permission bug) would ship undetected, since nothing exercises these code paths automatically.

## What's done (internal/auth + internal/tokens)
- `internal/tokens/verisafe_claims_test.go` (new): `JTI`, `ValidateJWT` (valid, expired, missing expiry, wrong secret, non-HMAC signing method), `HashToken`.
- `internal/tokens/token_service_test.go`: added `RevokeFamily`, `RevokeAccessToken`, `IsAccessTokenRevoked`, `RevokeByRawToken`, `ValidateAccessToken`.
- `internal/auth`: `isRedirectAllowed`, `encodeState`/`decodeState` round-trip, `GenerateAppleClientSecret` (valid/malformed-PEM/non-ECDSA branches), `LoginHandler` end-to-end via `httptest` (including the full valid-login path using a dummy goth provider — no network access needed since goth's "begin auth" step builds the URL locally), `ExchangeAuthCodeHandler` (full coverage, it's the only DB-free `AppHandler`-wrapped auth endpoint).
- **Accepted, documented gap**: `RefreshTokenHandler`/`RevokeTokenHandler`'s DB-touching paths and `CallbackHandler` as a whole are not covered — they construct `repository.New(tx)` internally from a real transaction, so meaningful testing needs a real Postgres or brittle per-statement mocking of a mocked `pgx.Tx`. Only the pre-DB validation/auth-check branches of `RefreshTokenHandler`/`RevokeTokenHandler` are covered. This is a deliberate scope boundary, not an oversight — revisit if/when testcontainers or similar gets added to the repo.

## Suggested fix (remaining scope)
Add table-driven unit tests using the existing `mockservice`/`mockQuerier` pattern already used in `device_service_test.go` and now in `internal/auth`/`internal/tokens`. Suggested order, by blast radius:
1. `account_handler.go` — creation/deletion/recovery
2. `permission_handler.go` / `role_handler.go` — RBAC
3. `institution_handler.go`, `service_token_handler.go`, `activity_handler.go`

## Priority
High — `internal/auth`/`internal/tokens` done; the `internal/handlers` slice above is ongoing, not a single PR.
