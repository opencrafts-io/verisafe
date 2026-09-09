-- name: CreateOrder :one
-- Create a new empty order and return the created record.
INSERT INTO public.orders (
    user_id,
    currency,
    metadata,
    expires_at
)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(currency),
    sqlc.arg(metadata),
    sqlc.narg(expires_at)
)
RETURNING *;


-- name: GetOrder :one
-- Retrieve one order by its order ID.
select *
from public.orders
where id = sqlc.arg(id)
;


-- name: GetUserOrder :one
-- Retrieve one order by ID, limited to the specified user.
select *
from public.orders
where id = sqlc.arg(id) and user_id = sqlc.arg(user_id)
;


-- name: ListOrdersByUser :many
-- List all orders belonging to a user.
select *
from public.orders
where user_id = sqlc.arg(user_id)
order by created_at desc
limit sqlc.arg(page_size)
offset sqlc.arg(page_offset)
;


-- name: ListOrdersByStatus :many
-- List orders with a specific status.
select *
from public.orders
where status = sqlc.arg(status)
order by created_at desc
limit sqlc.arg(page_size)
offset sqlc.arg(page_offset)
;


-- name: ListOrders :many
-- List all orders with pagination.
select *
from public.orders
order by created_at desc
limit sqlc.arg(page_size)
offset sqlc.arg(page_offset)
;


-- name: UpdateOrder :one
-- Update editable order details without changing financial totals.
UPDATE public.orders
SET
    currency = sqlc.arg(currency),
    metadata = sqlc.arg(metadata),
    expires_at = sqlc.narg(expires_at),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status NOT IN (
      'paid'::public.order_status,
      'cancelled'::public.order_status
  )
RETURNING *;


-- name: UpdateOrderStatus :one
-- Update the status of an order and return the updated order.
UPDATE public.orders
SET
    status = sqlc.arg(status),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;


-- name: MarkOrderPaid :one
-- Mark an order as paid and record the payment time.
UPDATE public.orders
SET
    status = 'paid'::public.order_status,
    paid_at = COALESCE(paid_at, now()),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status NOT IN (
      'paid'::public.order_status,
      'cancelled'::public.order_status
  )
RETURNING *;


-- name: CancelOrder :one
-- Cancel an order and record the cancellation time.
UPDATE public.orders
SET
    status = 'cancelled'::public.order_status,
    cancelled_at = COALESCE(cancelled_at, now()),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND status NOT IN (
      'paid'::public.order_status,
      'cancelled'::public.order_status
  )
RETURNING *;


-- name: CountOrdersByUser :one
-- Count all orders belonging to a user.
select count(*)::bigint
from public.orders
where user_id = sqlc.arg(user_id)
;


-- name: CountOrdersByUserAndStatus :one
-- Count orders with a specific status for a user.
select count(*)::bigint
from public.orders
where user_id = sqlc.arg(user_id) and status = sqlc.arg(status)
;


-- name: RecalculateOrderTotals :exec
-- Recalculate the financial totals for an order from its order items.
select public.recalculate_order_totals(sqlc.arg(order_id))
;
