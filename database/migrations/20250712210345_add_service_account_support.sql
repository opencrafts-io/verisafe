-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

CREATE TYPE account_type AS ENUM (
  'human',         -- Individual user (default)
  'service',       -- Service/microservice/API user
  'bot',           -- Automated accounts not directly tied to a human or service
  'organization'   -- An entity that can have members and resources
);

ALTER TABLE accounts
ADD COLUMN type account_type NOT NULL DEFAULT 'human';


CREATE TABLE IF NOT EXISTS service_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  rotated_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,

  UNIQUE(account_id, name)
);


ALTER TABLE roles ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION assign_default_roles_to_account()
RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO user_roles (user_id, role_id)
  SELECT NEW.id, id FROM roles WHERE is_default = true
  ON CONFLICT DO NOTHING;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER trigger_assign_default_roles_to_account
AFTER INSERT ON accounts
FOR EACH ROW
EXECUTE FUNCTION assign_default_roles_to_account();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION assign_default_role_to_all_accounts()
RETURNS TRIGGER AS $$
BEGIN
  IF NEW.is_default THEN
    INSERT INTO user_roles (user_id, role_id) SELECT id, NEW.id FROM accounts
    ON CONFLICT DO NOTHING;
  END IF;

  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- +goose StatementEnd

CREATE TRIGGER trigger_assign_default_role_to_all_accounts
AFTER INSERT ON roles
FOR EACH ROW
EXECUTE FUNCTION assign_default_role_to_all_accounts();


-- Create a default user role for all users
INSERT INTO roles (name, description, is_default)
VALUES (
  'user',
  'Default role assigned to all users',
  true
);

INSERT INTO roles (name, description, is_default)
VALUES (
  'bot',
  'Default role assigned to all bots',
  false
);


-- Permissions for this iteration
INSERT INTO permissions (name, description) VALUES
  ('create:service_token:own','Permission to generate a service token'),
  ('update:service_token:own','Permission to rotate a service token'),
  ('read:service_token:any','Permission to retrieve a service token'),
  ('delete:service_token:own','Permission to revoke a service token'),
  ('delete:service_token:any','Permission to revoke a service token on behalf of service (for admin use only)'),


  ('update:account:own','Permission to update own account details'),
  ('read:account:own','Permission to read own account details'),
  ('delete:account:own','Permission to delete own account'),

  ('create:account:any','Permission to create any account'),
  ('update:account:any','Permission to update any account'),
  ('read:account:any','Permission to read any account details'),
  ('delete:account:any','Permission to delete any account');

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'bot'
  AND p.name IN (
  'create:service_token:own',
  'update:service_token:own',
  'delete:service_token:own'
);


INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'user'
  AND p.name IN (
  'update:account:own',
  'read:account:own',
  'delete:account:own'
);


-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd

ALTER TABLE accounts
DROP COLUMN type;
DROP TYPE IF EXISTS account_type; DROP TABLE IF EXISTS service_tokens; ALTER TABLE roles
DROP COLUMN IF EXISTS is_default;


DROP TRIGGER IF EXISTS trigger_assign_default_roles_to_account ON accounts;
DROP FUNCTION IF EXISTS assign_default_roles_to_account;


DROP TRIGGER IF EXISTS trigger_assign_default_role_to_all_accounts ON roles;
DROP FUNCTION IF EXISTS assign_default_role_to_all_accounts;

DELETE FROM roles WHERE name = 'user';
DELETE FROM roles WHERE name = 'bot';

DELETE FROM permissions
WHERE name IN (
  'create:service_token:own',
  'update:service_token:own',
  'read:service_token:any',
  'delete:service_token:own',
  'delete:service_token:any',

  'update:account:own',
  'read:account:own',
  'delete:account:own',

  'create:account:any',
  'update:account:any',
  'read:account:any',
  'delete:account:any'
);
