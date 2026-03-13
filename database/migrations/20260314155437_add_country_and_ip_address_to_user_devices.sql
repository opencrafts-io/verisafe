-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
ALTER TABLE user_devices
  ADD COLUMN ip_address INET,
  ADD COLUMN country VARCHAR(3);


-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
ALTER TABLE user_devices
  DROP COLUMN IF EXISTS ip_address CASCADE,
  DROP COLUMN IF EXISTS country CASCADE;
-- +goose StatementEnd
