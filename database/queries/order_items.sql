-- name: CreateOrderItem :one
-- Create a new order item and return the created record.
INSERT INTO public.order_items (
    order_id,
    added_by,
    unit_price,
    discount,
    quantity,
    tax,
    plan_id
)
VALUES (
    sqlc.arg(order_id),
    sqlc.arg(added_by),
    sqlc.arg(unit_price),
    sqlc.arg(discount),
    sqlc.arg(quantity),
    sqlc.arg(tax),
    sqlc.narg(plan_id)
)
RETURNING *
;


-- name: GetOrderItem :one
-- Retrieve one order item by its ID.
select *
from public.order_items
where id = sqlc.arg(id)
;


-- name: ListOrderItemsByOrder :many
-- Retrieve all items belonging to an order.
select *
from public.order_items
where order_id = sqlc.arg(order_id)
order by created_at asc
;


-- name: UpdateOrderItem :one
-- Update an existing order item and return the updated record.
UPDATE public.order_items
SET
    unit_price = sqlc.arg(unit_price),
    discount = sqlc.arg(discount),
    quantity = sqlc.arg(quantity),
    tax = sqlc.arg(tax),
    plan_id = sqlc.narg(plan_id),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *
;


-- name: DeleteOrderItem :exec
-- Delete one order item by its ID.
delete from public.order_items
where id = sqlc.arg(id)
;


-- name: DeleteOrderItemsByOrder :exec
-- Delete all items belonging to an order.
delete from public.order_items
where order_id = sqlc.arg(order_id)
;
