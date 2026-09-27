-- name: CreateSubscriptionsForPaidOrder :exec
-- Activate each plan included in a paid order. The partial unique index on
-- subscriptions keeps a user from receiving a second simultaneous active plan.
WITH paid_order AS (
    SELECT user_id
    FROM public.orders
    WHERE orders.id = sqlc.arg(order_id)
      AND orders.status = 'paid'::public.order_status
), plan_items AS (
    SELECT DISTINCT plan_id
    FROM public.order_items
    WHERE order_id = sqlc.arg(order_id)
      AND plan_id IS NOT NULL
)
INSERT INTO public.subscriptions (
    user_id,
    plan_id,
    current_period_end
)
SELECT
    paid_order.user_id,
    plan_items.plan_id,
    now() + make_interval(days => plans.billing_interval_days)
FROM paid_order
JOIN plan_items ON true
JOIN public.plans ON plans.id = plan_items.plan_id
ON CONFLICT (user_id) WHERE status = 'active'::public.subscription_status
DO NOTHING;

-- name: GetActiveSubscriptionByUser :one
-- Retrieves the caller's currently valid active subscription and its plan.
SELECT
    subscriptions.id,
    subscriptions.plan_id,
    plans.code AS plan_code,
    plans.name AS plan_name,
    subscriptions.status,
    subscriptions.started_at,
    subscriptions.current_period_start,
    subscriptions.current_period_end,
    subscriptions.cancel_at_period_end,
    subscriptions.cancelled_at
FROM public.subscriptions
JOIN public.plans ON plans.id = subscriptions.plan_id
WHERE subscriptions.user_id = sqlc.arg(user_id)
  AND subscriptions.status = 'active'::public.subscription_status
  AND (
      subscriptions.current_period_end IS NULL
      OR subscriptions.current_period_end > now()
  )
ORDER BY subscriptions.started_at DESC
LIMIT 1;
