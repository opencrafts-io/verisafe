-- +goose Up
-- +goose StatementBegin
-- Assign read:plan:any permission to the user role
INSERT INTO role_permissions (role_id, permission_id)
SELECT 
  r.id,
  p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'user'
AND p.name = 'read:plan:any'
ON CONFLICT DO NOTHING;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
-- Remove read:plan:any permission from the user role
delete from role_permissions
where
    role_id = (select id from roles where name = 'user')
    and permission_id = (select id from permissions where name = 'read:plan:any')
;

-- +goose StatementEnd
