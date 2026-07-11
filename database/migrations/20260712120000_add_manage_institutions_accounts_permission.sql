-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

-- Admin override for POST/DELETE /institutions/account: lets a holder link
-- or unlink any account's institution membership, not just their own.
-- Auto-granted to the system and Administrator roles by the existing
-- auto_assign_to_system_role / auto_assign_to_admin_role triggers.
INSERT INTO permissions (name, description)
VALUES
    ('manage:institutions:accounts:any', 'Permission to link or unlink any account''s institution membership, not just your own.');

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd

DELETE FROM permissions
WHERE name = 'manage:institutions:accounts:any';
