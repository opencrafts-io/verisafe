-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.recalculate_order_totals(p_order_id text)
RETURNS void
LANGUAGE plpgsql
AS $$
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
    WHERE o.id = p_order_id
      AND o.status NOT IN (
          'paid'::public.order_status,
          'cancelled'::public.order_status
      );
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.recalculate_order_totals(p_order_id text)
RETURNS void
LANGUAGE plpgsql
AS $$
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
$$;
-- +goose StatementEnd
