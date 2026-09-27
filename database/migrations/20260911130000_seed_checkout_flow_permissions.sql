-- +goose Up
-- +goose StatementBegin
INSERT INTO permissions (id, name, description, created_at, updated_at)
VALUES
  (
    gen_random_uuid(),
    'create:checkout-session:own',
    'Permission to create browser checkout sessions for own billing orders',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
  ),
  (
    gen_random_uuid(),
    'create:checkout-session:any',
    'Permission to create browser checkout sessions for any billing order',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
  ),
  (
    gen_random_uuid(),
    'checkout:order:read',
    'Checkout-scoped permission to read the bound billing order',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
  ),
  (
    gen_random_uuid(),
    'checkout:order-item:read',
    'Checkout-scoped permission to read items on the bound billing order',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
  ),
  (
    gen_random_uuid(),
    'checkout:order:charge',
    'Checkout-scoped permission to charge the bound billing order',
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
  )
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT
  r.id,
  p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'user'
AND p.name IN (
  'create:checkout-session:own',
  'checkout:order:read',
  'checkout:order-item:read',
  'checkout:order:charge'
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
  'create:checkout-session:any',
  'checkout:order:read',
  'checkout:order-item:read',
  'checkout:order:charge'
)
ON CONFLICT DO NOTHING;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM role_permissions
WHERE role_id IN (
  SELECT id
  FROM roles
  WHERE name IN ('user', 'Billing Manager')
)
AND permission_id IN (
  SELECT id
  FROM permissions
  WHERE name IN (
    'create:checkout-session:own',
    'create:checkout-session:any',
    'checkout:order:read',
    'checkout:order-item:read',
    'checkout:order:charge'
  )
);

DELETE FROM permissions
WHERE name IN (
  'create:checkout-session:own',
  'create:checkout-session:any',
  'checkout:order:read',
  'checkout:order-item:read',
  'checkout:order:charge'
);
-- +goose StatementEnd
