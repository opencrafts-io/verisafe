# Service Tokens Implementation

This document describes the implementation of service API keys using industry-standard patterns including rotation, expiry, and security measures.

## Overview

Service tokens are API keys that can only be issued to bot accounts and provide secure access to the Verisafe API. They implement industry-standard security patterns including:

- **Token Rotation**: Automatic and manual token rotation capabilities
- **Expiry Management**: Configurable expiration dates
- **Usage Limits**: Optional usage count limits
- **IP Whitelisting**: Restrict access to specific IP addresses
- **User Agent Validation**: Validate client user agents
- **Scope-based Permissions**: Fine-grained permission control
- **Audit Logging**: Comprehensive usage tracking

## Database Schema

### Enhanced Service Tokens Table

The `service_tokens` table includes the following fields:

```sql
CREATE TABLE service_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  description TEXT,
  token_hash TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  rotated_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ,
  scopes TEXT[], -- Array of permission scopes
  max_uses INTEGER, -- Maximum number of uses (NULL = unlimited)
  use_count INTEGER DEFAULT 0, -- Current usage count
  rotation_policy JSONB, -- Rotation policy configuration
  ip_whitelist TEXT[], -- Allowed IP addresses (NULL = any)
  user_agent_pattern TEXT, -- Allowed user agent pattern
  created_by UUID REFERENCES accounts(id), -- Who created this token
  metadata JSONB -- Additional metadata
);
```

### Key Features

1. **Token Hash**: Tokens are stored as SHA256 hashes for security
2. **Usage Tracking**: `use_count` tracks how many times the token has been used
3. **Rotation Policy**: JSONB field stores rotation configuration
4. **IP Whitelist**: Array of allowed IP addresses
5. **User Agent Pattern**: Regex pattern for validating user agents
6. **Metadata**: Flexible JSONB field for additional data

## API Endpoints

### Service Token Management

#### Create Service Token
```http
POST /api/v1/service-tokens
Authorization: Bearer <jwt_token>
Content-Type: application/json

{
  "name": "Production API Key",
  "description": "API key for production services",
  "expires_in_days": 365,
  "scopes": ["read:data", "write:data"],
  "max_uses": 1000,
  "rotation_policy": {
    "auto_rotate": true,
    "rotation_interval_days": 90,
    "notify_before_days": 7
  },
  "ip_whitelist": ["192.168.1.100", "10.0.0.50"],
  "user_agent_pattern": "MyApp/.*",
  "metadata": {
    "environment": "production",
    "team": "backend"
  }
}
```

#### List Service Tokens
```http
GET /api/v1/service-tokens
Authorization: Bearer <jwt_token>
```

#### Get Service Token
```http
GET /api/v1/service-tokens/{id}
Authorization: Bearer <jwt_token>
```

#### Update Service Token
```http
PUT /api/v1/service-tokens/{id}
Authorization: Bearer <jwt_token>
Content-Type: application/json

{
  "name": "Updated API Key",
  "description": "Updated description",
  "scopes": ["read:data"],
  "max_uses": 500
}
```

#### Rotate Service Token
```http
POST /api/v1/service-tokens/{id}/rotate
Authorization: Bearer <jwt_token>
```

#### Revoke Service Token
```http
DELETE /api/v1/service-tokens/{id}
Authorization: Bearer <jwt_token>
```

#### Get Service Token Statistics
```http
GET /api/v1/service-tokens/stats
Authorization: Bearer <jwt_token>
```

### Admin Endpoints

#### List All Service Tokens (Admin)
```http
GET /api/v1/admin/service-tokens
Authorization: Bearer <jwt_token>
```

#### Cleanup Expired Tokens (Admin)
```http
POST /api/v1/admin/service-tokens/cleanup
Authorization: Bearer <jwt_token>
```

## Authentication

### Using Service Tokens

Service tokens are used via the `X-API-Key` header:

```http
GET /api/v1/some-endpoint
X-API-Key: vst_<token_value>
```

### Token Validation

The authentication middleware performs comprehensive validation:

1. **Token Existence**: Verifies the token exists in the database
2. **Revocation Check**: Ensures the token hasn't been revoked
3. **Expiration Check**: Validates the token hasn't expired
4. **Usage Limits**: Checks if usage limits have been exceeded
5. **IP Whitelist**: Validates the client IP against whitelist
6. **User Agent**: Validates the user agent against the pattern
7. **Account Type**: Ensures only bot accounts can use service tokens

## Security Features

### Token Generation

Tokens are generated using cryptographically secure random bytes:

```go
func generateSecureToken() (string, error) {
    bytes := make([]byte, 32)
    if _, err := rand.Read(bytes); err != nil {
        return "", err
    }
    
    token := "vst_" + base64.URLEncoding.EncodeToString(bytes)
    return token, nil
}
```

### Token Hashing

All tokens are stored as SHA256 hashes:

```go
func HashToken(token string) string {
    hash := sha256.Sum256([]byte(token))
    return base64.StdEncoding.EncodeToString(hash[:])
}
```

### IP Validation

The system supports IP whitelisting with proper IP address validation:

```go
func isValidIP(ip string) bool {
    matched, _ := regexp.MatchString(`^(\d{1,3}\.){3}\d{1,3}$`, ip)
    if !matched {
        return false
    }
    
    parts := strings.Split(ip, ".")
    for _, part := range parts {
        if len(part) > 3 || len(part) == 0 {
            return false
        }
        if part[0] == '0' && len(part) > 1 {
            return false
        }
    }
    
    return true
}
```

## Token Rotation

### Automatic Rotation

Tokens can be configured for automatic rotation:

```json
{
  "rotation_policy": {
    "auto_rotate": true,
    "rotation_interval_days": 90,
    "notify_before_days": 7
  }
}
```

### Manual Rotation

Tokens can be manually rotated via the API:

```http
POST /api/v1/service-tokens/{id}/rotate
Authorization: Bearer <jwt_token>
```

### Rotation Process

1. Generate new secure token
2. Update token hash in database
3. Reset usage count
4. Update rotation timestamp
5. Return new token to client

## Usage Tracking

### Usage Statistics

The system tracks comprehensive usage statistics:

- Total tokens created
- Active tokens
- Revoked tokens
- Expired tokens
- Recently used tokens (last 30 days)

### Usage Limits

Tokens can have configurable usage limits:

```json
{
  "max_uses": 1000
}
```

When the limit is reached, the token becomes invalid.

## Permissions

### Required Permissions

Service token operations require specific permissions:

- `create:service_token:own` - Create service tokens
- `read:service_token:own` - Read own service tokens
- `list:service_token:own` - List own service tokens
- `update:service_token:own` - Update own service tokens
- `rotate:service_token:own` - Rotate own service tokens
- `revoke:service_token:own` - Revoke own service tokens

### Admin Permissions

Administrators have additional permissions:

- `read:service_token:any` - Read any service tokens
- `list:service_token:any` - List any service tokens
- `update:service_token:any` - Update any service tokens
- `rotate:service_token:any` - Rotate any service tokens
- `revoke:service_token:any` - Revoke any service tokens

## Background Tasks

### Token Cleanup

The system includes background tasks for maintenance:

1. **Expired Token Cleanup**: Runs every hour to revoke expired tokens
2. **Rotation Check**: Runs every 6 hours to mark tokens needing rotation

### Scheduler

```go
type TokenRotationScheduler struct {
    repo   *repository.Queries
    logger *slog.Logger
}

func (trs *TokenRotationScheduler) StartScheduler(ctx context.Context) {
    go trs.runCleanupScheduler(ctx)
    go trs.runRotationScheduler(ctx)
}
```

## Best Practices

### Token Management

1. **Regular Rotation**: Rotate tokens every 90 days
2. **Scope Limitation**: Use minimal required scopes
3. **IP Restriction**: Whitelist specific IP addresses when possible
4. **Usage Monitoring**: Monitor token usage patterns
5. **Immediate Revocation**: Revoke compromised tokens immediately

### Security Considerations

1. **Secure Storage**: Never store tokens in plain text
2. **HTTPS Only**: Always use HTTPS for token transmission
3. **Logging**: Log all token usage for audit purposes
4. **Monitoring**: Monitor for unusual usage patterns
5. **Backup Tokens**: Maintain backup tokens for critical services

### Error Handling

The system provides detailed error messages:

- `"token has been revoked"` - Token was manually revoked
- `"token has expired"` - Token passed its expiration date
- `"token usage limit exceeded"` - Token reached its usage limit
- `"access denied from IP address"` - Client IP not in whitelist
- `"user agent not allowed"` - User agent doesn't match pattern

## Migration

### Running Migrations

To apply the enhanced service tokens schema:

```bash
goose up
```

### Backward Compatibility

The implementation maintains backward compatibility with existing tokens while adding new features.

## Monitoring and Alerting

### Key Metrics

Monitor these key metrics:

- Token creation rate
- Token revocation rate
- Failed authentication attempts
- Token usage patterns
- Rotation success rate

### Alerts

Set up alerts for:

- High rate of failed authentications
- Tokens approaching expiration
- Tokens needing rotation
- Unusual usage patterns

## Troubleshooting

### Common Issues

1. **Token Not Working**: Check if token is revoked, expired, or usage limit exceeded
2. **IP Access Denied**: Verify client IP is in the whitelist
3. **User Agent Rejected**: Check if user agent matches the pattern
4. **Permission Denied**: Ensure account has required permissions

### Debug Information

Enable debug logging to troubleshoot authentication issues:

```go
logger.SetLevel(slog.LevelDebug)
```

## Future Enhancements

### Planned Features

1. **Token Analytics**: Detailed usage analytics and reporting
2. **Webhook Notifications**: Notify when tokens need rotation
3. **Token Templates**: Predefined token configurations
4. **Bulk Operations**: Bulk token management operations
5. **Advanced Scoping**: More granular permission scopes

