# 6. Endpoint permission enforcement rollout

Date: 2026-07-12

## Status

partially implemented

## Context

A historical route-by-route audit found roughly 20 endpoints across `activity_handler.go`,
`streak_handler.go`, `institution_handler.go`, `leaderboard_handler.go`, and `auth_handler.go`'s
logout/revoke that require `IsAuthenticated` but no specific permission — meaning any logged-in
account, regardless of role, can call them today:

```
POST /auth/token/revoke                              GET /auth/{provider}/logout
GET  /devices/mine
GET  /leaderboard/global                              GET /leaderboard/global/{user}
POST /activity/add                                    GET  /activity/all
GET  /activity/active                                 GET  /activity/inactive
PATCH /activity/{id}                                  DELETE /activity/{id}
GET  /users/activity/completions/for-user/{id}
POST /users/activity/complete                         POST /streaks/milestone/create
GET  /streaks/milestone/active                        DELETE /streaks/milestone/{id}
GET  /institutions/find/{id}                          GET /institutions/search
GET  /institutions/for-account                        GET /institutions/accounts
GET  /institutions/accounts/fanout
```

(`POST /users/activity/complete` and `POST`/`DELETE /institutions/account` are excluded from this
list — they're fixed separately via ownership checks, see ADR 0007, since they don't need a
role/permission at all.)

The historical concern that ordinary accounts have no default role has since been resolved; see
`docs/RBAC.md`, "Default role at signup". That does not mean every previously open endpoint should
be gated with an administrative permission. User-facing reads and self-service actions need
appropriate user permissions or ownership checks, while shared reward-rule mutations need an
administrative capability.

## Decision

The reward-related part of the rollout is implemented:

- Activity catalog writes require `create:activity:any`, `update:activity:any`, or
  `delete:activity:any`.
- Milestone creation and deletion require `create:streak_milestone:any` or
  `delete:streak_milestone:any`.
- Trusted service awards require `award:activity:any` and a service token. This permission is
  granted explicitly to the integrating bot role; it is not added to the ordinary user role.
- Completion-history reads enforce ownership unless the caller has `read:activity:any`.
- User streak reads derive the account from the authenticated caller rather than accepting another
  account ID.

New reward permissions are seeded by migration and automatically assigned to `Administrator` by
the existing permission trigger. User-facing leaderboard and catalog reads remain available to
authenticated callers. The other routes in the historical audit need separate authorization
decisions.

## Consequences

**What becomes easier:**
- Reward-rule changes and service-side awards have explicit permission requirements.
- Integrating services can receive a narrow award permission without becoming administrators.
- The user role remains the baseline for ordinary user-facing activity and leaderboard behavior.

**What remains:**
- The other endpoints from the historical audit need individual authorization decisions. Distinguish
  user-facing reads, ownership checks, and administrative operations instead of applying one blanket
  permission.
