-- name: GetPendingChargeAttemptsByOrder :many
SELECT *
FROM charge_attempts
WHERE order_id = sqlc.arg(order_id)
  AND status = 'pending'
ORDER BY requested_at DESC
LIMIT sqlc.arg(page_size)
OFFSET sqlc.arg(page_offset);

-- name: CreateChargeAttempt :one
INSERT INTO charge_attempts (id, order_id, payer_phone_number, amount)
VALUES (
    sqlc.arg(id),
    sqlc.arg(order_id),
    sqlc.arg(payer_phone_number),
    sqlc.arg(amount)
)
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
