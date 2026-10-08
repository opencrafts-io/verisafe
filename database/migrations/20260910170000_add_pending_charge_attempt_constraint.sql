-- +goose Up
CREATE UNIQUE INDEX charge_attempts_one_pending_per_order_idx
    ON public.charge_attempts (order_id)
    WHERE status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS public.charge_attempts_one_pending_per_order_idx;
