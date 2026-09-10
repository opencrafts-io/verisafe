-- +goose Up
-- +goose StatementBegin
select 'up SQL query'
;
-- +goose StatementEnd
CREATE TYPE public.charge_attempt_status AS ENUM (
    'pending',
    'success',
    'failure'
);


create table public.charge_attempts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id TEXT NOT NULL,
  status charge_attempt_status NOT NULL DEFAULT 'pending',
  payer_phone_number text NOT NULL,
  amount bigint NOT NULL DEFAULT 0,
  notes text,
  requested_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);

-- +goose Down
-- +goose StatementBegin
select 'down SQL query'
;
DROP TABLE IF EXISTS public.charge_attempts;
DROP TYPE public.charge_attempt_status;
-- +goose StatementEnd
