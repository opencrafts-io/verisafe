-- name: CreateOrderItem :one
-- Create a new item for an editable order and return the created record.
WITH editable_order AS (
    SELECT o.id
    FROM public.orders AS o
    WHERE o.id = sqlc.arg(order_id)
      AND o.status NOT IN (
          'paid'::public.order_status,
          'cancelled'::public.order_status
      )
      AND NOT EXISTS (
          SELECT 1
          FROM public.charge_attempts AS ca
          WHERE ca.order_id = o.id
            AND ca.status = 'pending'::public.charge_attempt_status
      )
    FOR UPDATE
)
INSERT INTO public.order_items (
    order_id,
    added_by,
    unit_price,
    discount,
    quantity,
    tax,
    plan_id
)
SELECT
    editable_order.id,
    sqlc.arg(added_by),
    sqlc.arg(unit_price),
    sqlc.arg(discount),
    sqlc.arg(quantity),
    sqlc.arg(tax),
    sqlc.narg(plan_id)
FROM editable_order
RETURNING *;


-- name: GetOrderItem :one
-- Retrieve one order item by its ID.
select *
from public.order_items
where id = sqlc.arg(id)
  AND order_id = sqlc.arg(order_id)
;


-- name: ListOrderItemsByOrder :many
-- Retrieve all items belonging to an order.
select *
from public.order_items
where order_id = sqlc.arg(order_id)
order by created_at asc
;


-- name: UpdateOrderItem :one
-- Update an item only while its parent order is editable.
WITH editable_order AS (
    SELECT o.id
    FROM public.orders AS o
    WHERE o.id = sqlc.arg(order_id)
      AND o.status NOT IN (
          'paid'::public.order_status,
          'cancelled'::public.order_status
      )
      AND NOT EXISTS (
          SELECT 1
          FROM public.charge_attempts AS ca
          WHERE ca.order_id = o.id
            AND ca.status = 'pending'::public.charge_attempt_status
      )
    FOR UPDATE
)
UPDATE public.order_items AS oi
SET
    unit_price = sqlc.arg(unit_price),
    discount = sqlc.arg(discount),
    quantity = sqlc.arg(quantity),
    tax = sqlc.arg(tax),
    plan_id = sqlc.narg(plan_id),
    updated_at = now()
WHERE oi.id = sqlc.arg(id)
  AND oi.order_id = editable_order.id
RETURNING oi.*;

-- name: DeleteOrderItem :one
-- Delete an item only while its parent order is editable.
WITH editable_order AS (
    SELECT o.id
    FROM public.orders AS o
    WHERE o.id = sqlc.arg(order_id)
      AND o.status NOT IN (
          'paid'::public.order_status,
          'cancelled'::public.order_status
      )
      AND NOT EXISTS (
          SELECT 1
          FROM public.charge_attempts AS ca
          WHERE ca.order_id = o.id
            AND ca.status = 'pending'::public.charge_attempt_status
      )
    FOR UPDATE
)
DELETE FROM public.order_items AS oi
USING editable_order
WHERE oi.id = sqlc.arg(id)
  AND oi.order_id = editable_order.id
RETURNING oi.*;

-- name: DeleteOrderItemsByOrder :exec
-- Delete all items only while the parent order is editable.
WITH editable_order AS (
    SELECT o.id
    FROM public.orders AS o
    WHERE o.id = sqlc.arg(order_id)
      AND o.status NOT IN (
          'paid'::public.order_status,
          'cancelled'::public.order_status
      )
      AND NOT EXISTS (
          SELECT 1
          FROM public.charge_attempts AS ca
          WHERE ca.order_id = o.id
            AND ca.status = 'pending'::public.charge_attempt_status
      )
    FOR UPDATE
)
DELETE FROM public.order_items AS oi
USING editable_order
WHERE oi.order_id = editable_order.id;
