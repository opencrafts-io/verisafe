-- +goose Up
-- +goose StatementBegin
select 'up SQL query'
;
-- +goose StatementEnd
ALTER TABLE public.orders
    ADD COLUMN discount bigint NOT NULL DEFAULT 0,
    ADD COLUMN tax      bigint NOT NULL DEFAULT 0;


ALTER TABLE public.orders
ADD CONSTRAINT orders_amounts_nonnegative
CHECK (
    subtotal >= 0
    AND discount >= 0
    AND tax >= 0
    AND total >= 0
);


-- +goose StatementBegin
create or replace function public.recalculate_order_totals(p_order_id text)
returns void
language plpgsql
as $$
BEGIN
    UPDATE public.orders AS o
    SET
        subtotal = totals.subtotal,
        discount = totals.discount,
        tax      = totals.tax,
        total    = totals.subtotal - totals.discount + totals.tax,
        updated_at = now()
    FROM (
        SELECT
            COALESCE(SUM(unit_price * quantity), 0)::bigint AS subtotal,
            COALESCE(SUM(discount), 0)::bigint AS discount,
            COALESCE(SUM(tax), 0)::bigint AS tax
        FROM public.order_items
        WHERE order_id = p_order_id
    ) AS totals
    WHERE o.id = p_order_id;
END;
$$
;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
select 'down SQL query'
;

drop function if exists public.recalculate_order_totals(text)
;

ALTER TABLE public.orders
    DROP CONSTRAINT IF EXISTS orders_amounts_nonnegative;

ALTER TABLE public.orders
    DROP COLUMN IF EXISTS discount,
    DROP COLUMN IF EXISTS tax;
-- +goose StatementEnd
