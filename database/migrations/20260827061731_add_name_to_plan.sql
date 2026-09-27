-- +goose Up
-- +goose StatementBegin
ALTER TABLE public.plans
  ADD COLUMN name TEXT;

UPDATE public.plans
SET name = COALESCE(description, 'Unnamed Plan')
WHERE name IS NULL;

ALTER TABLE public.plans
  ALTER COLUMN name SET NOT NULL;

ALTER TABLE public.plans
  RENAME billing_interval
  TO billing_interval_days;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
ALTER TABLE public.plans
  RENAME billing_interval_days
  TO billing_interval;

ALTER TABLE public.plans
  DROP COLUMN name;
-- +goose StatementEnd
