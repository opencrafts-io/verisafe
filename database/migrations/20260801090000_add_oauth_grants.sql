-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

-- oauth_grants holds third-party OAuth credentials and, for the first time,
-- what the user actually granted.
--
-- This is a new table rather than columns on `socials` for one decisive
-- reason: `socials` is keyed on the external provider user_id, so production
-- may hold more than one row per (account_id, provider). Adding a UNIQUE
-- constraint there could fail at boot — and RunGooseMigrations panics on
-- error, which would take the service down. A new table is safe by
-- construction. It also keeps credentials structurally out of
-- repository.Social, which is the struct the /socials endpoints serialize.
CREATE TABLE IF NOT EXISTS oauth_grants (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id        UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    provider          VARCHAR(50) NOT NULL,
    external_user_id  VARCHAR(255),

    -- AES-256-GCM sealed as nonce(12) || ciphertext || tag. See
    -- internal/secrets. enc_key_version records which key sealed the row so
    -- key rotation can be lazy rather than a backfill.
    access_token_enc  BYTEA,
    refresh_token_enc BYTEA,
    enc_key_version   SMALLINT NOT NULL DEFAULT 1,

    -- Transitional plaintext, carried over from `socials` by the backfill
    -- below. Every successful refresh re-seals into the _enc columns and NULLs
    -- these, so the population drains itself. Dropped in a later migration
    -- once a count confirms they are empty everywhere.
    access_token_plain  TEXT,
    refresh_token_plain TEXT,

    granted_scopes TEXT[] NOT NULL DEFAULT '{}',
    -- NULL means the scope list is *presumed* — seeded from what logins
    -- historically requested, never confirmed by the provider. Non-NULL means
    -- the provider told us this set at a token exchange or refresh. Providers
    -- offer no "what did user X grant" API, so this distinction is the honest
    -- way to model what we know.
    scopes_verified_at TIMESTAMPTZ,

    -- NULL expiry means unknown, which the refresh logic treats as stale. That
    -- is deliberate: every backfilled row lands here, so the first use of each
    -- grant triggers a refresh whose response verifies the scopes.
    expires_at            TIMESTAMPTZ,
    last_refreshed_at     TIMESTAMPTZ,
    refresh_failure_count INT NOT NULL DEFAULT 0,
    last_refresh_error    TEXT,

    revoked_at     TIMESTAMPTZ,
    revoked_reason TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT oauth_grants_account_provider_key UNIQUE (account_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_oauth_grants_account
    ON oauth_grants(account_id);

-- Drives the reconciliation worker, which drains grants nobody has brokered a
-- token for yet.
CREATE INDEX IF NOT EXISTS idx_oauth_grants_unverified
    ON oauth_grants(updated_at)
    WHERE scopes_verified_at IS NULL AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_oauth_grants_expiring
    ON oauth_grants(expires_at)
    WHERE revoked_at IS NULL;

-- Backfill from socials.
--
-- DISTINCT ON collapses any duplicate (account_id, provider) pairs the socials
-- primary key permitted, newest row winning. ON CONFLICT DO NOTHING keeps the
-- migration re-runnable after a rollback and redeploy.
--
-- granted_scopes is seeded from what each provider's logins historically
-- requested, and scopes_verified_at is left NULL to mark it as presumed. The
-- broker never hard-denies on a presumed-missing scope; it refreshes and lets
-- the provider adjudicate, so a wrong presumption costs one round trip rather
-- than a wrong denial.
INSERT INTO oauth_grants (
    account_id, provider, external_user_id,
    access_token_plain, refresh_token_plain,
    granted_scopes, scopes_verified_at, expires_at, created_at, updated_at
)
SELECT DISTINCT ON (s.account_id, lower(s.provider))
    s.account_id,
    lower(s.provider),
    s.user_id,
    NULLIF(s.access_token, ''),
    NULLIF(s.refresh_token, ''),
    CASE lower(s.provider)
        WHEN 'google' THEN ARRAY[
            'https://www.googleapis.com/auth/userinfo.email',
            'https://www.googleapis.com/auth/userinfo.profile',
            'https://www.googleapis.com/auth/calendar',
            'https://www.googleapis.com/auth/tasks']
        WHEN 'spotify' THEN ARRAY[
            'user-read-playback-state','user-modify-playback-state',
            'user-read-currently-playing','user-read-recently-played',
            'user-top-read','app-remote-control','playlist-read-private',
            'playlist-modify-private','playlist-modify-public',
            'user-follow-modify','user-follow-read','user-read-email',
            'user-read-private']
        WHEN 'apple' THEN ARRAY['name','email']
        ELSE ARRAY[]::TEXT[]
    END,
    NULL,
    s.expires_at AT TIME ZONE 'UTC',
    COALESCE(s.created_at, NOW()),
    COALESCE(s.updated_at, NOW())
FROM socials s
WHERE s.account_id IS NOT NULL
ORDER BY
    s.account_id,
    lower(s.provider),
    s.updated_at DESC NULLS LAST,
    s.created_at DESC NULLS LAST
ON CONFLICT (account_id, provider) DO NOTHING;

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd

DROP TABLE IF EXISTS oauth_grants;
