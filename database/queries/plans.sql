-- name: ListPlans :many
-- Retrieves plans.
-- No pagination logic is inserted as we don't anticipate having many plans
-- at the moment.
select
    code,
    price,
    currency,
    billing_interval,
    active,
    visible,
    description,
    created_by,
    created_at,
    updated_at
from public.plans
where sqlc.narg('visible')::bool is null or visible = sqlc.narg('visible')::bool
;

-- name: GetPlanByCode :one
-- Retrieves a plan by its code
select
    code,
    price,
    currency,
    billing_interval,
    active,
    visible,
    description,
    created_by,
    created_at,
    updated_at
from public.plans
where code = $1
;
;


-- name: CreatePlan :one
-- Creates a plan and returns its details.
INSERT INTO public.plans (
    code,
    price,
    currency,
    billing_interval,
    active,
    visible,
    created_by,
    description
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8
)
RETURNING
    code,
    price,
    currency,
    billing_interval,
    active,
    visible,
    description,
    created_by,
    created_at,
    updated_at;

-- name: UpdatePlan :one
-- Updates only the provided fields and returns the updated plan.
UPDATE public.plans
SET
    price = COALESCE(sqlc.narg('price'), price),
    currency = COALESCE(sqlc.narg('currency'), currency),
    billing_interval = COALESCE(sqlc.narg('billing_interval'), billing_interval),
    active = COALESCE(sqlc.narg('active'), active),
    visible = COALESCE(sqlc.narg('visible'), visible),
    description = COALESCE(sqlc.narg('description'), description),
    updated_at = NOW()
WHERE code = sqlc.arg('code')
RETURNING
    code,
    price,
    currency,
    billing_interval,
    active,
    visible,
    description,
    created_by,
    created_at,
    updated_at;
