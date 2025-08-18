# Bot Account Creation Guide

This guide explains how to create bot accounts in Verisafe with enhanced service tokens that follow industry-standard security practices.

## Overview

Bot accounts are special accounts designed for automated services, microservices, and API integrations. They can only be created by users with appropriate permissions and come with enhanced service tokens that provide secure API access.

## Prerequisites

Before creating a bot account, ensure you have:

1. **Valid Authentication**: A valid JWT token for a user account
2. **Required Permissions**: The `create:account:any` permission
3. **API Access**: Access to the Verisafe API endpoints

## Authentication

All bot account creation requests require authentication. Include your JWT token in the Authorization header:

```bash
Authorization: Bearer <your_jwt_token>
```

## Creating a Bot Account

### Endpoint

```
POST /accounts/bot/create
```

### Request Format

The request body should be a JSON object with two main sections:

```json
{
  "account": {
    "email": "string (required)",
    "name": "string (required)",
    "avatar_url": "string (optional)"
  },
  "service_token": {
    "name": "string (required)",
    "description": "string (optional)",
    "expires_in_days": "integer (optional, 1-3650)",
    "scopes": ["array of strings (optional)"],
    "max_uses": "integer (optional, minimum 1)",
    "rotation_policy": {
      "auto_rotate": "boolean",
      "rotation_interval_days": "integer (1-365)",
      "notify_before_days": "integer (1-30)"
    },
    "ip_whitelist": ["array of IP addresses (optional)"],
    "user_agent_pattern": "regex pattern (optional)",
    "metadata": {
      "key": "value (optional)"
    }
  }
}
```

### Field Descriptions

#### Account Fields

- **email** (required): A valid email address for the bot account
- **name** (required): A descriptive name for the bot account (1-100 characters)
- **avatar_url** (optional): URL to an avatar image for the bot account

#### Service Token Fields

- **name** (required): A descriptive name for the service token (1-100 characters)
- **description** (optional): Detailed description of the token's purpose
- **expires_in_days** (optional): Number of days until the token expires (1-3650, default: 365)
- **scopes** (optional): Array of permission scopes the token can access
- **max_uses** (optional): Maximum number of times the token can be used
- **rotation_policy** (optional): Configuration for automatic token rotation
  - **auto_rotate**: Whether to automatically rotate the token
  - **rotation_interval_days**: Days between rotations (1-365)
  - **notify_before_days**: Days before rotation to send notification (1-30)
- **ip_whitelist** (optional): Array of allowed IP addresses
- **user_agent_pattern** (optional): Regex pattern to validate user agent strings
- **metadata** (optional): Additional key-value pairs for custom data

## Examples

### Basic Bot Account Creation

```bash
curl -X POST http://localhost:8080/accounts/bot/create \
  -H "Authorization: Bearer <your_jwt_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "account": {
      "email": "mybot@company.com",
      "name": "My Production Bot"
    },
    "service_token": {
      "name": "Production API Key",
      "description": "API key for production environment"
    }
  }'
```

### Advanced Bot Account with Full Configuration

```bash
curl -X POST http://localhost:8080/accounts/bot/create \
  -H "Authorization: Bearer <your_jwt_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "account": {
      "email": "production-bot@company.com",
      "name": "Production Data Processor",
      "avatar_url": "https://example.com/bot-avatar.png"
    },
    "service_token": {
      "name": "Production Data API Key",
      "description": "API key for production data processing services",
      "expires_in_days": 365,
      "scopes": ["read:data", "write:data", "process:reports"],
      "max_uses": 10000,
      "rotation_policy": {
        "auto_rotate": true,
        "rotation_interval_days": 90,
        "notify_before_days": 7
      },
      "ip_whitelist": ["192.168.1.100", "10.0.0.50"],
      "user_agent_pattern": "DataProcessor/.*",
      "metadata": {
        "environment": "production",
        "team": "data-science",
        "service": "data-processor",
        "version": "1.0.0"
      }
    }
  }'
```

### Development Bot Account

```bash
curl -X POST http://localhost:8080/accounts/bot/create \
  -H "Authorization: Bearer <your_jwt_token>" \
  -H "Content-Type: application/json" \
  -d '{
    "account": {
      "email": "dev-bot@company.com",
      "name": "Development Test Bot"
    },
    "service_token": {
      "name": "Development API Key",
      "description": "API key for development and testing",
      "expires_in_days": 30,
      "scopes": ["read:data"],
      "max_uses": 1000,
      "metadata": {
        "environment": "development",
        "team": "engineering"
      }
    }
  }'
```

## Response Format

### Success Response (201 Created)

```json
{
  "account": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "mybot@company.com",
    "name": "My Production Bot",
    "type": "bot",
    "created_at": "2025-01-01T00:00:00Z"
  },
  "service_token": {
    "id": "550e8400-e29b-41d4-a716-446655440001",
    "name": "Production API Key",
    "description": "API key for production environment",
    "token": "vst_abc123def456ghi789jkl012mno345pqr678stu901vwx234yz",
    "expires_at": "2026-01-01T00:00:00Z",
    "scopes": ["read:data", "write:data"],
    "max_uses": 10000,
    "created_at": "2025-01-01T00:00:00Z",
    "metadata": {
      "environment": "production",
      "team": "backend"
    }
  }
}
```

### Error Responses

#### 400 Bad Request
```json
{
  "error": "Email and name are required"
}
```

#### 401 Unauthorized
```json
{
  "error": "Missing Authorization or X-API-Key header"
}
```

#### 403 Forbidden
```json
{
  "error": "You do not have the necessary permissions to perform this action"
}
```

#### 500 Internal Server Error
```json
{
  "error": "We couldn't create this account at the moment please try again later"
}
```

## Using the Service Token

Once you have created a bot account and received the service token, you can use it to authenticate API requests:

```bash
curl -X GET http://localhost:8080/api/v1/some-endpoint \
  -H "X-API-Key: vst_abc123def456ghi789jkl012mno345pqr678stu901vwx234yz"
```

## Security Best Practices

### 1. Token Storage
- **Never store tokens in plain text**
- Use secure secret management systems
- Rotate tokens regularly
- Monitor token usage

### 2. IP Whitelisting
- Restrict tokens to specific IP addresses when possible
- Use CIDR notation for IP ranges
- Regularly review and update whitelists

### 3. Scope Limitation
- Grant only the minimum required permissions
- Use specific scopes instead of broad permissions
- Regularly audit token permissions

### 4. Usage Monitoring
- Monitor token usage patterns
- Set up alerts for unusual activity
- Track failed authentication attempts

### 5. Token Rotation
- Enable automatic token rotation
- Set appropriate rotation intervals
- Plan for manual rotation when needed

## Token Management

After creating a bot account, you can manage the service tokens using the service token management endpoints:

- **List tokens**: `GET /api/v1/service-tokens`
- **Get token details**: `GET /api/v1/service-tokens/{id}`
- **Update token**: `PUT /api/v1/service-tokens/{id}`
- **Rotate token**: `POST /api/v1/service-tokens/{id}/rotate`
- **Revoke token**: `DELETE /api/v1/service-tokens/{id}`

## Troubleshooting

### Common Issues

1. **Permission Denied**
   - Ensure your account has the `create:account:any` permission
   - Check that your JWT token is valid and not expired

2. **Invalid Request Body**
   - Verify all required fields are present
   - Check field validation rules (length limits, format requirements)
   - Ensure JSON is properly formatted

3. **Email Already Exists**
   - Use a unique email address for each bot account
   - Check existing accounts before creating new ones

4. **Token Generation Failed**
   - This is usually a server-side issue
   - Contact system administrators if the problem persists

### Validation Rules

- **Email**: Must be a valid email format
- **Name**: 1-100 characters, required
- **Token Name**: 1-100 characters, required
- **Expires In Days**: 1-3650 days
- **Max Uses**: Minimum 1
- **Rotation Interval**: 1-365 days
- **Notify Before Days**: 1-30 days

## API Reference

For complete API documentation, see the [Service Tokens Implementation Guide](SERVICE_TOKENS.md).

## Support

If you encounter issues creating bot accounts:

1. Check the error messages for specific guidance
2. Verify your permissions and authentication
3. Review the request format and validation rules
4. Contact your system administrator for assistance

## Examples in Different Languages

### Python
```python
import requests
import json

def create_bot_account(jwt_token, account_data):
    url = "http://localhost:8080/accounts/bot/create"
    headers = {
        "Authorization": f"Bearer {jwt_token}",
        "Content-Type": "application/json"
    }
    
    response = requests.post(url, headers=headers, json=account_data)
    return response.json()

# Usage
account_data = {
    "account": {
        "email": "mybot@company.com",
        "name": "My Production Bot"
    },
    "service_token": {
        "name": "Production API Key",
        "description": "API key for production environment"
    }
}

result = create_bot_account("your_jwt_token", account_data)
print(f"Bot account created: {result['account']['id']}")
print(f"Service token: {result['service_token']['token']}")
```

### JavaScript/Node.js
```javascript
async function createBotAccount(jwtToken, accountData) {
    const response = await fetch('http://localhost:8080/accounts/bot/create', {
        method: 'POST',
        headers: {
            'Authorization': `Bearer ${jwtToken}`,
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(accountData)
    });
    
    return await response.json();
}

// Usage
const accountData = {
    account: {
        email: 'mybot@company.com',
        name: 'My Production Bot'
    },
    service_token: {
        name: 'Production API Key',
        description: 'API key for production environment'
    }
};

createBotAccount('your_jwt_token', accountData)
    .then(result => {
        console.log(`Bot account created: ${result.account.id}`);
        console.log(`Service token: ${result.service_token.token}`);
    })
    .catch(error => console.error('Error:', error));
```

### Go
```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

type BotAccountRequest struct {
    Account struct {
        Email     string  `json:"email"`
        Name      string  `json:"name"`
        AvatarUrl *string `json:"avatar_url,omitempty"`
    } `json:"account"`
    ServiceToken struct {
        Name     string   `json:"name"`
        Scopes   []string `json:"scopes,omitempty"`
        MaxUses  *int     `json:"max_uses,omitempty"`
    } `json:"service_token"`
}

func createBotAccount(jwtToken string, accountData BotAccountRequest) error {
    jsonData, err := json.Marshal(accountData)
    if err != nil {
        return err
    }
    
    req, err := http.NewRequest("POST", "http://localhost:8080/accounts/bot/create", bytes.NewBuffer(jsonData))
    if err != nil {
        return err
    }
    
    req.Header.Set("Authorization", "Bearer "+jwtToken)
    req.Header.Set("Content-Type", "application/json")
    
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusCreated {
        return fmt.Errorf("failed to create bot account: %d", resp.StatusCode)
    }
    
    return nil
}

func main() {
    accountData := BotAccountRequest{}
    accountData.Account.Email = "mybot@company.com"
    accountData.Account.Name = "My Production Bot"
    accountData.ServiceToken.Name = "Production API Key"
    
    err := createBotAccount("your_jwt_token", accountData)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
    } else {
        fmt.Println("Bot account created successfully")
    }
}
```
