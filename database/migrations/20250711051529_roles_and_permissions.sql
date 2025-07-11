-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
CREATE TABLE roles (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL UNIQUE,
  description TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);


CREATE TABLE permissions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(255) NOT NULL UNIQUE,
  description TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP 
);


CREATE TABLE role_permissions (
  role_id UUID REFERENCES roles(id) ON DELETE CASCADE,
  permission_id UUID REFERENCES permissions(id) ON DELETE CASCADE,
  PRIMARY KEY (role_id, permission_id)
);


CREATE TABLE user_roles (
  user_id UUID REFERENCES accounts(id) ON DELETE CASCADE,
  role_id UUID REFERENCES roles(id) ON DELETE CASCADE,
  PRIMARY KEY (user_id, role_id)
);


INSERT INTO accounts (email, name)
VALUES('systemuser@opencrafts.io', 'System User')
ON CONFLICT (email) DO NOTHING
;

INSERT INTO roles (name, description) VALUES ('system', 'System level role')
ON CONFLICT(name) DO NOTHING;


INSERT INTO user_roles (user_id, role_id)
SELECT a.id, r.id
FROM accounts a, roles r
WHERE a.email = 'systemuser@opencrafts.io'
  AND r.name = 'system'
ON CONFLICT DO NOTHING;

-- +goose StatementBegin
-- Trigger to add all roles to the system user
CREATE OR REPLACE FUNCTION assign_permission_to_system_role()
RETURNS TRIGGER AS $$
DECLARE
    system_role_id UUID;
BEGIN
    SELECT id INTO system_role_id FROM roles WHERE name = 'system';

    -- Only assign if system role exists
    IF system_role_id IS NOT NULL THEN
        INSERT INTO role_permissions (role_id, permission_id)
        VALUES (system_role_id, NEW.id)
        ON CONFLICT DO NOTHING;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER auto_assign_to_system_role
AFTER INSERT ON permissions
FOR EACH ROW
EXECUTE FUNCTION assign_permission_to_system_role();

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd


-- 1. Drop the trigger from the `permissions` table
DROP TRIGGER IF EXISTS auto_assign_to_system_role ON permissions;

-- 2. Drop the trigger function
DROP FUNCTION IF EXISTS assign_permission_to_system_role();

-- 3. Remove the user-role association
DELETE FROM user_roles
WHERE user_id = (
    SELECT id FROM accounts WHERE email = 'systemuser@opencrafts.io'
)
AND role_id = (
    SELECT id FROM roles WHERE name = 'system'
);

-- 4. Remove the system user
DELETE FROM accounts
WHERE email = 'systemuser@opencrafts.io';

-- 5. Remove the system role
DELETE FROM roles
WHERE name = 'system';

DROP TABLE IF EXISTS user_roles;

DROP TABLE IF EXISTS role_permissions;

DROP TABLE IF EXISTS permissions;

DROP TABLE IF EXISTS roles;
