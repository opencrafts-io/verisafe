# Incremental OAuth scopes and provider token brokering

Date: 2026-07-26

## Status

Accepted

## Context

Verisafe's integration with Google, Spotify and Apple had three structural gaps that only became visible together.

**Provider tokens were write-only.** `socials.access_token` and `socials.refresh_token` were written on every
login and never read by anything. There was no `oauth2.Config`, no TokenSource, and no call to any provider's
token endpoint anywhere in the codebase. A Google access token is valid for roughly an hour, so within an hour
of a login the stored value was dead, and nothing existed to revive it. Any service wanting to act on a user's
behalf — reading their calendar, say — had no way to obtain a working token.

**Nothing recorded what a user had granted.** There was no scopes column, no scope claim on Verisafe's own JWT,
and no endpoint exposing grants. Neither a client nor a downstream service could answer "does this user have
calendar access", which meant the only way to find out was to try the call and interpret the provider's error.

**There was no way to ask for more.** `googleScopes` was a fixed, startup-bound list containing Calendar and
Tasks, so the sign-in consent screen demanded full calendar access from every user regardless of whether they
would ever use the feature. There was no upgrade path either: re-running `/auth/google` is a complete re-login
that mints a new token family and registers a duplicate device.

Two live bugs surfaced while investigating. `socials.expires_at` had been written as SQL NULL on every login
since the column was introduced, because the `pgtype.Timestamp` was constructed without setting `Valid`. And
`GET /socials/me` and `GET /socials/user/{user_id}` serialized `repository.Social` directly, returning provider
access and refresh tokens in the response body — a user's own to every user, and every user's to any holder of
`read:account:any`.

Verisafe serves 3000+ users and is the authentication and authorization server for the platform, so no change
here could break an existing contract.

## Decision

**Store grants in a new `oauth_grants` table**, not as columns on `socials`. `socials` is keyed on the external
provider `user_id`, so production may hold more than one row per `(account_id, provider)`. Adding a UNIQUE
constraint there could fail at boot, and `RunGooseMigrations` panics on error — which would take the service
down. A new table is safe by construction, and it keeps credentials structurally out of `repository.Social`,
which is the struct the leaking endpoints serialize.

**Model scope knowledge honestly.** Providers offer no "what did user X grant" API; scope is reported only in a
token response. Existing grants are therefore seeded from what logins historically *requested* and marked
presumed via a NULL `scopes_verified_at`. The broker never denies on a presumed-missing scope — it refreshes and
lets the provider adjudicate, then re-checks — so a wrong presumption costs one round trip rather than a wrong
refusal. Because every backfilled row has a NULL expiry, which the freshness check treats as stale, the first
broker call for any account verifies it automatically.

**Broker tokens rather than distributing them.** `POST /oauth/{provider}/token` takes an account and a set of
capabilities and returns a short-lived access token. Refresh tokens never leave the process, so a compromised
downstream service loses access when the access token expires rather than retaining indefinite reach into the
user's Google account. The endpoint requires a service token even from a caller holding the permission, so a
human cannot read another user's provider token from their own session.

**Ask for capabilities, not scopes.** Callers name `"calendar"`, and the provider registry maps that onto wire
scopes. There is no raw-scope escape hatch: a caller needing something unmapped adds it to the registry. That
friction is deliberate — it keeps the registry authoritative and keeps the `insufficient_scope` response
meaningful.

**Add a separate incremental-authorization route** rather than a mode flag on the login callback. That handler
upserts the account, registers a device, mints a new token family and issues a fresh token pair — all correct
for a login and all wrong for a scope upgrade, where the user already has a live session that must survive.
Threading a flag through the most load-bearing function in the service would make any mistake a sign-in outage.

**Carry upgrade state as an opaque Redis handle.** The login flow's pipe-delimited state blob holds
client-supplied values with no escaping and no integrity protection, and is replayable. Rather than extend it,
the new flow stores state server-side under a 256-bit handle: nothing user-controlled is in the state, reading
deletes it, and there is no delimiter to escape. PKCE is added where the provider supports it.

**Encrypt provider refresh tokens at rest** with AES-256-GCM under versioned keys, with additional authenticated
data derived from the row identity so ciphertext cannot be transplanted between accounts. Every write re-seals
both tokens under the active key, which makes key rotation lazy — no backfill job.

**Refresh lazily, never on a schedule.** Warming 3000 users' tokens hourly would generate tens of thousands of
pointless provider calls a day to save a few hundred milliseconds on the one request that actually needs a
token. A background reconciler exists solely to drain grants whose scopes are still presumed, and switches off
once that population reaches zero.

**Serialize refreshes with a Redis lock, not `SELECT ... FOR UPDATE`.** A row lock would hold a pooled Postgres
connection across an outbound HTTPS call to the provider; under provider latency that starves the pool and takes
unrelated endpoints down with it. A caller that loses the lock proceeds anyway — a duplicate refresh is
recoverable, a 503 to a downstream service is not.

**Keep the JSON keys when fixing the `/socials/*` leak.** The credential fields serialize as `null` rather than
disappearing, so a strict decoder in an existing client keeps working.

**Do not add a scope claim to `VerisafeClaims`.** It would be stale by construction — a scope granted seconds
after issuance stays invisible until the next token refresh — and there is no performance argument for it, since
`IsAuthenticated` already queries roles and permissions from the database on every request.

## Consequences

Adding Microsoft, which has been planned for some time, becomes one `Descriptor` literal in
`internal/providers/descriptors.go` plus client credentials in config. The broker, refresh service, scope
diffing, database schema and API contracts need no changes.

A downstream service can now obtain a working Google token, and when the user has not granted the capability it
receives a machine-readable `insufficient_scope` body naming exactly what is missing and where to send the user.
Those response keys are a contract; a golden-body test pins them.

`PROVIDER_TOKEN_ENC_KEYS` becomes a required environment variable. A deployment missing it fails to start. This
is intentional: silently storing long-lived replayable credentials in plaintext because a variable was unset is
worse than a failed deploy.

The rollout is sequenced so no release depends on clients moving first. Grant storage and both bug fixes ship
with no new routes; the broker and incremental flow ship while logins still request the historical broad scope
set, so existing users are unaffected; the scope reduction ships last, behind `OAUTH_MINIMAL_LOGIN_SCOPES`, so a
rollback is a restart rather than a rebuild. `include_granted_scopes=true` is load-bearing for that last step —
without it a returning user could complete a narrower authorization and be silently downgraded.

Applying `include_granted_scopes` requires decorating the URL goth produces, because goth binds its auth-code
options at provider construction. That re-encodes the query string including the state parameter, on the one
path every login takes, so a test pins the state round trip byte-for-byte.

The reconciler is the first background job in the codebase. It is off by default, rate limited, and
leader-elected across replicas.

Two transitional plaintext columns exist on `oauth_grants` until every row has been refreshed at least once.
They are not a regression — the same values sit in plaintext in `socials` today — but they are not a fix either
until the reconciler drains them.
