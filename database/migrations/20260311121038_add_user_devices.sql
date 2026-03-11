-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
CREATE TABLE user_devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    device_name     TEXT,
    platform        TEXT,
    push_token      TEXT,
    last_active_at  TIMESTAMP,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';

DROP TABLE IF EXISTS user_devices;
-- +goose StatementEnd
