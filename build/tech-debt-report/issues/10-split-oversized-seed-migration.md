---
title: "Cleanup: 9,988-line seed migration is hard to review"
labels: [low-priority, tech-debt]
---

## Summary
`database/migrations/20250910120000_seed_institutions.sql` is a single migration file containing roughly 9,988 lines, essentially one very large set of `INSERT` statements seeding institution data.

## Impact
Not urgent — this is data, not application logic, and goose migrations are meant to be immutable once shipped, so there's no correctness risk today. It is, however, effectively unreviewable as a diff, and any future change of this kind (a large one-off data seed) will hit the same problem.

## Suggested fix
No action needed on the existing migration. For future large seed datasets:
- Generate the migration from a source CSV/JSON file via a small script, so the reviewable artifact is the compact source data, not a 10k-line SQL diff.
- Or load large seed datasets via a data-loading step outside of the goose migration chain entirely (e.g. an idempotent seed script run post-deploy), reserving migrations for schema changes.

## Priority
Low — process note for next time, not a fix for existing code.
