-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

-- Permissions for the third-party OAuth token broker.
--
-- The existing auto_assign_to_system_role / auto_assign_to_admin_role triggers
-- grant every new permission to `system` and `Administrator` automatically.
-- They do NOT grant to `bot`, which is deliberate here.
INSERT INTO permissions (name, description)
VALUES
    ('read:provider_token:any',
     'Permission to obtain a usable third-party OAuth access token on behalf of any account.'),
    ('manage:provider_token:any',
     'Permission to force reconciliation or revocation of any account''s third-party OAuth grant.')
ON CONFLICT (name) DO NOTHING;

-- A dedicated, non-default role rather than granting to the whole `bot` role.
--
-- Granting read:provider_token:any to `bot` would be one line less work and
-- would let every bot in the system read every user's Google token. Instead
-- each consuming service account is assigned this role individually via
-- GET /roles/assign/{user_id}/{role_id}.
INSERT INTO roles (name, description, is_default)
VALUES (
    'oauth-token-broker',
    'Grants a specific service account the ability to broker third-party OAuth tokens on behalf of users.',
    false
)
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'oauth-token-broker'
  AND p.name = 'read:provider_token:any'
ON CONFLICT DO NOTHING;

-- Lets a user see and manage their own third-party connections
-- (GET /oauth/scopes, POST /oauth/{provider}/authorize). Attached to the
-- default `user` role so every existing and future account holds it — the
-- trigger_assign_default_roles_to_account trigger guarantees every account has
-- `user`, so no user_roles backfill is needed.
INSERT INTO permissions (name, description)
VALUES ('manage:oauth_grant:own',
        'Permission to view and authorize your own third-party OAuth connections.')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'user'
  AND p.name = 'manage:oauth_grant:own'
ON CONFLICT DO NOTHING;

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd

DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions
    WHERE name IN (
        'read:provider_token:any',
        'manage:provider_token:any',
        'manage:oauth_grant:own'
    )
);

DELETE FROM roles WHERE name = 'oauth-token-broker';

DELETE FROM permissions
WHERE name IN (
    'read:provider_token:any',
    'manage:provider_token:any',
    'manage:oauth_grant:own'
);
