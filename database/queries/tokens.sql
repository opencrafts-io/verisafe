-- name: RecordIssuedToken :one
-- Records an issued access token's information to the db 
INSERT INTO issued_tokens (
  jti, user_id, device_id, expires_at
) VALUES ( $1, $2, $3, $4 )
RETURNING *;

-- name: RecordIssuedRefreshToken :one
-- Persists an issued refresh token's information to the db 
INSERT INTO refresh_tokens (
  token_hash, user_id, device_id, jwt_jti, issued_at, expires_at, family_id
)
VALUES($1, $2, $3, $4, $5, $6, $7)
RETURNING *;


-- name: GetRefreshTokenByHash :one
-- Retrieves an earlier issued refresh token given its hash
SELECT * FROM refresh_tokens WHERE token_hash = $1 LIMIT 1;

-- name: MarkRefreshTokenUsed :exec
-- Marks and persists that a refresh token has been used
UPDATE refresh_tokens
  SET 
    used_at = NOW(),
    revoked_at = NOW()
  WHERE id = $1;

-- name: RevokeRefreshTokenFamily :exec
-- RevokeRefreshTokenFamily revokes all active refresh tokens belonging to a given family.
-- This is triggered when a refresh token reuse attack is detected — i.e. a token that
-- has already been used is presented again. Revoking the entire family forces the user
-- to re-authenticate, invalidating any tokens the attacker may have obtained.
UPDATE refresh_tokens
SET revoked_at = NOW()
WHERE family_id = @family_id
  AND revoked_at IS NULL;



