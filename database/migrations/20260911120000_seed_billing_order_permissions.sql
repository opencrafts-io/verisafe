-- +goose Up
-- +goose StatementBegin
INSERT INTO permissions (id, name, description, created_at, updated_at)
VALUES
  (gen_random_uuid(), 'create:order:any', 'Permission to create billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'read:order:any', 'Permission to read billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'update:order:any', 'Permission to update, cancel, recalculate, and charge billing orders', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'create:order-item:any', 'Permission to add billing order items', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'read:order-item:any', 'Permission to read billing order items', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'update:order-item:any', 'Permission to update billing order items', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  (gen_random_uuid(), 'delete:order-item:any', 'Permission to delete billing order items', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (name) DO NOTHING;

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
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
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

DELETE FROM permissions
WHERE name IN (
  'create:order:any',
  'read:order:any',
  'update:order:any',
  'create:order-item:any',
  'read:order-item:any',
  'update:order-item:any',
  'delete:order-item:any'
);
-- +goose StatementEnd
