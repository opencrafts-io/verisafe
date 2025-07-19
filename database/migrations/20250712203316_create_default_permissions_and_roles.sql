-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
INSERT INTO roles (name, description) VALUES ('Administrator', 'Default role for administrative purposes')
ON CONFLICT(name) DO NOTHING;

-- +goose StatementBegin
-- Trigger to add all roles to the administrative role
CREATE OR REPLACE FUNCTION assign_permission_to_admin_role()
RETURNS TRIGGER AS $$
DECLARE
    admin_role_id UUID;
BEGIN
    SELECT id INTO admin_role_id FROM roles WHERE name = 'Administrator';

    -- Only assign if system role exists
    IF admin_role_id IS NOT NULL THEN
        INSERT INTO role_permissions (role_id, permission_id)
        VALUES (admin_role_id, NEW.id)
        ON CONFLICT DO NOTHING;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd


CREATE TRIGGER auto_assign_to_admin_role
AFTER INSERT ON permissions
FOR EACH ROW
EXECUTE FUNCTION assign_permission_to_admin_role();

INSERT INTO permissions (name, description)
VALUES
    ('create:role', 'Permission to create new roles.'),
    ('read:role:any', 'Permission to read any role.'),
    ('read:role:permissions', 'Permission to view permissions associated with a role.'),
    ('update:role:any', 'Permission to update any role.'),
    ('assign:role:any', 'Permission to assign roles to users.'),
    ('revoke:role:any', 'Permission to revoke roles from users.'),
    ('create:permission', 'Permission to create new permissions.'),
    ('read:permission:any', 'Permission to read any permission.'),
    ('read:permission:user', 'Permission to view the permissions of a user.'),
    ('update:permission:any', 'Permission to update any permission.'),
    ('assign:permission:role', 'Permission to assign permissions to roles.'),
    ('revoke:permission:role', 'Permission to revoke permissions from roles.');

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd
DELETE FROM permissions
WHERE name IN (
    'create:role',
    'read:role:any',
    'read:role:permissions',
    'update:role:any',
    'assign:role:any',
    'revoke:role:any',
    'create:permission',
    'read:permission:any',
    'read:permission:user',
    'update:permission:any',
    'assign:permission:role',
    'revoke:permission:role'
);

DELETE FROM roles
WHERE name = 'Administrator';

DROP TRIGGER IF EXISTS auto_assign_to_admin_role ON permissions;

-- 2. Drop the trigger function
DROP FUNCTION IF EXISTS assign_permission_to_admin_role();


