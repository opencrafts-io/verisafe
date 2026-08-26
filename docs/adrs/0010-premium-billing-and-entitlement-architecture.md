# 10. Premium Billing and Entitlement Architecture

Date: 2026-08-12

## Status

Accepted

## Context

The application is already in production and uses a microservice architecture with a dedicated authentication service and feature-specific services.

We need to introduce Premium subscriptions without coupling feature services to a specific payment provider. The initial payment provider will be **M-Pesa STK Push**, with checkout handled on the web.

Premium access must also support future providers such as Google Play Billing and Apple IAP without changing feature-service authorization.

## Decision

Separate **billing**, **entitlements**, and **authentication**.

```text
Auth Service
    |
    | user identity
    v
Billing Service
    |
    | subscription events
    v
Entitlements
    |
    | authorization
    v
Feature Services
```

### Billing owns

```text
plans
subscriptions
payments
```

Core models:

```text
plans
-----
id
code
price
currency
billing_interval
active

subscriptions
-------------
id
user_id
plan_id
status
started_at
current_period_start
current_period_end
cancel_at_period_end
cancelled_at

payments
--------
id
user_id
subscription_id
provider
provider_transaction_id
amount
currency
status
created_at
completed_at
```

Do **not** add `users.is_premium`.

A user is Premium because they have an active subscription associated with a Premium plan.

### Entitlements

Feature access is represented using explicit entitlements:

```text
ai.study
ai.study.unlimited
anki.unlimited
analytics.advanced
```

Feature services authorize against entitlements:

```go
if !authorizer.HasEntitlement(ctx, "ai.study.unlimited") {
    return ErrPremiumRequired
}
```

They must not query billing databases or check payment-provider state directly.

### M-Pesa flow

```text
Web
 ↓
Billing API
 ↓
Create pending payment
 ↓
M-Pesa STK Push
 ↓
M-Pesa callback
 ↓
Verify + process idempotently
 ↓
Payment completed
 ↓
Subscription activated
 ↓
Entitlements granted
```

The client must never be trusted to mark a payment as successful.

M-Pesa callbacks must be **idempotent** using provider transaction identifiers.

### Events

Billing publishes subscription lifecycle events through RabbitMQ:

```text
subscription.activated
subscription.renewed
subscription.cancelled
subscription.expired
```

Entitlement state is updated from these events.

Consumers must be idempotent.

### Client feature gates

Premium features will remain visible in the application:

```text
Premium
AI Study
Unlimited AI sessions
```

The client can use entitlement data to show/disable premium functionality, but this is **UX only**.

The backend remains the security boundary:

```text
Client
  ↓
Feature Service
  ↓
Check entitlement
  ↓
Allow / PremiumRequired
```

### Payment providers

Billing will use a provider abstraction so additional providers can be added later:

```go
type PaymentProvider interface {
    InitiatePayment(ctx context.Context, req PaymentRequest) (PaymentResult, error)
    VerifyPayment(ctx context.Context, transactionID string) (PaymentStatus, error)
}
```

Initial implementation:

```text
MpesaProvider
```

Future implementations:

```text
GooglePlayProvider
AppleProvider
StripeProvider
```

All providers ultimately produce the same internal subscription and entitlement state.

### Existing users

Users without an active subscription are treated as Free.

No subscription row needs to be created for every existing production user.

```text
active subscription → Premium entitlements
no active subscription → Free entitlements
```

## Consequences

### Positive

* Feature services remain independent of payment providers.
* M-Pesa can be introduced without spreading M-Pesa logic across the system.
* Google Play/Apple can be added later without redesigning feature authorization.
* Premium access works across web and mobile clients.
* Feature access is granular rather than a single `is_premium` flag.
* Payment processing is resilient to duplicate callbacks.
* Existing production users require minimal migration.

### Negative

* More complexity than a simple Premium boolean.
* Subscription expiry, cancellation, renewal, and failed payments must be handled.
* Entitlement caching can introduce stale authorization and requires invalidation/expiry.
* Billing and entitlement events require reliable, idempotent processing.
