---
title: "Fix: geo_test.go fails by default — depends on a file path that doesn't exist"
labels: [bug, tests, tech-debt, resolved]
---

## Status
- [x] Fixed, landed on `fix/ci-build-test-gate` alongside issues #2 and #3

## Summary
`TestGeoIP_Lookup` in `internal/geo/geo_test.go` failed out of the box:

```
--- FAIL: TestGeoIP_Lookup (0.00s)
    geo_test.go:17:
        Error: Received unexpected error:
               open ../../database/mmdb/GeoLite2-City.mmdb: no such file or directory
```

## Root cause
The test opened the MaxMind `.mmdb` databases from a stale relative path (`../../database/mmdb/GeoLite2-City.mmdb`); the actual files live at `internal/geo/mmdb/` and are `go:embed`'d into the binary. `NewGeoIPLocater` already supports an embedded fallback (pass empty strings for both paths) — added in the same change that moved the databases to embed, but the test was never updated to use it.

## Impact
The test wasn't hermetic and failed on any checkout without that exact stale path present, which was the default state. A test that fails by default trains people to ignore `go test` output.

## Resolution
Changed `geo_test.go` to call `geo.NewGeoIPLocater("", "")`, using the embedded databases instead of a filesystem path. No environment dependency left. Updated the package doc comment in `geo.go`, which still showed the old file-path usage example.

## Priority
Resolved.
