# Verisafe — Technical Debt Report

**Date:** 2026-07-11
**Branch:** staging @ d677810
**Scope:** Go source under `internal/`, `main.go`, `database/`, CI workflows, git metadata.
**Method:** Automated scan (tech-debt-tracker scanner) + manual verification of every finding below via `go build`, `go vet`, `go test`, `git log`, and direct code reading. Raw scanner output is noisy for Go (false-positives on generated code, imports, seed SQL) — only findings confirmed by hand are reported here.

## Executive summary

The codebase itself (handler/service/repository layering, use of sqlc, mockgen, goose migrations) is reasonably conventional. The real risk isn't code style — it's **process debt**: nothing currently verifies that the code works before it ships, a private key is sitting in git history, and the test suite that does exist doesn't compile. Any one of these would be a quick fix; together they mean broken code can reach staging/production silently.

| Severity | Count | Theme |
|---|---|---|
| Critical | 2 | Secret committed to git; CI has no test/build gate |
| High | 3 | Test suite fails to compile; near-zero coverage on critical handlers; env-coupled test |
| Medium | 3 | 75MB of binaries in git history; duplicated error-handling boilerplate; two unresolved auth/permission TODOs |
| Low | 2 | Outdated dependencies; oversized seed migration |

---

## Critical

### 1. Private key committed to git (`keys/AuthKey_ZJ3TXALZ66.p8`)
`keys/` is listed in `.gitignore`, but `keys/AuthKey_ZJ3TXALZ66.p8` — an OpenSSH-format private key, named in Apple's `AuthKey_<KEYID>.p8` convention (Sign in with Apple / APNs) — is tracked in git and present in the current working tree (added before the ignore rule existed; the ignore rule doesn't retroactively remove it).

- **Impact:** anyone with repo access (or a leaked clone) has the signing key. If this key is live, it should be treated as compromised.
- **Action:** rotate the key with Apple immediately, then remove it from git history (`git filter-repo` or BFG — not just `git rm`, since it's still reachable from old commits), and confirm `keys/*.p8` is excluded going forward.

### 2. CI has no build/test/vet gate
`.github/workflows/build-push-tag.yml` runs on every push to `main` and `staging`: checkout → `docker build` → push → **update the deployment manifest and auto-deploy**. There is no `go build`, `go vet`, or `go test` step anywhere in `.github/workflows/`.

- **Impact:** the Dockerfile's multi-stage build only requires `go build` to succeed — it does not run tests — so a broken test suite (see Finding 3) can merge and auto-deploy to staging/production indefinitely without anyone noticing.
- **Action:** add a `go build ./...`, `go vet ./...`, and `go test ./...` step before the Docker build/push step; fail the pipeline on any non-zero exit.

---

## High

### 3. Test suite currently fails to compile
`go vet ./...` and `go test ./...` both fail today:

```
internal/tokens/token_service_test.go: cannot use repo (...*mockQuerier.MockQuerier) as repository.Querier value
    ... does not implement repository.Querier (missing method ListInstitutionConnections)
internal/service/device_service_test.go: same error, different mock package
```

The `repository.Querier` interface gained `ListInstitutionConnections` (institution-connection feature, merged via PR #60) but the hand/mockgen-generated mocks in `internal/repository/mocks` and `internal/service/mocks` were never regenerated. Both `internal/tokens` and `internal/service` package tests are **currently non-functional** — `go test ./...` reports `FAIL [build failed]` for both.

- **Action:** regenerate mocks (`go generate ./...` or the project's mockgen invocation) and commit. Add a CI check (Finding 2) so this class of drift fails the build next time instead of shipping silently.

### 4. Near-zero test coverage, concentrated on the least-tested being the highest-risk code
Only 4 of 65 hand-written `.go` files have a companion test: `geo_test.go`, `device_service_test.go`, `token_service_test.go`, and `ping_handler_test.go` (a 14-line health-check handler). The `internal/handlers` package — 6,131 lines, the majority of the HTTP surface — has exactly one tested file, and it's the trivial one.

Zero test coverage on:
- `account_handler.go` (1,334 lines) — account creation, deletion, **recovery**, bot accounts
- `service_token_handler.go` (1,157 lines) — service-to-service auth tokens
- `institution_handler.go` (995 lines) — institution connections/permissions
- `auth_handler.go` (758 lines) — OAuth login/callback flow
- `activity_handler.go`, `role_handler.go`, `permission_handler.go` (523–636 lines) — RBAC and permission assignment

This is the authentication, authorization, and account-lifecycle surface of the service, currently validated only by manual testing.

- **Action:** prioritize coverage for `auth_handler.go`, `account_handler.go`, and `permission_handler.go`/`role_handler.go` first — they're the highest blast-radius on a bug (auth bypass, wrong account deleted/recovered, privilege escalation).

### 5. `geo_test.go` depends on an environment file that isn't guaranteed present
```
TestGeoIP_Lookup: open ../../database/mmdb/GeoLite2-City.mmdb: no such file or directory
```
The test reads a real MaxMind database from a relative path rather than a fixture or interface mock, so `go test ./...` fails in any checkout that doesn't happen to have that file at that exact path (e.g. this checkout has the mmdb files under `internal/geo/mmdb/`, not `database/mmdb/`). The test is neither hermetic nor reliably reproducible across machines/CI.

- **Action:** either commit a small fixture reader-mock behind the same interface, or skip the test gracefully when the file is absent (`t.Skip`), or fix the path — but don't leave a test that fails by default.

---

## Medium

### 6. ~75MB of binary GeoIP databases committed to git
`internal/geo/mmdb/GeoLite2-City.mmdb` (63MB) and `GeoLite2-ASN.mmdb` (11MB) are tracked in git (introduced in `2e612d1 chore: use geo embed files over common file paths`). These are third-party MaxMind data files that update periodically — committing them means every clone pays ~75MB, and every future update adds another ~75MB to git history permanently (git never shrinks history without a rewrite).

- **Action:** move to Git LFS, or better, download/mount the mmdb files at build/deploy time (MaxMind's own distribution mechanism, or an init container) instead of vendoring them in source control.

### 7. Handler layer: large files, heavy duplication, no shared response helper
The scanner's duplicate-code detector is noisy on generated code and imports, but manual inspection of the highest-signal cluster confirms a real pattern: **121 near-identical inline error-response blocks** across the handlers package (`w.WriteHeader(...); json.NewEncoder(w).Encode(map[string]string{"error": ...}); return`), with no shared `respondError`/`respondJSON` helper reused consistently:

| File | inline error-response blocks | file size |
|---|---|---|
| account_handler.go | 58 | 1,334 lines |
| role_handler.go | 26 | 538 lines |
| permission_handler.go | 22 | 523 lines |
| social_handler.go | 6 | 170 lines |
| activity_handler.go | 4 | 636 lines |
| institution_handler.go | 3 | 995 lines |
| streak_handler.go | 2 | 419 lines |

This directly correlates with the oversized handler files (7 files over 500 lines; `account_handler.go` alone is 1,334) and is why the duplicate-code count from the raw scan is so large (5,929 raw hits — dominated by this pattern plus generated mocks/sqlc code, which are not real debt and were excluded here).

- **Action:** extract a small `respondJSON(w, status, payload)` / `respondError(w, status, msg)` helper (one already exists unused in `internal/handlers/app_handler.go` and `internal/geo/geo.go` — just not applied to the rest of the package) and sweep the handlers to use it. This alone would meaningfully shrink every oversized handler file and make error-response shape consistent across endpoints.

### 8. Two unresolved TODOs in authorization-sensitive code
- `internal/handlers/institution_handler.go:76` — `TODO: (erick) Add fine permissions for both admin and the user in question`
- `internal/handlers/account_handler.go:704` — `TODO: implement verifying mechanisms`

Both sit in code paths that are already live (institution connection endpoints, account handling) and both are about authorization/verification gaps rather than cosmetic work — worth triaging explicitly rather than leaving as inline comments, given they're in the same package that has zero test coverage (Finding 4).

---

## Low

### 9. Outdated dependencies
`go list -m -u all` shows the module graph is behind on several transitive dependencies (e.g. `golang-jwt/jwt/v5` v5.3.0 → v5.3.1, `go-chi/chi/v5` v5.2.4 → v5.3.1, `golang/protobuf` v1.5.3 → v1.5.4 and marked **deprecated** upstream). None of these are in the direct `require` block flagged as urgent, but `govulncheck` isn't installed/run anywhere in this environment or CI, so there's no visibility into whether any of these carry known CVEs.

- **Action:** run `govulncheck ./...` (install via `go install golang.org/x/vuln/cmd/govulncheck@latest`) and consider adding it to CI once Finding 2 is addressed.

### 10. 9,988-line seed migration
`database/migrations/20250910120000_seed_institutions.sql` is a single ~10k-line INSERT migration. Not urgent (it's data, not logic, and goose migrations are meant to be immutable once shipped), but worth splitting or generating from a CSV/script next time a seed dataset this size is needed, for reviewability.

---

## Suggested remediation order

1. Rotate the leaked Apple key and scrub it from git history (Critical #1) — do this first, independent of everything else.
2. Fix the stale mocks so `go test ./...` compiles again (High #3) — quick, unblocks everything else.
3. Add a `go build && go vet && go test` gate to CI before Docker build (Critical #2) — prevents regression of #3 and catches the next one.
4. Fix or skip the environment-coupled geo test (High #5).
5. Move GeoIP mmdb files out of git (Medium #6) — independent, can happen anytime.
6. Extract the error-response helper and sweep handlers (Medium #7) — biggest maintainability win per hour spent.
7. Backfill tests for `auth_handler.go` → `account_handler.go` → `permission_handler.go`/`role_handler.go`, in that order of blast radius (High #4).
8. Triage the two authorization TODOs (Medium #8) as real tickets, not comments.

---

## Appendix

- Raw automated scan output: `build/tech-debt-report/debt_inventory.json` (11,820 raw items — treat with caution; ~99% is generated-code noise: mockgen output, sqlc output, and the seed-data SQL file matched by generic long-line/duplicate-line/keyword heuristics not aware of Go idioms or generated-file markers).
- Scan excluded: `tmp/` (133MB of stale local build binaries from `air`, not tracked in git), `.git/`, `keys/`.
