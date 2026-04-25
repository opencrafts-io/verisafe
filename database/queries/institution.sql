-- name: CreateInstitution :one
INSERT INTO institutions (
    name, web_pages, domains, alpha_two_code, country, state_province
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: GetInstitution :one
select *
from institutions
where institution_id = $1
limit 1
;

-- name: ListInstitutions :many
select *
from institutions
order by institution_id
limit $1
offset $2
;

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
delete from institutions
where institution_id = $1
;


-- name: SearchInstitutionsByName :many
select *
from institutions
where lower(name) like '%' || lower(@name::varchar) || '%'
order by name
limit $1
offset $2
;


-- name: AddAccountInstitution :one
with
    ins as (
        insert into account_institutions(account_id, institution_id)
        values ($1, $2) on conflict do nothing
        returning *
    )
select *
from ins
union
select *
from account_institutions
where account_id = $1 and institution_id = $2
;

-- name: ListInstitutionConnections :many
select *
from account_institutions
limit $1
offset $2
;


-- name: RemoveAccountInstitution :exec
delete from account_institutions
where account_id = $1 and institution_id = $2
;

-- name: ListInstitutionsForAccount :many
select i.*
from institutions i
join account_institutions ai on i.institution_id = ai.institution_id
where ai.account_id = $1
order by i.name
limit $2
offset $3
;

-- name: ListAccountsForInstitution :many
select a.*
from accounts a
join account_institutions ai on a.id = ai.account_id
where ai.institution_id = $1
order by a.name
limit $2
offset $3
;


-- name: GetInstitutionsCount :one
-- Returns the number of all institutions in the system
select count(*)
from institutions
;
