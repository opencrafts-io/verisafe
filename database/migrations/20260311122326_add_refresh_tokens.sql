-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
CREATE TABLE refresh_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash      TEXT UNIQUE NOT NULL,
    user_id         UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    device_id       UUID REFERENCES user_devices(id) ON DELETE SET NULL,
    jwt_jti         UUID REFERENCES issued_tokens(jti) ON DELETE SET NULL,
    issued_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMP NOT NULL,
    used_at         TIMESTAMP,
    revoked_at      TIMESTAMP,
    replaced_by     UUID REFERENCES refresh_tokens(id),
    family_id       UUID NOT NULL DEFAULT gen_random_uuid()
);

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
DROP TABLE IF EXISTS refresh_tokens;
-- +goose StatementEnd
