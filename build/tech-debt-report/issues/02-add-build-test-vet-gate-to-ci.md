---
title: "CI: add go build/vet/test gate before Docker build and deploy"
labels: [ci, critical, tech-debt, resolved]
---

## Status
- [x] Fixed on `fix/ci-build-test-gate`, alongside issues #3 and #5 (required for the gate to be green from the first run)

## Summary
`.github/workflows/build-push-tag.yml` ran on every push to `main` and `staging`, going straight from checkout to `docker build` / `docker push` / auto-updating the deployment manifest in `infraops`. No `go build`, `go vet`, or `go test` step existed anywhere in `.github/workflows/`.

## Impact
Because the Dockerfile's build stage only needs `go build` to succeed, a broken test suite could merge to `staging` or `main` and auto-deploy unnoticed. This is what happened: the test suite failed to compile (issue #3) and nothing in CI caught it.

## Resolution
Added a `test` job to `build-push-tag.yml` that runs `go build ./...`, `go vet ./...`, and `go test ./...`, and gated the existing `build-push-update-infraops` job on it via `needs: test`. Landed together with the mock-compilation fix (issue #3) and the geo test fix (issue #5), since adding the gate on its own would have gone red immediately.

Not included here: `govulncheck` in CI (tracked separately — dependency-freshness issue).

## Priority
Resolved.
