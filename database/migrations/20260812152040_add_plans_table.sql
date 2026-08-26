-- +goose Up
-- +goose StatementBegin
select 'up SQL query'
;
-- +goose StatementEnd
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- +goose StatementBegin
create or replace function generate_plan_code()
returns text
language sql
as $$
  SELECT 'PLN-' || upper(substr(replace(gen_random_uuid()::text, '-', ''), 1, 8));
$$
;
-- +goose StatementEnd
CREATE TABLE IF NOT EXISTS public.plans (
  id  SERIAL PRIMARY KEY,
  code TEXT NOT NULL UNIQUE DEFAULT generate_plan_code(),
  price BIGINT NOT NULL DEFAULT 0,  -- in cents / smallest denomination possible
  currency VARCHAR(3) NOT NULL DEFAULT 'KES', -- iso
  billing_interval SMALLINT NOT NULL, -- in days
  active BOOLEAN DEFAULT TRUE,
  visible BOOLEAN DEFAULT TRUE, -- whether the plan is visible to general public
  description TEXT,
  created_by UUID REFERENCES accounts(id) ON DELETE SET NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
-- +goose StatementBegin
select 'down SQL query'
;

DROP TABLE public.plans;
drop function if exists generate_plan_code()
;
-- +goose StatementEnd
