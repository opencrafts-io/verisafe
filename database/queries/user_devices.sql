-- name: RecordUserDevice :one
-- Records a new user device 
INSERT INTO user_devices (
  user_id, device_name, platform, push_token, last_active_at
) VALUES ( $1, $2, $3, $4, $5)
RETURNING *;


-- name: GetUserDevices :many
-- Retrieves all user devices that a user has ever used to access their accounts
-- Results are orderd by the most recent device used to access the account
SELECT 
  id, 
  user_id,
  device_name,
  platform,
  push_token,
  last_active_at,
  created_at
FROM user_devices
WHERE user_id = $1
ORDER BY created_at DESC;
