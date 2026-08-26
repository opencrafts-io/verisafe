-- +goose Up
-- +goose StatementBegin
-- Insert the Plans Manager role
INSERT INTO roles (id, name, description, is_default, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Plans Manager',
  'Manages plans including create, read, update, and delete operations',
  false,
  CURRENT_TIMESTAMP,
  CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;

-- Insert the permissions
INSERT INTO permissions (id, name, description, created_at, updated_at)
VALUES 
  (gen_random_uuid(), 'create:plan:any', 'Permission to create plans', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'read:plan:any', 'Permission to read/list plans', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'update:plan:any', 'Permission to update plans', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'delete:plan:any', 'Permission to delete plans', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (name) DO NOTHING;

-- Link permissions to the Plans Manager role
INSERT INTO role_permissions (role_id, permission_id)
SELECT 
  r.id,
  p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'Plans Manager'
AND p.name IN ('create:plan:any', 'read:plan:any', 'update:plan:any', 'delete:plan:any')
ON CONFLICT DO NOTHING;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
-- Delete the role (this will cascade delete role_permissions)
delete from roles
where name = 'Plans Manager'
;

-- Delete the permissions
delete from permissions
where name in ('create:plan:any', 'read:plan:any', 'update:plan:any', 'delete:plan:any')
;

-- +goose StatementEnd
