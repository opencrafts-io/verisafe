-- name: CreatePermission :one
-- Creates a permission on the database
INSERT INTO permissions (
  name, description
) VALUES ( $1, $2 )
RETURNING *;



-- name: GetPermissionByID :many
SELECT * FROM permissions
WHERE id = $1;


-- name: GetAllPermissions :many
SELECT * FROM permissions
LIMIT $1
OFFSET $2;


-- name: GetUserPermissions :many
-- Returns all permissions associated to a user
SELECT * FROM user_permissions_view
WHERE user_id = $1;


-- name: UpdatePermission :one
UPDATE permissions
  SET name = COALESCE($2, name),
  description = COALESCE($3, description),
  updated_at = NOW()
  WHERE id = $1
RETURNING *;


-- name: AssignRolePermission :one
-- Assigns a permission to a role
INSERT INTO role_permissions (
  role_id, permission_id
) VALUES ( $1, $2 )
RETURNING *;

-- name: RevokeRolePermission :exec
-- Revokes a permission from a role
DELETE FROM role_permissions
WHERE role_id = $1 AND permission_id = $2;

