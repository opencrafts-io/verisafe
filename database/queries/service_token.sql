-- name: CreateServiceToken :one
INSERT INTO service_tokens (
  account_id, name, token_hash, expires_at
) VALUES (
  $1, $2, $3, $4
)
RETURNING *;



-- name: GetServiceTokenByHash :one
SELECT * FROM service_tokens
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > NOW());



-- name: ListServiceTokensByAccount :many
SELECT * FROM service_tokens
WHERE account_id = $1
ORDER BY created_at DESC;


-- name: RotateServiceToken :exec
UPDATE service_tokens
SET
  token_hash = $2,
  rotated_at = NOW(),
  created_at = NOW(),
  expires_at = $3,
  last_used_at = NULL
WHERE id = $1;


-- name: RevokeServiceToken :exec
UPDATE service_tokens
SET revoked_at = NOW()
WHERE id = $1;


-- name: UpdateServiceTokenLastUsed :exec
UPDATE service_tokens
SET last_used_at = NOW()
WHERE id = $1;


-- name: DeleteServiceToken :exec
DELETE FROM service_tokens
WHERE id = $1;
