-- +goose Up
-- +goose StatementBegin
select 'up SQL query'
;
-- +goose StatementEnd
CREATE TABLE IF NOT EXISTS public.order_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    TEXT NOT NULL REFERENCES public.orders(id),
    added_by    uuid NOT NULL REFERENCES accounts(id), 
    unit_price  bigint NOT NULL,  -- in cents
    discount    bigint NOT NULL, 
    quantity    smallint NOT NULL DEFAULT 1,
    tax         bigint NOT NULL DEFAULT 0, -- in cents

-- What is being bought product / plan
    plan_id     integer REFERENCES public.plans(id),

-- TODO: Extend later to add products instead of having
-- them now
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()


    CONSTRAINT order_items_quantity_positive
        CHECK (quantity > 0),

    CONSTRAINT order_items_unit_price_nonnegative
        CHECK (unit_price >= 0),

    CONSTRAINT order_items_discount_nonnegative
        CHECK (discount >= 0),

    CONSTRAINT order_items_tax_nonnegative
        CHECK (tax >= 0)
);

-- +goose Down
-- +goose StatementBegin
select 'down SQL query'
;

DROP TABLE IF EXISTS public.order_items;
-- +goose StatementEnd
