-- name: GetPendingChargeAttemptsByOrder :many
SELECT *
FROM charge_attempts
WHERE order_id = sqlc.arg(order_id)
  AND status = 'pending'
ORDER BY requested_at DESC
LIMIT sqlc.arg(page_size)
OFFSET sqlc.arg(page_offset);

-- name: CreateChargeAttempt :one
WITH chargeable_order AS (
    SELECT o.id, o.total
    FROM public.orders AS o
    WHERE o.id = sqlc.arg(order_id)
      AND o.status IN (
          'pending'::public.order_status,
          'failed'::public.order_status
      )
      AND (o.expires_at IS NULL OR o.expires_at > now())
      AND o.total > 0
    FOR UPDATE
)
INSERT INTO charge_attempts (id, order_id, payer_phone_number, amount)
SELECT
    sqlc.arg(id),
    chargeable_order.id,
    sqlc.arg(payer_phone_number),
    sqlc.arg(amount)
FROM chargeable_order
WHERE chargeable_order.total = sqlc.arg(amount)
RETURNING *;

-- name: GetChargeAttempt :one
SELECT *
FROM charge_attempts
WHERE id = sqlc.arg(id);

-- name: ResolveChargeAttempt :one
UPDATE charge_attempts
SET
    status = sqlc.arg(status),
    notes = sqlc.narg(notes),
    resolved_at = now()
WHERE id = sqlc.arg(id)
  AND status = 'pending'
RETURNING *;
