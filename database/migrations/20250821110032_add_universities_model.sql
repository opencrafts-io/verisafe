-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
CREATE TABLE institutions (
    institution_id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    web_pages TEXT[],
    domains TEXT[],
    alpha_two_code CHAR(2),
    country VARCHAR(100),
    state_province VARCHAR(100)   
);

CREATE TABLE account_institutions (
  account_id UUID NOT NULL,
  institution_id INT NOT NULL,
  PRIMARY KEY (account_id, institution_id),
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
  FOREIGN KEY (institution_id) REFERENCES institutions(institution_id) ON DELETE CASCADE
);


CREATE VIEW account_institution_info AS
SELECT 
    a.id AS account_id,
    a.name AS account_name,
    a.email AS account_email,
    a.created_at AS account_created_at,
    a.updated_at AS account_updated_at,
    i.institution_id,
    i.name AS institution_name,
    i.country AS institution_country,
    i.state_province AS institution_state,
    i.alpha_two_code AS institution_country_code
FROM 
    accounts a
JOIN 
    account_institutions ai ON ai.account_id = a.id  -- Link accounts to institutions
JOIN 
    institutions i ON ai.institution_id = i.institution_id;  -- Get institution details


-- Permissions for institutions manipulation
INSERT INTO permissions (name, description) VALUES
  ('list:institutions:any', 'Permission to list all institutions'),
  ('create:institutions:any', 'Permission to create any institution'),
  ('update:institutions:any', 'Permission to update any institution'),
  ('delete:institutions:any', 'Permission to delete an institution')
ON CONFLICT (name) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'user'
  AND p.name IN (
    'list:institutions:any'
  )
ON CONFLICT DO NOTHING;



-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd
DELETE FROM permissions
WHERE name IN (
  'list:institutions:any',
  'create:institutions:any',
  'update:institutions:any',
  'delete:institutions:any'
);

DROP VIEW IF EXISTS account_institution_info;
DROP TABLE IF EXISTS account_institutions;
DROP TABLE IF EXISTS institutions;
