-- name: CreateRole :one
-- Creates a role
INSERT INTO roles ( 
  name, description
) VALUES ( $1, $2 )
RETURNING *;



-- name: GetRoleByID :one
-- Retrieves a role specified by its id
SELECT * FROM roles WHERE id = $1;


-- name: GetAllRoles :many
-- Retrieves a list of roles
SELECT * FROM roles 
LIMIT $1
OFFSET $2;


-- name: GetAllUserRoles :many
-- Retrieves all roles that a user has 
SELECT * FROM user_roles_view WHERE user_id = $1;

-- name: UpdateRole :one
UPDATE roles
  SET name =  COALESCE($2, name),
  description = COALESCE($3, description)
  WHERE id = $1
RETURNING *;


-- name: GetRolePermissions :many
-- Retrieves all permissions that a re assigned to a role
SELECT * FROM role_permissions_view
WHERE role_id = $1;


-- name: AssignRole :one
-- Assigns a role to a user
INSERT INTO user_roles  (
  user_id, role_id
) VALUES ( $1, $2 )
RETURNING *;


-- name: RevokeRole :exec
-- Revokes a role from a user
DELETE FROM user_roles
  WHERE user_id = $1 AND role_id = $2;
