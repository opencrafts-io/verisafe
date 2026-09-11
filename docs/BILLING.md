# Billing Flow

This document explains how Verisafe billing moves from plans, through orders
and checkout, into active subscriptions.

## Core Concepts

### Plans

Plans describe what a user can buy. A plan has a generated or caller-provided
`code`, a display `name`, `price`, `currency`, `billing_interval_days`,
visibility flags, and an optional description.

`active` controls whether a plan is sellable. `visible` controls whether normal
clients should show the plan in plan lists. A plan can be hidden while it is
being prepared or reserved for internal/partner flows.

Plan management is protected by the `Plans Manager` role and plan permissions
seeded in the plan migrations. The default `user` role is granted
`read:plan:any` so clients can browse available plans.

### Entitlements

Entitlements describe the capabilities a plan unlocks. They are keyed by plan
code and entitlement key, with a value, unit, and optional description. Feature
services should authorize premium behavior against entitlements rather than
checking payment providers or plan prices directly.

The architectural rule is:

```text
Billing owns plans and subscriptions.
Entitlements express feature access.
Feature services enforce entitlements.
```

### Orders

An order is the purchase container. It belongs to one account and moves through
these statuses:

| Status | Meaning |
| --- | --- |
| `pending` | The order can still be edited and charged. |
| `failed` | A previous charge failed; the order can be charged again. |
| `paid` | Payment succeeded. The order is complete. |
| `cancelled` | The user or an operator cancelled the order. |
| `expired` | The order is no longer chargeable. |

Amounts are stored in the smallest currency unit. For example, `KES` values are
stored as integer cents or the smallest practical denomination used by the
payment provider integration.

Order metadata is `jsonb`. Empty or `null` metadata is normalized to `{}`.
Array metadata is accepted when the client intentionally sends an array.

### Order Items

Order items are the billable lines inside an order. Today an item can reference
a plan through `plan_id`; the model leaves room for non-plan products later.

Each item stores:

| Field | Meaning |
| --- | --- |
| `unit_price` | Price per unit in the smallest currency unit. |
| `quantity` | Number of units. Must be greater than zero. |
| `discount` | Discount amount. Must be non-negative. |
| `tax` | Tax amount. Must be non-negative. |
| `plan_id` | Optional purchased plan. |

Order totals are derived from the items. After item changes, the order can be
recalculated through `POST /orders/{id}/totals/recalculation`.

## API Flow

### 1. Browse Plans

Clients list plans and choose a visible, active plan. Plan reads use the normal
authenticated API flow.

Typical route:

```text
GET /plans
```

### 2. Create an Order

The mobile or desktop app creates an order for the authenticated user:

```text
POST /orders
```

The handler ignores any client-provided `user_id` and uses the authenticated
subject as the owner. The default `user` role can create own orders with
`create:order:own`.

### 3. Add Order Items

The app adds one or more order items:

```text
POST /orders/{order_id}/items
```

Normal users need `create:order-item:own`. Billing managers use the `:any`
variant to operate across users.

### 4. Recalculate and Review

The app can recalculate totals and fetch the order before checkout:

```text
POST /orders/{order_id}/totals/recalculation
GET /orders/{order_id}
GET /orders/{order_id}/items
```

The order total must be greater than zero before charging.

### 5. Start Checkout

Browser checkout is intentionally separate from the mobile or desktop app
environment. The mobile app should not pass its normal API token into an
embedded browser.

Instead, the authenticated app creates a short-lived checkout handoff:

```text
POST /checkout-sessions
{
  "order_id": "ORD-ABC12345"
}
```

This requires:

| Permission | Role | Meaning |
| --- | --- | --- |
| `create:checkout-session:own` | `user` | Start checkout for the caller's own order. |
| `create:checkout-session:any` | `Billing Manager` | Start checkout for any order. |

The API verifies the order exists and that the caller can act on it. It then
stores a one-time handoff code in cache for five minutes and returns a checkout
URL:

```json
{
  "checkout_url": "https://api.example.test/checkout/start?code=...",
  "expires_at": "2026-09-11T06:25:43Z"
}
```

### 6. Exchange the Handoff in the Browser

The browser checkout app reads the code from the URL and exchanges it:

```text
POST /checkout-sessions/exchange
{
  "code": "..."
}
```

The code is one-time-use. On success, the API returns a specialized checkout
JWT. This token is short-lived, bound to one order, and carries checkout scopes:

| Scope | Allows |
| --- | --- |
| `checkout:order:read` | Read the bound checkout order. |
| `checkout:order-item:read` | Read items on the bound checkout order. |
| `checkout:order:charge` | Initiate payment for the bound checkout order. |

The token cannot be used as a normal app access token because checkout JWTs use
the checkout token type and checkout audience.

### 7. Complete Checkout

The browser checkout app uses the checkout JWT against the checkout-only routes:

```text
GET /checkout/orders/{order_id}
GET /checkout/orders/{order_id}/items
POST /checkout/orders/{order_id}/charge
```

The API checks that the token's `order_id` matches the path. A token minted for
one order cannot be replayed against another order.

Desktop clients can call the same checkout routes with a normal app JWT when
they are not constrained by app-store payment policies. In that case, the API
falls back to normal RBAC:

| Checkout action | Normal app permission |
| --- | --- |
| Read checkout order | `read:order:own` or `read:order:any` |
| Read checkout order items | `read:order-item:own` or `read:order-item:any` |
| Charge checkout order | `update:order:own` or `update:order:any` |

### 8. Charge Processing

Charging an order creates a charge attempt and publishes a Veribroke STK request
through RabbitMQ. The charge request includes the attempt ID as `request_id`,
the payer phone number, the amount from the order total, and `order_id` metadata.

An order is chargeable only when:

| Rule | Reason |
| --- | --- |
| Status is `pending` or `failed` | Completed, cancelled, and expired orders must not be charged. |
| `expires_at` is empty or in the future | Expired checkout windows should not collect payment. |
| `total` is greater than zero | Zero-value charges are rejected. |
| Payer phone number is present | M-Pesa STK push needs a target phone number. |
| No pending attempt exists | Prevents duplicate live payment prompts. |

If the publish to Veribroke fails, the charge attempt is marked `failure` and
the API returns an unavailable response.

### 9. Charge Result and Subscription Activation

Veribroke sends the result back to the billing service. The charge result
handler validates the request ID and status, loads the charge attempt, and
ignores duplicate results for already-resolved attempts.

On failure, only the charge attempt is resolved as failed.

On success, the service performs one database transaction:

```text
resolve charge attempt as success
mark order as paid
create subscriptions for distinct plan items on the paid order
```

Subscriptions are created from the paid order's plan items. The subscription
period end is calculated from the plan's `billing_interval_days`.

The database enforces one active subscription per user. If a user already has an
active subscription, activation for a new paid order is ignored by the conflict
handler until subscription upgrade/renewal behavior is explicitly added.

## Permissions Summary

### Default User

The default `user` role can:

| Area | Permissions |
| --- | --- |
| Plans | `read:plan:any` |
| Orders | `create:order:own`, `read:order:own`, `update:order:own` |
| Order items | `create:order-item:own`, `read:order-item:own`, `update:order-item:own`, `delete:order-item:own` |
| Checkout | `create:checkout-session:own` |

Checkout JWT scopes are also seeded as permissions for visibility in RBAC
records, but the browser checkout flow receives them as JWT scopes from the
session exchange rather than from a database role lookup.

### Billing Manager

The `Billing Manager` role can operate across users:

| Area | Permissions |
| --- | --- |
| Orders | `create:order:any`, `read:order:any`, `update:order:any` |
| Order items | `create:order-item:any`, `read:order-item:any`, `update:order-item:any`, `delete:order-item:any` |
| Checkout | `create:checkout-session:any` |

`system` and `Administrator` receive new permissions automatically through the
global permission insert triggers documented in `docs/RBAC.md`.

## Security Properties

The checkout flow is designed around two environments:

| Environment | Token |
| --- | --- |
| Mobile or desktop app | Normal app access token. |
| Browser checkout app | Specialized checkout JWT from a one-time code exchange. |

The mobile app never needs to expose its normal API token to the embedded
browser. The browser receives only enough authority to read and charge one
specific order for a short period.

Important details:

| Control | Behavior |
| --- | --- |
| One-time handoff | The exchange code is deleted after use. |
| Short TTLs | Handoff codes live for five minutes; checkout JWTs live for fifteen minutes. |
| Hashed cache keys | Raw handoff codes are not used directly as cache keys. |
| Order binding | Checkout JWTs include `order_id`; every checkout route checks it. |
| Scoped authority | Checkout JWTs need the matching checkout scope for each action. |
| Token type separation | Invalid checkout-looking tokens do not fall back to normal JWT validation. |

## Operational Notes

Billing depends on the cache for checkout handoffs and on RabbitMQ for payment
requests and results. If either dependency is unavailable, checkout session
creation or charge initiation can fail even when the order itself is valid.

Payment success is accepted only from the Veribroke result path. Clients can
start a charge and observe order/subscription state, but they cannot mark an
order paid.

The current implementation activates subscriptions from paid plan items but does
not yet implement upgrade proration, renewal extension, refunds, or cancellation
workflows beyond the stored subscription fields.
