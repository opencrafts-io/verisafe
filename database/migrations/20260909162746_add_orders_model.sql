-- +goose Up
-- +goose StatementBegin
select 'up SQL query'
;
-- +goose StatementEnd
CREATE TYPE public.order_status AS ENUM (
    'pending',
    'paid',
    'failed',
    'cancelled',
    'expired'
);


-- +goose StatementBegin
create or replace function public.generate_order_code()
returns text
language plpgsql
as $$
DECLARE
    code text;
BEGIN
    LOOP
        code := 'ORD-' || (
            SELECT string_agg(
                substr('ABCDEFGHJKLMNPQRSTUVWXYZ23456789', 
                       floor(random() * 32 + 1)::int, 1),
                ''
            )
            FROM generate_series(1, 8)
        );

        EXIT WHEN NOT EXISTS (
            SELECT 1 FROM orders WHERE id = code
        );
    END LOOP;

    RETURN code;
END;
$$
;
-- +goose StatementEnd
CREATE TABLE public.orders (
    id           text PRIMARY KEY DEFAULT generate_order_code(),
    user_id      uuid NOT NULL REFERENCES accounts(id),
    status       public.order_status NOT NULL DEFAULT 'pending',

-- Amounts are in cents/ the lowest denomination
    subtotal     bigint NOT NULL,
    total        bigint NOT NULL,
    currency     varchar(3) NOT NULL DEFAULT 'KES',
    metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    paid_at      timestamptz,
    cancelled_at timestamptz,
    expires_at   timestamptz
);

CREATE INDEX idx_orders_user_id
    ON public.orders(user_id);

CREATE INDEX idx_orders_status
    ON public.orders(status);

CREATE INDEX idx_orders_created_at
    ON public.orders(created_at DESC);

-- +goose Down
-- +goose StatementBegin
select 'down SQL query'
;

DROP TABLE public.orders;
DROP TYPE public.order_status;
drop function public.generate_order_code()
;
-- +goose StatementEnd
