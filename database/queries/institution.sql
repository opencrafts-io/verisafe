
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
