-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
ALTER TABLE accounts
  ADD COLUMN national_id VARCHAR(255);
ALTER TABLE accounts
  ADD COLUMN username VARCHAR(255);
ALTER TABLE accounts
  ADD COLUMN avatar_url TEXT;
ALTER TABLE accounts
  ADD COLUMN bio TEXT;
ALTER TABLE accounts
  ADD COLUMN vibe_points BIGINT NOT NULL DEFAULT 0;
ALTER TABLE accounts
  ADD COLUMN phone VARCHAR(30);


-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd
ALTER TABLE accounts
  DROP COLUMN IF EXISTS national_id;
ALTER TABLE accounts
  DROP COLUMN IF EXISTS username;
ALTER TABLE accounts
  DROP COLUMN IF EXISTS avatar_url;
ALTER TABLE accounts
  DROP COLUMN IF EXISTS bio;
ALTER TABLE accounts
  DROP COLUMN IF EXISTS vibe_points;
ALTER TABLE accounts
  DROP COLUMN IF EXISTS phone;
