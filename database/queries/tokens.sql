-- name: RecordIssuedToken :one
-- Records an issued access token's information to the db 
INSERT INTO issued_tokens (
  jti, user_id, device_id, expires_at
) VALUES ( $1, $2, $3, $4 )
RETURNING *;

-- name: RecordIssuedRefreshToken :one
-- Persists an issued refresh token's information to the db 
INSERT INTO refresh_tokens (
  token_hash, user_id, device_id, jwt_jti, issued_at, family_id
)
VALUES($1, $2, $3, $4, $5, $6)
RETURNING *;

