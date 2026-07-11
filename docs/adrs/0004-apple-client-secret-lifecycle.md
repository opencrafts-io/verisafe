# 4. Apple client secret lifecycle

Date: 2026-07-12

## Status

proposed

## Context

Sign in with Apple does not accept a static client secret. Instead, `GenerateAppleClientSecret`
(`internal/auth/auth.go:234-272`) signs a fresh ES256 JWT on every server start, using the
`APPLE_PRIVATE_KEY_BASE64` / `APPLE_KEY_ID` / `APPLE_TEAM_ID` config values, with an expiry of
`180 * 24 * time.Hour` (Apple's maximum allowed lifetime is 6 months). The function's own doc
comment already warns it "should be called fresh on each server start" — but nothing enforces that:

- There is no scheduled rotation. If a server process runs longer than 6 months without a restart
  (a realistic scenario for a stable, low-deploy-frequency service), Apple begins rejecting the
  client secret and every Apple login starts failing with no advance warning.
- There is no alerting on the secret's remaining validity window.
- `internal/config` has no `Validate()` step at all. If `APPLE_PRIVATE_KEY_BASE64` is empty,
  malformed, or the wrong key type, the failure only surfaces the first time
  `GenerateAppleClientSecret` runs (at process start, inside `setupOAuthProviders`) with a
  PEM-decode or type-assertion error — not at config load, and with no indication of *which*
  environment variable is at fault.

This is a live operational risk today, independent of any code refactor: the current key was
recently rotated after a leak (see the git-history purge work), so the 6-month clock has effectively
restarted, but the underlying landmine — no rotation enforcement, no fail-fast validation — remains.

## Decision

Two changes, scoped to this pass:

1. **Fail-fast config validation**: add a `Validate()` step (`internal/config/config.go`) that checks
   `SessionSecret` is non-empty (currently only checked deep inside `setupSessionStore`) and, when
   `ApplePrivateKeyBase64` is set, that it decodes to a well-formed PEM/PKCS8 EC key —
   i.e. that `GenerateAppleClientSecret` would actually succeed — at startup, with an error naming
   the offending environment variable. This turns a first-Apple-login failure into a
   fails-to-boot failure, which is far easier to diagnose and impossible to miss in a deploy.
2. **Document the 6-month expiry operationally**: this ADR is itself the record of the risk. The
   constant is named and commented in code (see Phase 1 cleanup) and called out in
   `docs/AUTHENTICATION.md` as a runbook item, so it's discoverable without reading
   `auth.go` line by line.

Proactive rotation (e.g. a scheduled job that regenerates and hot-swaps the secret before expiry, or
alerting on remaining validity) is **not** implemented in this pass — it requires deciding where such
a job runs (in-process ticker vs. external scheduler) and how a hot-swapped secret propagates to
already-running replicas, which is a larger design question than this pass's scope. It's recorded
here as explicit future work rather than left undocumented.

## Consequences

**What becomes easier:**
- Misconfigured Apple credentials are caught at deploy time, not at the first user login attempt
  after a config change.
- Anyone investigating "why did Apple login suddenly break" has a single doc/ADR pointing at the
  known 6-month expiry mechanism instead of having to re-derive it from `auth.go`.

**What becomes harder or requires attention:**
- The 6-month expiry risk is now documented but **not eliminated** — a server that runs untouched for
  6+ months will still fail Apple logins. Whoever owns deploys needs to be aware restarts within
  that window (for unrelated reasons — deploys, dependency bumps) incidentally reset the clock, and
  that this is not something to rely on as a substitute for real rotation/alerting.
- Config validation adds a new startup failure mode: environments with genuinely incomplete Apple
  config (e.g. local dev without Apple credentials configured at all) need to either fully omit
  Apple config or provide a complete, valid set — a partially-configured state that previously failed
  silently/late now fails loudly/early.
