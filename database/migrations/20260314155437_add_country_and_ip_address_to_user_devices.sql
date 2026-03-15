-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
ALTER TABLE user_devices
  RENAME COLUMN push_token TO device_token;

ALTER TABLE user_devices
  ADD COLUMN ip_address INET,
  ADD COLUMN country VARCHAR(3);

ALTER TABLE user_devices
  ADD CONSTRAINT uq_user_devices_device_token
  UNIQUE(user_id, device_token);

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
ALTER TABLE user_devices 
  DROP CONSTRAINT
  uq_user_devices_device_token;

ALTER TABLE user_devices
  DROP COLUMN IF EXISTS ip_address CASCADE,
  DROP COLUMN IF EXISTS country CASCADE;

ALTER TABLE user_devices 
  RENAME COLUMN
  device_token TO push_token;
-- +goose StatementEnd
