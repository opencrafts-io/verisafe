-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

CREATE OR REPLACE VIEW user_roles_view AS
SELECT
  a.id AS user_id,
  a.email,
  a.name,
  r.id AS role_id,
  r.name AS role_name,
  r.description AS role_description,
  r.created_at AS role_created_at
FROM
  accounts a
JOIN
  user_roles ur ON ur.user_id = a.id
JOIN
  roles r ON r.id = ur.role_id;



CREATE OR REPLACE VIEW user_permissions_view AS
SELECT
  a.id AS user_id,
  r.id AS role_id,
  r.name AS role_name,
  p.id AS permission_id,
  p.name AS permission
FROM
  accounts a
JOIN
  user_roles ur ON ur.user_id = a.id
JOIN
  roles r ON r.id = ur.role_id
JOIN
  role_permissions rp ON rp.role_id = r.id
JOIN
  permissions p ON p.id = rp.permission_id;


CREATE OR REPLACE VIEW role_permissions_view AS
SELECT
  r.id AS role_id,
  r.name AS role_name,
  r.description AS role_description,
  p.id AS permission_id,
  p.name AS permission_name
FROM
  roles r
JOIN
  role_permissions rp ON rp.role_id = r.id
JOIN
  permissions p ON p.id = rp.permission_id;
-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd

DROP VIEW IF EXISTS user_roles_view;

DROP VIEW IF EXISTS user_permissions_view;

DROP VIEW IF EXISTS role_permissions_view;
