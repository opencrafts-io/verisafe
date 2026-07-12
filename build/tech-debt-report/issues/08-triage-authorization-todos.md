---
title: "Triage: unresolved authorization/verification TODOs in live handler code"
labels: [security, tech-debt]
---

## Summary
Two TODO comments mark known authorization/verification gaps in code paths that are already live:

1. `internal/handlers/institution_handler.go:76`
   ```go
   // TODO: (erick) Add fine permissions for both admin and the user in question
   ```
2. `internal/handlers/account_handler.go:704`
   ```go
   // TODO: implement verifying mechanisms
   ```

## Impact
Both are about missing authorization/verification logic, not cosmetic cleanup, and both sit in a package (`internal/handlers`) that currently has zero test coverage over these files (see the companion test-coverage issue). That combination — known-incomplete auth logic, unverified by any test — is worth resolving deliberately rather than leaving as inline comments that can silently persist indefinitely.

## Suggested fix
- Read the surrounding code for each TODO and determine the actual scope of the gap.
- Convert each into a tracked issue with explicit acceptance criteria (this issue can be split into two once triaged), or resolve directly if scope is small.
- Add a regression test alongside the fix so the specific permission/verification gap can't silently regress later.

## Priority
Medium — triage soon; actual fix priority depends on what each TODO turns out to guard.
