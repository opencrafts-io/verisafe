-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

ALTER TABLE accounts
  ADD COLUMN terms_accepted BOOLEAN DEFAULT FALSE,
  ADD COLUMN onboarded BOOLEAN DEFAULT FALSE;

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
ALTER TABLE accounts
  DROP COLUMN IF EXISTS terms_accepted,
  DROP COLUMN IF EXISTS onboarded;
-- +goose StatementEnd
