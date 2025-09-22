# RabbitMQ Integration

Verisafe now includes RabbitMQ event publishing capabilities to notify other services about user events. This integration follows the same pattern as GossipMonger for consistency.

## Overview

Verisafe publishes user events to RabbitMQ when:
- A new user account is created during OAuth authentication
- An existing user's social account is updated during OAuth authentication

## Configuration

Add the following environment variables to your `.env` file:

```env
# RabbitMQ Configuration
RABBITMQ_USER=guest
RABBITMQ_PASSWORD=guest
RABBITMQ_ADDRESS=localhost
RABBITMQ_PORT=5672
RABBITMQ_EXCHANGE=verisafe.exchange
```

## Event Types

### User Created Event
- **Routing Key**: `verisafe.user.created`
- **Event Type**: `user.created`
- **Published When**: A new user account is created during OAuth authentication

### User Updated Event
- **Routing Key**: `verisafe.user.updated`
- **Event Type**: `user.updated`
- **Published When**: An existing user's social account is updated during OAuth authentication

## Event Structure

All events follow this structure:

```json
{
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "name": "User Name",
    "type": "human",
    "avatar_url": "https://example.com/avatar.jpg",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  },
  "meta": {
    "event_type": "user.created",
    "timestamp": "2024-01-01T00:00:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "uuid"
  }
}
```

## Integration with GossipMonger

GossipMonger subscribes to these events using the same routing keys:
- `verisafe.user.created`
- `verisafe.user.updated`
- `verisafe.user.deleted` (for future use)

## Error Handling

- If RabbitMQ is not configured, event publishing is disabled and the application continues to function normally
- If event publishing fails, the error is logged but does not prevent the authentication flow from completing
- Each event includes a unique `request_id` for tracking and debugging

## Development

To test the integration locally:

1. Start RabbitMQ:
   ```bash
   docker run -d --name rabbitmq -p 5672:5672 -p 15672:15672 rabbitmq:3-management
   ```

2. Configure your `.env` file with RabbitMQ settings

3. Start Verisafe and perform OAuth authentication

4. Check RabbitMQ management interface at `http://localhost:15672` to see published events

## Architecture

The event bus implementation follows the same pattern as GossipMonger:

- `internal/eventbus/client.go` - RabbitMQ connection and basic event bus interface
- `internal/eventbus/user_event.go` - Event structure definitions
- `internal/eventbus/user_event_bus.go` - Type-safe event publishing methods

This ensures consistency across the OpenCrafts microservices architecture.
