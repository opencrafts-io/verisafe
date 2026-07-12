---
title: "Fix: test suite fails to compile — mocks missing ListInstitutionConnections"
labels: [bug, tests, high-priority, tech-debt, resolved]
---

## Status
- [x] Mocks regenerated, `go build`/`go vet`/`go test` all pass
- [x] Landed on `fix/ci-build-test-gate` alongside the CI gate (issue #2)

## Summary
`go vet ./...` and `go test ./...` failed to compile in two packages:

```
internal/tokens/token_service_test.go:24:25: cannot use repo (variable of type *mockQuerier.MockQuerier) as repository.Querier value in argument to NewTokenService: *mockQuerier.MockQuerier does not implement repository.Querier (missing method ListInstitutionConnections)

internal/service/device_service_test.go:68:34: cannot use mockQuerier (variable of type *mockservice.MockQuerier) as repository.Querier value in argument to service.NewDeviceService: *mockservice.MockQuerier does not implement repository.Querier (missing method ListInstitutionConnections)
```

## Root cause
`repository.Querier` gained a `ListInstitutionConnections` method as part of the institution-connection feature (merged via PR #60), but the generated mocks in `internal/repository/mocks` and `internal/service/mocks` were never regenerated to match.

## Impact
Both `internal/tokens` and `internal/service` had zero working tests, not just reduced coverage, until this was fixed.

## Resolution
Regenerated both mocks from their recorded `mockgen` invocations (found in each file's header comment):
```
mockgen -package mockQuerier -destination internal/repository/mocks/mock_querier.go github.com/opencrafts-io/verisafe/internal/repository Querier
mockgen -source=../repository/querier.go -destination=mocks/mock_querier.go -package=mockservice
```

Regenerating the mocks unmasked two real, previously-unrun test failures in `internal/service/device_service_test.go` — `RegisterDevice`'s error paths weren't wrapped with `core.ErrInvalidInput`/`core.ErrInternal`, and one test case contradicted actual production behavior (empty timestamp defaulting to `now()`, which is what `auth_handler.go`'s login flow relies on). Both fixed in the same branch; see commit `fb5a3d6`.

`go build ./...`, `go vet ./...`, and `go test ./...` all pass clean.

## Priority
Resolved.
