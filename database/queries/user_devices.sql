-- name: RecordUserDevice :one
-- Inserts a new device. If the device is already registered (same user + push_token),
-- only last_active_at, ip_address, and country are updated.
INSERT INTO user_devices (
  user_id, device_name, platform, device_token, ip_address, country, last_active_at
) VALUES ( $1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (user_id, device_token)
DO UPDATE SET
  last_active_at = EXCLUDED.last_active_at,
  ip_address     = EXCLUDED.ip_address,
  country        = EXCLUDED.country
RETURNING *;


-- name: GetUserDevices :many
-- Retrieves all user devices that a user has ever used to access their accounts
-- Results are orderd by the most recent device used to access the account
SELECT 
  *
FROM user_devices
WHERE user_id = $1
ORDER BY created_at DESC;
