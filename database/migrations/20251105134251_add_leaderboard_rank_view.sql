-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
CREATE VIEW account_vibepoint_rank AS
SELECT 
  id,
  email,
  name,
  username,
  vibe_points,
  avatar_url,
  created_at,
  updated_at,
  RANK() OVER (ORDER BY vibe_points DESC) AS vibe_rank
  FROM accounts 
WHERE accounts.type = 'human';

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
DROP VIEW IF EXISTS account_vibepoint_rank;
-- +goose StatementEnd
