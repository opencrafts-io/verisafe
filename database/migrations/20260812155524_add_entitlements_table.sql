-- +goose Up
-- +goose StatementBegin
select 'up SQL query'
;
-- +goose StatementEnd
CREATE TYPE public.entitlement_unit AS ENUM (
  'gb',
  'mb',
  'kb',
  'requests',
  'days',
  'months',
  'hours',
  'minutes',
  'credits',
  'api_calls'
);

CREATE TABLE IF NOT EXISTS public.entitlements (
  id BIGSERIAL PRIMARY KEY,
  plan_id BIGINT NOT NULL REFERENCES public.plans(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  value NUMERIC NOT NULL,
  unit public.entitlement_unit NOT NULL,
  description TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (plan_id, key)
);

CREATE INDEX IF NOT EXISTS idx_entitlements_plan_id ON public.entitlements(plan_id);
CREATE INDEX IF NOT EXISTS idx_entitlements_key ON public.entitlements(key);

-- +goose Down
-- +goose StatementBegin
select 'down SQL query'
;

DROP TABLE IF EXISTS public.entitlements;
DROP TYPE IF EXISTS public.entitlement_unit;
-- +goose StatementEnd
