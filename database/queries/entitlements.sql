-- name: ListEntitlementsByPlanCode :many
-- Retrieves all entitlements for a plan, identified by its public code.
select
    p.code as plan_code,
    e.key,
    e.value,
    e.unit,
    e.description,
    e.created_at,
    e.updated_at
from public.entitlements e
join public.plans p on p.id = e.plan_id
where p.code = $1
order by e.key
;

-- name: GetEntitlement :one
-- Retrieves a single entitlement by plan code and key.
select
    p.code as plan_code,
    e.key,
    e.value,
    e.unit,
    e.description,
    e.created_at,
    e.updated_at
from public.entitlements e
join public.plans p on p.id = e.plan_id
where p.code = $1 and e.key = $2
;

-- name: CreateEntitlement :one
-- Creates a new entitlement under the plan identified by its public code.
-- Resolves plan_id internally so callers never see or supply it.
insert into public.entitlements (
    plan_id,
    key,
    value,
    unit,
    description
)
select
    p.id,
    sqlc.arg('key'),
    sqlc.arg('value'),
    sqlc.arg('unit'),
    sqlc.narg('description')
from public.plans p
where p.code = sqlc.arg('plan_code')
returning
    (select code from public.plans where id = entitlements.plan_id) as plan_code,
    key,
    value,
    unit,
    description,
    created_at,
    updated_at
;

-- name: UpdateEntitlement :one
-- Updates an entitlement identified by plan code and key.
-- Only non-null arguments overwrite existing values.
update public.entitlements e
set
    value = coalesce(sqlc.narg('value'), e.value),
    unit = coalesce(sqlc.narg('unit'), e.unit),
    description = coalesce(sqlc.narg('description'), e.description),
    updated_at = current_timestamp
from public.plans p
where p.id = e.plan_id
  and p.code = sqlc.arg('plan_code')
  and e.key = sqlc.arg('key')
returning
    p.code as plan_code,
    e.key,
    e.value,
    e.unit,
    e.description,
    e.created_at,
    e.updated_at
;

-- name: DeleteEntitlement :exec
-- Deletes an entitlement identified by plan code and key.
delete from public.entitlements e
using public.plans p
where p.id = e.plan_id and p.code = $1 and e.key = $2
;

-- name: DeleteEntitlementsByPlanCode :exec
-- Deletes all entitlements for a plan, identified by its public code.
-- Useful when replacing a plan's entire entitlement set atomically.
delete from public.entitlements e
using public.plans p
where p.id = e.plan_id and p.code = $1
;
