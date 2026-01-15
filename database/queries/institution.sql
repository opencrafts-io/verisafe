
-- name: CreateInstitution :one
INSERT INTO institutions (
    name, web_pages, domains, alpha_two_code, country, state_province
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: GetInstitution :one
SELECT * FROM institutions
WHERE institution_id = $1 LIMIT 1;

-- name: ListInstitutions :many
SELECT * FROM institutions
ORDER BY institution_id LIMIT $1 OFFSET $2;

-- name: UpdateInstitution :one
UPDATE institutions
SET 
    name = COALESCE(NULLIF(@name::varchar, ''), name),
    web_pages = COALESCE(NULLIF(@web_pages::text[], '{}'), web_pages),
    domains = COALESCE(NULLIF(@domains::text[], '{}'), domains),
    alpha_two_code = COALESCE(NULLIF(@alpha_two_code::char(2), ''), alpha_two_code),
    country = COALESCE(NULLIF(@country::varchar, ''), country),
    state_province = COALESCE(NULLIF(@state_province::varchar, ''), state_province)
WHERE institution_id = @institution_id
RETURNING *;

-- name: DeleteInstitution :exec
DELETE FROM institutions
WHERE institution_id = $1;


-- name: SearchInstitutionsByName :many
SELECT *
FROM institutions
WHERE lower(name) LIKE '%' || lower(@name::varchar) || '%'
ORDER BY name
LIMIT $1 OFFSET $2;




-- name: AddAccountInstitution :one
WITH ins AS (
  INSERT INTO account_institutions (account_id, institution_id)
  VALUES ($1, $2)
  ON CONFLICT DO NOTHING
  RETURNING *
)
SELECT * FROM ins
UNION
SELECT * FROM account_institutions
WHERE account_id = $1 AND institution_id = $2;


-- name: RemoveAccountInstitution :exec
DELETE FROM account_institutions
WHERE account_id = $1 AND institution_id = $2;

-- name: ListInstitutionsForAccount :many
SELECT i.*
FROM institutions i
JOIN account_institutions ai ON i.institution_id = ai.institution_id
WHERE ai.account_id = $1
ORDER BY i.name
LIMIT $2
OFFSET $3;

-- name: ListAccountsForInstitution :many
SELECT a.*
FROM accounts a
JOIN account_institutions ai ON a.id = ai.account_id
WHERE ai.institution_id = $1
ORDER BY a.name
LIMIT $2
OFFSET $3;


-- name: GetInstitutionsCount :one
-- Returns the number of all institutions in the system
SELECT count(*) from institutions;
