-- name: GetPendingChargeAttemptByOrder :one
select *
from charge_attempts
where order_id = $1 and status = 'pending'
order by requested_at desc
limit $2
offset $3
;

-- name: CreateChargeAttempt :one
INSERT INTO charge_attempts (id, order_id, payer_phone_number, amount)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetChargeAttempt :one
select *
from charge_attempts
where id = $1
;
