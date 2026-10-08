# Android rewards and leaderboard API handoff

This handoff covers the Verisafe endpoints an Android client can use to show a user's points,
available activities, activity history, streaks, milestones, and nearby leaderboard. All paths below
are relative to the environment's Verisafe API base URL. Do not hardcode a staging or production host
in the app.

## Authentication

Use the user's Verisafe access token on authenticated requests:

```http
Authorization: Bearer <access_token>
Accept: application/json
Content-Type: application/json
```

Use the existing mobile sign-in and refresh-token flow in [AUTHENTICATION.md](AUTHENTICATION.md).
Store refresh tokens using Android secure storage and refresh access tokens through the auth API.
Do not ship a service token or `X-API-Key` in the Android app. That credential is for trusted
backend-to-backend integrations only.

## Endpoints for Android screens

| Client task | Method and path | Notes |
|---|---|---|
| Load the signed-in user's account and point total | `GET /accounts/me` | Use `id` as the account ID for self-scoped calls; `vibe_points` is the current total. |
| Load available activities | `GET /activity/active?page=1&page_size=100` | Paginated. Use each activity's `id` when submitting a completion. |
| Submit a user-reported activity | `POST /users/activity/complete` | `account_id` must match the signed-in user. The server applies the activity's point value and daily cap. |
| Load the signed-in user's streaks | `GET /users/streaks/me` | Returns an array; an account with no streak rows gets `[]`. |
| Load active streak milestones | `GET /streaks/milestone/active?page=1&page_size=100` | Paginated. Match milestone `activity_id` with an activity ID. |
| Load the signed-in user's completion history | `GET /users/activity/completions/for-user/{account_id}?page=1&page_size=20` | The caller can read their own history. |
| Load leaderboard entries around the signed-in user | `GET /leaderboard/global/{account_id}/around?limit=20` | Includes the signed-in user's row; maximum window size is 50. |
| Load a page of the global leaderboard | `GET /leaderboard/global?page=1&page_size=20` | Optional if the UI also has a top/global leaderboard view. |

The `page` and `page_size` endpoints return this envelope:

```json
{
  "count": 120,
  "next": "https://api.example.com/activity/active?page=2&page_size=100",
  "previous": null,
  "results": []
}
```

`page_size` defaults to 10 and is capped at 100. Follow `next` when the UI needs more results. For
the activity and milestone pickers, a page size of 100 is enough for the current API limit, but still
check `next` if the response provides one.

## Suggested screen loading flow

After sign-in, call `GET /accounts/me` and retain the returned `id` for self-scoped requests. To
render the rewards area, fetch active activities, streaks, active milestones, and recent completion
history. These requests can run in parallel once the account ID is known. Load the nearby leaderboard
using that same ID. Treat the account profile's `vibe_points` as the current total and refresh it
after an activity completion or when returning to the rewards screen.

## Submit an activity completion

The app should submit only actions it can report as user-initiated, such as launching the app. The
completion endpoint is self-reported; it does not prove that a client-side event occurred. Verisafe
still checks that the activity exists and is active, applies the configured point value and daily
completion limit, advances eligible streaks, and records any milestone bonus.

First resolve the activity ID from `GET /activity/active` (or from environment-specific app
configuration). Activity IDs can differ between environments. Avoid hardcoding a development ID in
the Android build.

```http
POST /users/activity/complete
Authorization: Bearer <access_token>
Content-Type: application/json
```

```json
{
  "account_id": "0a9d2e52-ff8e-4a7f-87c7-0635f0a6c5c7",
  "activity_id": "6fcdb3c1-736f-4ac1-a54f-18079bf4786b",
  "idempotency_key": "app-launch:0a9d2e52:2026-10-08",
  "metadata": {
    "source": "android",
    "event": "app_launch"
  }
}
```

`idempotency_key` is optional for this endpoint, but recommended. Reuse the same key when retrying
the same event after a timeout so the client does not show the reward twice. Keep it at or below 200
characters. For a once-per-day app launch, use one stable key per account and reward date, aligned
with the reward date used by the backend. The database's `CURRENT_DATE` determines the completion
date, so an offline event submitted later is credited on the server receipt date. The server daily
cap remains authoritative; local state should only avoid unnecessary duplicate requests.

Example success response:

```json
{
  "message": "Activity recorded successfully!",
  "completion_id": 1842,
  "points_earned": 5,
  "current_streak": 8,
  "milestone_achieved": false,
  "milestone_bonus": 0,
  "already_processed": false
}
```

Use `points_earned` and `milestone_bonus` from the response for the reward confirmation UI. If
`already_processed` is true, treat the request as a retry result: refresh state if needed, but do not
play the reward animation or increment a local total a second time. Refresh the account total and
streak list from their GET endpoints rather than maintaining those values as the source of truth.

Do not call `POST /rewards/activity-completions` from Android. That endpoint is for trusted services
that have verified an event on their own backend and requires a service token with
`award:activity:any`.

## Streak and milestone fields

`GET /users/streaks/me` returns one row per streak-eligible activity:

```json
[
  {
    "activity_name": "App launch",
    "current_streak": 8,
    "longest_streak": 12,
    "total_completions": 38,
    "last_completion_date": "2026-10-08",
    "days_until_next_milestone": 2
  }
]
```

Use `current_streak` and `days_until_next_milestone` for display. A missed backend reward date makes
the current streak zero. Do not calculate streaks from local launch history. Milestone definitions
are returned through the paginated active milestone endpoint; bonus points are granted by the
server when the milestone is reached.

## Nearby leaderboard

```http
GET /leaderboard/global/0a9d2e52-ff8e-4a7f-87c7-0635f0a6c5c7/around?limit=20
Authorization: Bearer <access_token>
```

`limit` defaults to 20, accepts values from 1 to 50, and larger values are capped at 50. Invalid or
less-than-one values return `400`. A user not on the leaderboard returns `404`.

```json
{
  "user_id": "0a9d2e52-ff8e-4a7f-87c7-0635f0a6c5c7",
  "user_position": 48,
  "total_users": 312,
  "results": [
    {
      "id": "0a9d2e52-ff8e-4a7f-87c7-0635f0a6c5c7",
      "name": "Example User",
      "username": "example",
      "vibe_points": 125,
      "avatar_url": null,
      "vibe_rank": 48,
      "position": 48,
      "total_users": 312
    }
  ]
}
```

`position` is the unique display position; `vibe_rank` is the competition rank, so users with equal
point totals share a rank. The target account is included in `results`. The current response also
contains an `email` field on each leaderboard row; the Android UI should render only approved public
profile fields such as name, username, and avatar.

## Errors and client behavior

| Status | Client handling |
|---|---|
| `400` | Show validation feedback or ignore malformed local state; do not retry unchanged input. |
| `401` | Refresh the access token once; if refresh fails, return the user to sign-in. |
| `403` | The account does not match the authenticated user or the operation requires a permission the app should not have. |
| `404` | The requested account has no leaderboard entry, or the resource is unavailable. |
| `5xx` / network failure | Show retry affordance. For completion submissions, retry with the same idempotency key. |

Error responses are JSON. Avoid logging access tokens, refresh tokens, or full profile payloads. For
the full reward rules and service-to-service award flow, see
[REWARDS_LEADERBOARD_AND_STREAKS.md](REWARDS_LEADERBOARD_AND_STREAKS.md). The generated
[Swagger/OpenAPI document](swagger.yaml) is the API schema reference.
