---
title: "Refactor: extract shared JSON error-response helper to remove handler duplication"
labels: [refactor, tech-debt, in-progress]
---

## Status
- [x] Foundation + `internal/auth` migrated — see ADR 0003 and `refactor/auth-error-responses`
- [ ] Rest of `internal/handlers/*.go` (~121 remaining call sites) — not started

## Summary
The handlers package repeats the same inline error-response block dozens of times per file instead of using a shared helper:

```go
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]string{
    "error": "...",
})
return
```

Counted directly (via `grep -c`), this exact pattern appears **121 times** across `internal/handlers/*.go`:

| File | occurrences | file size |
|---|---|---|
| account_handler.go | 58 | 1,334 lines |
| role_handler.go | 26 | 538 lines |
| permission_handler.go | 22 | 523 lines |
| social_handler.go | 6 | 170 lines |
| activity_handler.go | 4 | 636 lines |
| institution_handler.go | 3 | 995 lines |
| streak_handler.go | 2 | 419 lines |

## Impact
- Directly inflates the size of the largest files in the codebase — `account_handler.go` alone is 1,334 lines, a large fraction of which is this repeated boilerplate.
- Error response shape is duplicated by hand instead of centralized, so it's easy for a future edit to make one endpoint's error response subtly inconsistent with the rest (different key name, missing field, wrong status code semantics).
- `internal/auth/auth_handler.go` had an additional, sharper version of this problem: three of its six handlers used plain-text `http.Error` while the other three already emitted JSON — see ADR 0003.

## What's done
Per ADR 0003 (`docs/adrs/0003-standardize-http-error-responses.md`), the helper now lives in `internal/core/http_error.go` (`core.WriteJSON`, `core.WriteError`, `core.HandleError`) rather than as unexported functions in `internal/handlers/app_handler.go` — `core` is importable from any package, including `internal/auth`, which the old location wasn't.

`internal/handlers/app_handler.go`'s `AppHandler.ServeHTTP` now delegates to `core.HandleError`. `internal/auth/auth_handler.go`'s six endpoints are fully migrated: the three already-JSON handlers needed no changes (inherited automatically), and the three plain-text handlers (`LoginHandler`, `CallbackHandler`, `LogoutHandler`) had their 14 `http.Error` call sites mechanically replaced with `core.WriteError`. Verified against a local run: all six now return consistent `application/json` error bodies.

## Suggested fix (remaining scope)
Sweep the rest of `internal/handlers/*.go` to use `core.WriteError`/`core.HandleError` instead of hand-rolled blocks — start with `account_handler.go`, `role_handler.go`, and `permission_handler.go` (over 80% of the remaining duplication is in those three files). This is a mechanical, low-risk refactor with an outsized readability payoff, and the helper it depends on already exists and is proven in production use via `internal/auth`.

## Priority
Medium — foundation and highest-risk package (`internal/auth`) done; the rest is a good candidate for a dedicated cleanup PR.
