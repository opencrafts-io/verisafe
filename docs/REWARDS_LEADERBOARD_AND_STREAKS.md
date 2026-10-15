# Rewards, leaderboard, and streaks

This guide explains how Verisafe awards points, tracks streaks, and ranks accounts. It also gives
other services the supported flow for awarding a user after verifying an event they own.

## How points are awarded

An activity is a configured reward rule. It has a point value, a daily completion limit, an active
flag, and a flag controlling whether completions advance a streak. Activity points are currently
limited to 1–10 and daily completion limits must be positive.

When a completion is recorded, Verisafe writes an `activity_completions` row and a corresponding
`vibepoint_transactions` row in one database transaction. A database trigger updates the account's
cached `vibe_points` total from that transaction. The leaderboard reads this total. The transaction
ledger is the record of why points were awarded; services should never update `accounts.vibe_points`
directly.

The daily cap and streak state are serialized per account and activity. Multiple completions on the
same date can earn points when the activity allows them, but the streak advances at most once per
date. Date boundaries use the database session's `CURRENT_DATE`; deployments should keep the database
session timezone consistent. A user's displayed current streak becomes zero after that date passes
the last completion date. The longest streak and lifetime completion count remain available.

An active milestone awards its configured bonus when the streak reaches its exact `days_required`
value. A user earns each milestone at most once. Milestones are activity-specific in the award path;
create one for each activity that should have a milestone. Activity and milestone bonuses are each
limited to 1–10 points.

## Awarding from another service

Use `POST /rewards/activity-completions` after the calling service has verified the event that
qualifies for a reward. The endpoint accepts only a service token, and the bot account behind that
token must have the `award:activity:any` permission. Grant that permission through a dedicated least-
privilege role; the `Administrator` role receives it automatically, but should not be used for a
routine integration.

Create a bot account and service token using the existing [bot account guide](BOT_ACCOUNT_CREATION.md)
and [service token guide](SERVICE_TOKENS.md). Assign the reward permission to the bot account's role
using the RBAC role-permission APIs. Send the token in the `X-API-Key` header. Never put it in a
browser or mobile client.

An administrator first creates an activity definition with `POST /activity/add`; other services can
discover enabled activity IDs through `GET /activity/active`. Agree on which verified source event
maps to each activity and its configured daily limit. Grant the bot role only `award:activity:any`;
the award route does not require activity-management permissions.

```http
POST /rewards/activity-completions
X-API-Key: vst_<service-token>
Content-Type: application/json
```

```json
{
  "account_id": "0a9d2e52-ff8e-4a7f-87c7-0635f0a6c5c7",
  "activity_id": "6fcdb3c1-736f-4ac1-a54f-18079bf4786b",
  "idempotency_key": "course-completion:evt_01J9K1",
  "metadata": {
    "source": "learning-service",
    "event_id": "evt_01J9K1"
  }
}
```

Use a stable source event identifier for `idempotency_key` and reuse it for every retry of that
event. Keys are scoped to the account, limited to 200 characters, and cannot be reused for another
activity for that same account. A replay returns the original completion with
`already_processed: true`; it does not add points, advance a streak, or send another completion
notification. The key should identify one reward event, not a batch or a time window.

The response is `200 OK` for a new award and for a replay:

```json
{
  "completion_id": 1842,
  "points_earned": 5,
  "current_streak": 8,
  "milestone_achieved": false,
  "milestone_bonus": 0,
  "already_processed": false
}
```

The caller should treat the response as the authoritative award result. A missing or inactive
activity, an exhausted daily cap, an unknown account, or a key reused for another activity fails the
request. The endpoint does not verify the source event on the caller's behalf; the integrating
service must do that before making the request. The service bot account ID is recorded in
`vibepoint_transactions.awarded_by` for audit purposes.

Invalid payloads return `400`; a human token or a bot without the permission returns `403`. A failed
database award returns `500`, and the caller may safely retry with the same idempotency key.

For a user-initiated completion, use `POST /users/activity/complete` with the account ID matching
the authenticated subject. It accepts the same JSON metadata shape and an optional idempotency key.
This route is for self-reported actions. Use the service-token endpoint for actions verified by
another backend service.

## Activity and milestone administration

Reward definitions are shared rules and require administrative permissions:

| Operation | Endpoint | Required permission |
|---|---|---|
| Create activity | `POST /activity/add` | `create:activity:any` |
| Update activity | `PATCH /activity/{id}` | `update:activity:any` |
| Delete activity | `DELETE /activity/{id}` | `delete:activity:any` |
| Create milestone | `POST /streaks/milestone/create` | `create:streak_milestone:any` |
| Delete milestone | `DELETE /streaks/milestone/{id}` | `delete:streak_milestone:any` |

These permissions are seeded by migration and automatically granted to the `Administrator` role.
New ordinary roles do not receive them unless explicitly assigned. `PATCH /activity/{id}` supports
explicit `false` values for `is_active` and `streak_eligible`; omitted fields remain unchanged.
Optional point and daily-cap fields can be changed to positive valid values; zero is rejected.
Activity creation requires a name and a point value from 1–10. Omitting `max_daily_completions`
when creating an activity uses the default of one; omitted `streak_eligible` defaults to true.
Milestones require an activity, a title, a positive day threshold, and a bonus from 1–10.

## Streak API

`GET /users/streaks/me` returns the authenticated account's activity streaks. It includes the
current and longest streak, total completions, the last completion date, and the number of days to
the next unearned active milestone. No streak rows returns an empty JSON array.

## Leaderboard API

`GET /leaderboard/global` returns the existing paginated global ranking. Pages are ordered by points
descending, with account ID as a stable tie-breaker. Equal point totals share the same `vibe_rank`.
Only human accounts that are not pending deletion appear.

`GET /leaderboard/global/{user}/around?limit=20` returns up to `limit` accounts around the requested
user. The default is 20; values over 50 are capped at 50, and values below one are rejected. The
requested user is centered when the ranking window allows it, and the window shifts at the top or
bottom of the ranking to return as many rows as possible. A user outside the human leaderboard
receives `404`.

The following response is abbreviated; actual results include neighboring rows and the account
profile fields exposed by the existing leaderboard response.

```json
{
  "user_id": "0a9d2e52-ff8e-4a7f-87c7-0635f0a6c5c7",
  "user_position": 48,
  "total_users": 312,
  "results": [
    {
      "id": "0a9d2e52-ff8e-4a7f-87c7-0635f0a6c5c7",
      "email": "example@example.com",
      "name": "Example User",
      "username": "example",
      "vibe_points": 125,
      "avatar_url": null,
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-10-08T00:00:00Z",
      "vibe_rank": 48,
      "position": 48,
      "total_users": 312
    }
  ]
}
```

`position` is the unique 1-based display position used to construct the stable window. `vibe_rank`
uses standard competition ranking, so users tied on points share a rank. The target user's row is
included in `results` and its unique position is also returned as `user_position`.

## API reference

The generated [Swagger/OpenAPI document](swagger.yaml) includes request and response schemas for
these routes.
