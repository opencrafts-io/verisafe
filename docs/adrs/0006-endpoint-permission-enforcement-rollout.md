# 6. Endpoint permission enforcement rollout

Date: 2026-07-12

## Status

proposed

## Context

A full route-by-route audit found roughly 20 endpoints across `activity_handler.go`,
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

Naively adding `HasPermission` checks to the routes above would lock out every non-admin user
immediately, because **no human account is ever granted a role at signup** (see `docs/RBAC.md`,
"Known gap: no default role at signup"). A freshly created account has zero rows in `user_roles` and
therefore zero permissions until an admin manually grants a role — which today doesn't matter, because
none of these routes check for one. Flipping on enforcement without first giving every account
something to hold would be a self-inflicted outage, not a security improvement.

## Decision

This ADR records the plan without implementing it. When this work is picked up, do it in this order,
each step verified before the next:

1. **Introduce a default role** (e.g. `member`) seeded via migration, with a permission set covering
   today's any-authenticated-user behavior for the routes listed above (e.g. `read:leaderboard:any`,
   `complete:activity:own`, `read:activity:any`, etc. — exact set to be defined when this is
   implemented, informed by which of the routes above are genuinely meant to be open to any user vs.
   were simply never gated).
2. **Backfill migration**: assign the default role to every existing account that currently holds zero
   roles. Verify count of affected accounts before and after; this step must be idempotent and safe to
   re-run.
3. **Signup-time assignment**: `upsertAccount` (`internal/auth/auth_handler.go`) calls `AssignRole` for
   the default role on every new account, so step 2 never needs to run again for new signups.
4. **Only after 1–3 are deployed and verified**, add `HasPermission` checks to the routes above,
   endpoint-by-endpoint, one small PR per handler file — not all at once. Each PR should be verified
   against a production (or production-like) snapshot to confirm the affected accounts already hold
   the default role's permission before the check goes live.

`PATCH`/`DELETE /activity/{id}` need particular attention in step 1's design: `Activity` has no owner
field (it's a shared catalog resource, e.g. "Read a book"), so these two need an admin-style
permission (e.g. `update:activity:any`/`delete:activity:any`), not something the default role would
hold — meaning regular users would lose access to routes they technically could call today, if those
calls were ever exercised by non-admin clients in practice. Confirm actual usage before gating these
two specifically.

## Consequences

**What becomes easier, once implemented:**
- Every endpoint's authorization requirement is explicit and enforced, not implicit in "did anyone
  remember to add HasPermission."
- New accounts have a well-defined baseline of what they can do from the moment they sign up, instead
  of relying on every endpoint independently deciding to only check `IsAuthenticated`.

**What becomes harder or requires attention:**
- This is a multi-step, multi-deploy rollout, not a single PR — skipping steps 1–3 or reordering them
  (enforcing before backfilling) will lock out real users.
- Deciding the exact default permission set requires auditing actual client behavior against each of
  the ~20 routes above, not just guessing from the route name — some may turn out to be
  admin/internal-only in practice despite currently having no permission check.
- `PATCH`/`DELETE /activity/{id}` specifically may represent an existing capability being removed from
  whichever callers use them today; this needs product/client confirmation before gating, not just an
  engineering decision.
