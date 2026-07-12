---
title: "Repo hygiene: stop committing ~75MB of GeoIP binaries to git"
labels: [tech-debt, infra]
---

## Summary
`internal/geo/mmdb/GeoLite2-City.mmdb` (63MB) and `internal/geo/mmdb/GeoLite2-ASN.mmdb` (11MB) are tracked directly in git, introduced in commit `2e612d1` ("chore: use geo embed files over common file paths").

## Impact
These are third-party MaxMind data files that get updated periodically. Committing them as regular blobs means:
- Every clone of the repo pays ~75MB it doesn't need for source code.
- Every future update to these files adds another ~75MB to git history permanently — git never shrinks history without an explicit rewrite.
- The tech-debt scan of this repo was itself slowed to a crawl by these files being present in a naive recursive scan, which is a good proxy for how they affect any tooling that walks the repo.

## Suggested fix
Pick one:
- Move these files to Git LFS.
- Better: don't vendor them in source control at all — download/mount them at build or deploy time (MaxMind provides a direct download mechanism, or use an init container / startup step in the deployment).
- If neither is feasible short-term, at least document why they're embed-committed (the referenced commit message suggests this was a deliberate choice to avoid "common file paths" issues) so the tradeoff is visible to future readers.

## Priority
Medium — no urgency, but the repo will only get heavier the longer this waits.
