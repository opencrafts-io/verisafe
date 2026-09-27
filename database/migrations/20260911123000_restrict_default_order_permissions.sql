-- +goose Up
-- +goose StatementBegin
INSERT INTO permissions (id, name, description, created_at, updated_at)
VALUES
  (gen_random_uuid(), 'create:order:own', 'Permission to create own billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'read:order:own', 'Permission to read own billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'update:order:own', 'Permission to update, cancel, recalculate, and charge own billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'create:order-item:own', 'Permission to add items to own billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'read:order-item:own', 'Permission to read items on own billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'update:order-item:own', 'Permission to update items on own billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'delete:order-item:own', 'Permission to delete items on own billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (name) DO NOTHING;

INSERT INTO roles (id, name, description, is_default, created_at, updated_at)
VALUES (
  gen_random_uuid(),
  'Billing Manager',
  'Manages billing orders and order items across users',
  false,
  CURRENT_TIMESTAMP,
  CURRENT_TIMESTAMP
)
ON CONFLICT (name) DO NOTHING;

DELETE FROM role_permissions
WHERE role_id = (SELECT id FROM roles WHERE name = 'user')
AND permission_id IN (
  SELECT id
  FROM permissions
  WHERE name IN (
    'create:order:any',
    'read:order:any',
    'update:order:any',
    'create:order-item:any',
    'read:order-item:any',
    'update:order-item:any',
    'delete:order-item:any'
  )
);

INSERT INTO role_permissions (role_id, permission_id)
SELECT
  r.id,
  p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'user'
AND p.name IN (
  'create:order:own',
  'read:order:own',
  'update:order:own',
  'create:order-item:own',
  'read:order-item:own',
  'update:order-item:own',
  'delete:order-item:own'
)
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT
  r.id,
  p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'Billing Manager'
AND p.name IN (
  'create:order:any',
  'read:order:any',
  'update:order:any',
  'create:order-item:any',
  'read:order-item:any',
  'update:order-item:any',
  'delete:order-item:any'
)
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM role_permissions
WHERE role_id = (SELECT id FROM roles WHERE name = 'Billing Manager')
AND permission_id IN (
  SELECT id
  FROM permissions
  WHERE name IN (
    'create:order:any',
    'read:order:any',
    'update:order:any',
    'create:order-item:any',
    'read:order-item:any',
    'update:order-item:any',
    'delete:order-item:any'
  )
);

DELETE FROM role_permissions
WHERE role_id = (SELECT id FROM roles WHERE name = 'user')
AND permission_id IN (
  SELECT id
  FROM permissions
  WHERE name IN (
    'create:order:own',
    'read:order:own',
    'update:order:own',
    'create:order-item:own',
    'read:order-item:own',
    'update:order-item:own',
    'delete:order-item:own'
  )
);

INSERT INTO role_permissions (role_id, permission_id)
SELECT
  r.id,
  p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'user'
AND p.name IN (
  'create:order:any',
  'read:order:any',
  'update:order:any',
  'create:order-item:any',
  'read:order-item:any',
  'update:order-item:any',
  'delete:order-item:any'
)
ON CONFLICT DO NOTHING;

DELETE FROM permissions
WHERE name IN (
  'create:order:own',
  'read:order:own',
  'update:order:own',
  'create:order-item:own',
  'read:order-item:own',
  'update:order-item:own',
  'delete:order-item:own'
);

DELETE FROM roles
WHERE name = 'Billing Manager';
-- +goose StatementEnd
