---
title: "Deps: update outdated dependencies and add govulncheck visibility"
labels: [dependencies, low-priority, tech-debt]
---

## Summary
`go list -m -u all` shows the module graph is behind on several dependencies, for example:

```
github.com/golang-jwt/jwt/v5 v5.3.0 [v5.3.1]
github.com/go-chi/chi/v5 v5.2.4 [v5.3.1]
github.com/golang/protobuf v1.5.3 [v1.5.4] (deprecated)
```

None of these are flagged as urgent on their own, but `govulncheck` is not installed or run anywhere in this environment or in CI, so there is currently no visibility into whether any dependency in the graph carries a known CVE.

## Suggested fix
1. Install and run `govulncheck ./...` (`go install golang.org/x/vuln/cmd/govulncheck@latest`) and address anything it flags.
2. Bump the outdated direct/transitive dependencies where low-risk (patch/minor bumps).
3. Note `github.com/golang/protobuf` is upstream-deprecated in favor of `google.golang.org/protobuf` — check whether it's a direct dependency or only transitive, and plan a migration if direct.
4. Add `govulncheck ./...` as a CI step (can be bundled with the build/test/vet gate from the companion CI issue) so this has ongoing visibility instead of being a one-time check.

## Priority
Low — no known active exploit identified during this review, but worth scheduling.
