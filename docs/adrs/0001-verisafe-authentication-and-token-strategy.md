# 1. Verisafe authentication and token strategy

Date: 2026-03-11

## Status

proposed

## Context

Verisafe is the central authentication service for our platform. It issues internal JWTs for app authentication and manages OAuth2 tokens on behalf of users for third-party integrations (e.g. Google, Apple, Spotify).

We needed to define:

- How internal JWTs are managed and revoked
- How OAuth2 tokens are stored and accessed by downstream services
- How refresh tokens are structured to support mobile-first usage
- What database tables are required to support all of the above

## Decision

### 1. Internal Authentication — JWTs with Redis Blocklist
We use short-lived JWTs (15–30 min TTL) for internal authentication. JWTs are stateless and self-verifiable using Verisafe's secret/public key.

- JWTs are not stored in the database
- Each JWT carries a jti (UUID) claim embedded at issuance
- On revocation, the jti is written to a Redis blocklist with a TTL equal to the token's remaining lifetime
- Every service verifies requests by checking signature validity + Redis blocklist lookup
JWT metadata (jti, user_id, device_id) is recorded in the issued_tokens table for audit purposes

### 2. Refresh Tokens — Rotation with Reuse Detection
Since the primary client is mobile, users must remain authenticated across app launches without re-entering credentials. We implement long-lived opaque refresh tokens (90-day sliding expiry) with full rotation.

- Raw refresh tokens are never stored — only their sha256 hash
- Every use of a refresh token invalidates it and issues a new one (rotation)
- All tokens in a rotation chain share a family_id
- If a refresh token is used after already being consumed (used_at is set), this indicates token theft:

    - All tokens sharing the same family_id are immediately revoked
    - All associated issued_tokens rows are revoked
    - The user is forced to re-authenticate

### 3. OAuth2 Tokens — Encrypted DB + Redis Cache
OAuth2 tokens (access + refresh) obtained from third-party providers (Google etc.) are:

- Encrypted at rest (AES-256 or KMS) before being written to the database
- Cached in Redis with a TTL slightly shorter than the access token expiry
- Never published to RabbitMQ or included in event payloads
- Fetched by downstream services (e.g. Keepup) via Redis cache first, DB fallback


### 4.  RabbitMQ Events — No Tokens in Payloads

RabbitMQ is used to broadcast intent and identity only. Token values are never included in event payloads to prevent credential leakage into logs and message brokers.

```json
{
  "event": "user.oauth_connected",
  "userId": "uuid",
  "provider": "google",
  "scopes": [],
  "connectedAt": "2026-03-11T10:00:00Z"
}
```

### 5. Mobile Token Lifecycle
- On app launch, the client reads the refresh token from iOS Keychain / Android Keystore
- A silent refresh is performed to obtain a fresh JWT before any API calls
- An HTTP interceptor handles 401 responses mid-session by silently refreshing and retrying the original request
- Concurrent requests during a refresh are queued and retried once the new JWT is available
- On logout, the refresh token is revoked in the DB, the jti is added to the Redis blocklist, and the token is deleted from device storage


## Database Schema

`user_devices`

Tracks per-device sessions to support multi-device management.

```sql
CREATE TABLE user_devices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_name     TEXT,
    platform        TEXT,
    push_token      TEXT,
    last_active_at  TIMESTAMP,
    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);
```


`issued_tokens`
JWT audit trail and revocation metadata.

```sql
CREATE TABLE issued_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    jti             UUID UNIQUE NOT NULL,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id       UUID REFERENCES user_devices(id) ON DELETE SET NULL,
    issued_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMP NOT NULL,
    revoked_at      TIMESTAMP,
    last_used_at    TIMESTAMP
);
```


`refresh_tokens`
Core refresh token store with rotation chain and reuse detection.

```sql
CREATE TABLE refresh_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash      TEXT UNIQUE NOT NULL,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_id       UUID REFERENCES user_devices(id) ON DELETE SET NULL,
    jwt_jti         UUID REFERENCES issued_tokens(jti) ON DELETE SET NULL,
    issued_at       TIMESTAMP NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMP NOT NULL,
    used_at         TIMESTAMP,
    revoked_at      TIMESTAMP,
    replaced_by     UUID REFERENCES refresh_tokens(id),
    family_id       UUID NOT NULL DEFAULT gen_random_uuid()
);
```

## Consequences

### Positive
1. Stateless JWT verification keeps latency low — no DB call per request
2. Redis blocklist is self-cleaning via TTL, never grows unboundedly
3. Refresh token rotation with family_id provides automatic stolen token detection
4. OAuth2 tokens never travel through insecure channels
5. Mobile users remain authenticated indefinitely with normal app usage


### Negative
1. Redis becomes a required dependency for all services that verify JWTs
2. Refresh token rotation requires careful client-side handling to avoid race conditions on concurrent refresh calls
3. OAuth2 token encryption adds implementation overhead in Verisafe


## Alternatives Considered

| Option | Reason Rejected |
| :--- | :--- |
| Store full JWTs in the DB | Unnecessary overhead; JWTs are self-verifiable |
| Use RabbitMQ for token retrieval | Async — unsuitable for synchronous token lookups |
| Store OAuth Tokens in Redis only | Redis is volatile; tokens would be lost on flush/restart |
| Absolute refresh token expiry | Poor UX for mobile — forces re-login on active users |

