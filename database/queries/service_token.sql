-- name: CreateServiceToken :one
INSERT INTO service_tokens (
  account_id, name, description, token_hash, expires_at, scopes, max_uses, 
  rotation_policy, ip_whitelist, user_agent_pattern, created_by, metadata
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: GetServiceTokenByHash :one
SELECT * FROM service_tokens
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > NOW())
  AND (max_uses IS NULL OR use_count < max_uses);

-- name: GetServiceTokenByID :one
SELECT * FROM service_tokens
WHERE id = $1;

-- name: ListServiceTokensByAccount :many
SELECT * FROM service_tokens
WHERE account_id = $1
ORDER BY created_at DESC;

-- name: ListActiveServiceTokens :many
SELECT * FROM active_service_tokens
ORDER BY created_at DESC;

-- name: ListServiceTokensNeedingRotation :many
SELECT * FROM service_tokens
WHERE revoked_at IS NULL
  AND rotation_policy IS NOT NULL
  AND rotation_policy->>'auto_rotate' = 'true'
  AND rotation_policy->>'rotation_interval_days' IS NOT NULL
  AND rotated_at IS NOT NULL
  AND rotated_at + (rotation_policy->>'rotation_interval_days')::INTEGER * INTERVAL '1 day' < NOW();

-- name: RotateServiceToken :exec
UPDATE service_tokens
SET
  token_hash = $2,
  rotated_at = NOW(),
  created_at = NOW(),
  expires_at = $3,
  last_used_at = NULL,
  use_count = 0,
  metadata = COALESCE(metadata, '{}'::jsonb) - 'needs_rotation'
WHERE id = $1;

-- name: RevokeServiceToken :exec
UPDATE service_tokens
SET revoked_at = NOW()
WHERE id = $1;

-- name: UpdateServiceTokenLastUsed :exec
UPDATE service_tokens
SET 
  last_used_at = NOW(),
  use_count = use_count + 1
WHERE id = $1;

-- name: UpdateServiceToken :exec
UPDATE service_tokens
SET
  name = $2,
  description = $3,
  scopes = $4,
  max_uses = $5,
  rotation_policy = $6,
  ip_whitelist = $7,
  user_agent_pattern = $8,
  metadata = $9
WHERE id = $1;

-- name: DeleteServiceToken :exec
DELETE FROM service_tokens
WHERE id = $1;

-- name: GetServiceTokenUsageStats :one
SELECT 
  COUNT(*) as total_tokens,
  COUNT(*) FILTER (WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())) as active_tokens,
  COUNT(*) FILTER (WHERE revoked_at IS NOT NULL) as revoked_tokens,
  COUNT(*) FILTER (WHERE expires_at IS NOT NULL AND expires_at < NOW()) as expired_tokens,
  COUNT(*) FILTER (WHERE last_used_at IS NOT NULL AND last_used_at > NOW() - INTERVAL '30 days') as recently_used_tokens
FROM service_tokens
WHERE account_id = $1;

-- name: CleanupExpiredServiceTokens :exec
SELECT cleanup_expired_service_tokens();

-- name: MarkTokensForRotation :exec
SELECT auto_rotate_service_tokens();
