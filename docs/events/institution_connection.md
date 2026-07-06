# Institution Connection Event Schema - Consumer Guide

## Overview

This queue delivers events when users connect or disconnect their accounts from institutions in the Verisafe service. As a consumer service, you will receive notifications whenever a user establishes or terminates a connection between their account and an institution.

Only **Verisafe** publishes to this queue. All other services are consumers.

## Supported Events

This queue delivers two event types:
- `user.institution.connected` - User connected their account to an institution
- `user.institution.disconnected` - User disconnected their account from an institution

## Queue Configuration

To consume messages from this queue, use the following configuration:

| Property | Value |
|----------|-------|
| Exchange Name | `verisafe.events.topic` |
| Exchange Type | `topic` |
| Routing Key | `user.institution.*` |


### Verisafe publishes messages with the following routing keys:

- user.institution.connected (for user.institution.connected events)
- user.institution.disconnected (for user.institution.disconnected events)

Both match the consumer pattern: verisafe.institution.connection.*

## Message Structure

You will receive messages as JSON strings with the following structure:

```json
{
  "meta": {
    "event_type": "user.institution.connected",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  },
  "institution_connection": {
    "account_id": "550e8400-e29b-41d4-a716-446655440001",
    "institution_id": 12345
  }
}
```

The `event_type` can be either:
- `"user.institution.connected"` - When a user connects to an institution
- `"user.institution.disconnected"` - When a user disconnects from an institution

## Metadata Schema

```go
type InstitutionEventMetaData struct {
	EventType       string    `json:"event_type"`
	Timestamp       time.Time `json:"timestamp"`
	SourceServiceID string    `json:"source_service_id"`
	RequestID       string    `json:"request_id"`
}
```

### Meta Fields

| Field | Type | Required | Constraint | Description |
|-------|------|----------|-----------|-------------|
| `event_type` | string | YES | Must be `"user.institution.connected"` or `"user.institution.disconnected"` | Identifies the event type. Only these two values are accepted. |
| `timestamp` | string (ISO 8601) | YES | Valid RFC3339/ISO 8601 datetime | The UTC timestamp when the event was generated. Example: `"2024-04-03T14:30:00Z"` |
| `source_service_id` | string | YES | Must be exactly `"io.opencrafts.verisafe"` | Identifies the originating service. Events from other sources are rejected. |
| `request_id` | string (UUID) | YES | Valid UUID v4 | Unique identifier for tracing and correlation across services. Used for distributed request tracking. Example: `"550e8400-e29b-41d4-a716-446655440000"` |

## Institution Connection Payload Schema

| Field | Type | Required | Constraint | Description |
|-------|------|----------|-----------|-------------|
| `account_id` | string (UUID) | YES | Valid UUID v4, non-nullable | The unique identifier of the user account. Must exist in the receiving system's User table. |
| `institution_id` | integer | YES | Valid integer, must exist in Institution data | The unique identifier of the institution. Must exist in the receiving system's Institution table. |

## Validation Rules

When you consume a message, you must validate it before processing:

1. **JSON Parsing**: Parse the message body as JSON. If parsing fails, discard the message and log the error.

2. **Required Fields**: Verify that all of the following fields are present and non-empty:
   - `meta.event_type`
   - `meta.timestamp`
   - `meta.source_service_id`
   - `meta.request_id`
   - `institution_connection.account_id`
   - `institution_connection.institution_id`

3. **Event Type**: Verify that `meta.event_type` is either `"user.institution.connected"` or `"user.institution.disconnected"`. Any other value should be discarded.

4. **Source Service**: Verify that `meta.source_service_id` is exactly `"io.opencrafts.verisafe"`. If it is from a different source, discard the message.

5. **Account ID Format**: Verify that `account_id` is a valid UUID. If the format is invalid, discard the message.

6. **Request ID Format**: Verify that `request_id` is a valid UUID v4.

7. **Timestamp Format**: Verify that `meta.timestamp` is a valid ISO 8601 datetime string.

8. **Data Existence**: After passing the above checks, verify that the referenced entities exist in your system:
   - The User identified by `account_id` must exist
   - The Institution identified by `institution_id` must exist
   - If either entity does not exist, handle the error (see Error Handling section)

## Processing Behavior

As a consumer, when you receive a valid message, you learn that a user-institution relationship has changed:

**For `user.institution.connected` events:**
1. A user (identified by `account_id`) has connected their account to an institution (identified by `institution_id`)
2. You can use this information to:
   - Create or update user records related to the institution
   - Trigger institution-specific onboarding or setup flows
   - Enable institution-specific features for the user
   - Log the connection event in your audit trail
   - Synchronize state between services

**For `user.institution.disconnected` events:**
1. A user (identified by `account_id`) has disconnected their account from an institution (identified by `institution_id`)
2. You can use this information to:
   - Revoke institution-specific access or permissions
   - Disable institution-specific features for the user
   - Clean up institution-specific user data if applicable
   - Log the disconnection event in your audit trail
   - Synchronize state between services

In both cases:
- The timestamp (`meta.timestamp`) indicates when the event occurred
- The request_id (`meta.request_id`) can be used for correlation in your logs and with Verisafe support

## Idempotency

Messages from Verisafe are idempotent. If you receive the same message multiple times (due to retries or network issues), processing it again will yield the same result:

- The same user-institution connection will not create duplicate records in your system
- Subsequent processing of the same event will update the same records without creating inconsistencies
- Use the `request_id` and `account_id` combination as a unique key for deduplication if needed

This means your consumer should safely handle receiving the same message twice within a short time window without causing data corruption or duplicate entries.

## Example: Valid Message - User Connected

```json
{
  "meta": {
    "event_type": "user.institution.connected",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  },
  "institution_connection": {
    "account_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
    "institution_id": 42
  }
}
```

## Example: Valid Message - User Disconnected

```json
{
  "meta": {
    "event_type": "user.institution.disconnected",
    "timestamp": "2024-04-03T14:35:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "660e8400-e29b-41d4-a716-446655440001"
  },
  "institution_connection": {
    "account_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
    "institution_id": 42
  }
}
```

## Example: Invalid Message - Wrong Event Type

```json
{
  "meta": {
    "event_type": "user.institution.updated",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  },
  "institution_connection": {
    "account_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
    "institution_id": 42
  }
}
```

**Result**: Message rejected. `event_type` must be either `"user.institution.connected"` or `"user.institution.disconnected"`.

## Example: Invalid Message - Wrong Source Service

```json
{
  "meta": {
    "event_type": "user.institution.connected",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.different.service",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  },
  "institution_connection": {
    "account_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
    "institution_id": 42
  }
}
```

**Result**: Message rejected. `source_service_id` must be `"io.opencrafts.verisafe"`.

## Example: Invalid Message - Missing Required Field

```json
{
  "meta": {
    "event_type": "user.institution.connected",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  },
  "institution_connection": {
    "account_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479"
  }
}
```

**Result**: Message rejected. `institution_id` is required and missing.

## Example: Invalid Message - Malformed Account ID

```json
{
  "meta": {
    "event_type": "user.institution.connected",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  },
  "institution_connection": {
    "account_id": "not-a-uuid",
    "institution_id": 42
  }
}
```

**Result**: Message rejected. `account_id` must be a valid UUID.

## Example: Invalid Message - Malformed Request ID

```json
{
  "meta": {
    "event_type": "user.institution.connected",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "not-a-uuid"
  },
  "institution_connection": {
    "account_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
    "institution_id": 42
  }
}
```

**Result**: Message rejected. `request_id` must be a valid UUID.

## Example: Validation Passes - User Not Found

```json
{
  "meta": {
    "event_type": "user.institution.connected",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "550e8400-e29b-41d4-a716-446655440000"
  },
  "institution_connection": {
    "account_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
    "institution_id": 42
  }
}
```

**Result**: Passes validation. During processing, you attempt to look up the User but it does not exist in your system (with `account_id = f47ac10b-58cc-4372-a567-0e02b2c3d479`). You should log the error with the `request_id` for correlation and decide whether to retry or discard the message. See Error Handling section.

## Example: Validation Passes - Institution Not Found

```json
{
  "meta": {
    "event_type": "user.institution.disconnected",
    "timestamp": "2024-04-03T14:30:00Z",
    "source_service_id": "io.opencrafts.verisafe",
    "request_id": "550e8400-e29b-41d4-a716-446655440001"
  },
  "institution_connection": {
    "account_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
    "institution_id": 99999
  }
}
```

**Result**: Passes validation. During processing, you attempt to look up the Institution but it does not exist in your system (with `institution_id = 99999`). You should log the error with the `request_id` for correlation and decide whether to retry or discard the message. See Error Handling section.

## Error Handling

### Validation Failures

If a message fails validation, you must handle the error appropriately:

- Log the error with the full message body for debugging
- Do not process the message further
- Acknowledge the message to RabbitMQ to prevent redelivery (unless your consumer is configured otherwise)
- Consider alerting operations if malformed messages are arriving regularly

### Data Not Found

If a message passes validation but the referenced User or Institution does not exist in your system:

- Log the error with the `account_id`, `institution_id`, and `request_id` for correlation
- Decide whether to:
  - Wait and retry (the entities may be created shortly)
  - Discard the message
  - Store the event for later processing when the entities exist
- This typically indicates that Verisafe published the event before your system received the user/institution creation events

### Processing Failures

If an unexpected error occurs while processing the event (e.g., database errors):

- Log the full exception with the `request_id` for correlation
- Decide your retry strategy based on the type of failure
- Consider using negative acknowledgment to RabbitMQ for automatic redelivery (up to a maximum retry limit)
- After max retries, send the message to a dead-letter queue for manual inspection

## Consumer Implementation Guidelines

1. **Parse and Validate**: Implement strict validation as described in the Validation Rules section
2. **Idempotency**: Handle the possibility of receiving the same message multiple times. Design your consumer to be idempotent.
3. **Correlation**: Use the `request_id` field to correlate events across your service and with Verisafe for troubleshooting
4. **Async Processing**: Treat event processing as asynchronous. Do not assume immediate consistency with Verisafe
5. **Timing**: The user-institution connection happens in Verisafe before the event is published. Your system will receive the event after the connection is already established
6. **User/Institution Sync**: Ensure your service has received the user and institution data from Verisafe (or another source) before consuming these events
7. **Acknowledgment**: Only acknowledge the message after you have successfully processed it or decided to discard it
8. **Monitoring**: Monitor the consumer lag and error rates. Alert if messages are not being consumed in a timely manner
