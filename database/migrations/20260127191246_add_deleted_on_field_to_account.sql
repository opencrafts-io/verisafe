-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
ALTER TABLE accounts
ADD COLUMN deleted_at timestamptz; 


CREATE INDEX IF NOT EXISTS idx_accounts_active
ON accounts (id)
WHERE deleted_at IS NULL;

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';

DROP INDEX IF EXISTS idx_accounts_active;

ALTER TABLE accounts
DROP COLUMN deleted_at;

-- +goose StatementEnd
