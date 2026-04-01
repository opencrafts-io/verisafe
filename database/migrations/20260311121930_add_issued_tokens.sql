-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
CREATE TABLE issued_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    jti             UUID UNIQUE NOT NULL,
    user_id         UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    device_id       UUID REFERENCES user_devices(id) ON DELETE SET NULL,
    issued_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMP NOT NULL,
    revoked_at      TIMESTAMP,
    last_used_at    TIMESTAMP
);

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
DROP TABLE IF EXISTS issued_tokens;
-- +goose StatementEnd
