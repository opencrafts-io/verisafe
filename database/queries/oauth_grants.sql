-- name: GetOAuthGrant :one
-- Retrieves an account's grant for a single provider.
SELECT * FROM oauth_grants
WHERE account_id = $1 AND provider = lower($2)
LIMIT 1;


-- name: GetOAuthGrantByID :one
SELECT * FROM oauth_grants WHERE id = $1 LIMIT 1;


-- name: ListOAuthGrantsByAccount :many
-- Every provider an account has connected. Not paginated — the provider set
-- is small and bounded by the registry.
SELECT * FROM oauth_grants
WHERE account_id = $1
ORDER BY provider;


-- name: UpsertOAuthGrant :one
-- Records or replaces an account's grant for a provider.
--
-- IMPORTANT INVARIANT: both token columns are sealed under the single
-- enc_key_version stored on the row, so the caller must always supply BOTH
-- ciphertexts, freshly sealed under the active key. When a provider declines
-- to rotate the refresh token, the caller re-seals the one it already had
-- rather than passing NULL.
--
-- Preserving one column while overwriting the other (the obvious
-- COALESCE(EXCLUDED.x, oauth_grants.x) shape) would leave ciphertext from an
-- old key alongside a bumped enc_key_version and permanently corrupt
-- decryption for that row.
--
-- The plaintext columns are always cleared, which is what lazily migrates
-- backfilled rows off the transitional plaintext as they are used.
INSERT INTO oauth_grants (
    account_id,
    provider,
    external_user_id,
    access_token_enc,
    refresh_token_enc,
    enc_key_version,
    granted_scopes,
    scopes_verified_at,
    expires_at,
    last_refreshed_at,
    access_token_plain,
    refresh_token_plain,
    refresh_failure_count,
    last_refresh_error,
    revoked_at,
    revoked_reason
)
VALUES (
    @account_id,
    lower(@provider),
    @external_user_id,
    @access_token_enc,
    @refresh_token_enc,
    @enc_key_version,
    @granted_scopes,
    @scopes_verified_at,
    @expires_at,
    @last_refreshed_at,
    NULL,
    NULL,
    0,
    NULL,
    NULL,
    NULL
)
ON CONFLICT (account_id, provider) DO UPDATE SET
    external_user_id  = COALESCE(EXCLUDED.external_user_id, oauth_grants.external_user_id),
    access_token_enc  = EXCLUDED.access_token_enc,
    refresh_token_enc = EXCLUDED.refresh_token_enc,
    enc_key_version   = EXCLUDED.enc_key_version,
    granted_scopes    = EXCLUDED.granted_scopes,
    -- Never let a verified grant fall back to presumed.
    scopes_verified_at = COALESCE(EXCLUDED.scopes_verified_at, oauth_grants.scopes_verified_at),
    expires_at         = EXCLUDED.expires_at,
    last_refreshed_at  = EXCLUDED.last_refreshed_at,
    access_token_plain  = NULL,
    refresh_token_plain = NULL,
    -- A successful write clears the failure state and un-revokes: the account
    -- has just proven it holds working credentials again.
    refresh_failure_count = 0,
    last_refresh_error    = NULL,
    revoked_at            = NULL,
    revoked_reason        = NULL,
    updated_at            = NOW()
RETURNING *;


-- name: MarkOAuthGrantRevoked :exec
-- Marks a grant unusable and destroys the stored credentials.
--
-- Only for terminal conditions — the provider rejecting the refresh token, or
-- an explicit user disconnect. A transient provider failure must use
-- RecordOAuthGrantRefreshFailure instead; revoking on a provider outage would
-- disconnect every user at once.
UPDATE oauth_grants
SET revoked_at = NOW(),
    revoked_reason = @reason,
    access_token_enc = NULL,
    refresh_token_enc = NULL,
    access_token_plain = NULL,
    refresh_token_plain = NULL,
    expires_at = NULL,
    updated_at = NOW()
WHERE id = @id;


-- name: RecordOAuthGrantRefreshFailure :exec
-- Records a transient refresh failure without touching the credentials.
UPDATE oauth_grants
SET refresh_failure_count = refresh_failure_count + 1,
    last_refresh_error = @last_refresh_error,
    updated_at = NOW()
WHERE id = @id;


-- name: ListUnverifiedOAuthGrants :many
-- Grants whose scope list is still presumed rather than provider-confirmed.
-- Drives the reconciliation worker, which drains the long tail of accounts
-- nobody has brokered a token for.
SELECT * FROM oauth_grants
WHERE scopes_verified_at IS NULL
  AND revoked_at IS NULL
  AND (refresh_token_enc IS NOT NULL OR refresh_token_plain IS NOT NULL)
ORDER BY updated_at
LIMIT $1;


-- name: CountOAuthGrantsWithPlaintext :one
-- Operational check gating the removal of the transitional plaintext columns.
SELECT count(*) FROM oauth_grants
WHERE access_token_plain IS NOT NULL OR refresh_token_plain IS NOT NULL;
