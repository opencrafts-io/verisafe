# 5. RBAC handler bug fixes

Date: 2026-07-12

## Status

accepted

## Context

Three independent bugs were found in `internal/handlers/role_handler.go` and
`internal/handlers/permission_handler.go` during a review of the RBAC subsystem:

1. `DELETE /roles/revoke/{user_id}/{role_id}` (`RevokeUserRole`) is gated by
   `HasPermission(["assign:role:any"])` instead of the seeded `revoke:role:any` permission.
   `revoke:role:any` is created by the same migration that seeds `assign:role:any`
   (`database/migrations/20250712203316_create_default_permissions_and_roles.sql`) but was never
   referenced anywhere in the codebase — confirmed dead. The equivalent pair in
   `permission_handler.go` (`assign:permission:role` / `revoke:permission:role`) already uses two
   distinct permissions correctly, and the code itself carried a self-flagged TODO acknowledging the
   gap ("Some work might be needed to check for both the assign and revoke roles").
2. `PATCH /roles/{id}` (`UpdateRole`) parses `{id}` from the path via the router, but the handler
   never reads it — the row actually updated is whichever `id` field is present in the JSON body. The
   path segment is decorative. Every sibling `{id}`-based route (`GetRoleByID`, etc.) treats the path
   as the resource identifier; `UpdateRole` was the one outlier.
3. `GET /permissions/{id}` (`GetPermissionByID`) is declared `:many` in
   `database/queries/permissions.sql`, so the generated `internal/repository/permissions.sql.go`
   returns `([]Permission, error)`. Verified directly against a live database
   (`internal/repository`, temporary smoke test): a found permission returns a **1-element JSON
   array** instead of an object, and a not-found permission returns **`200 OK` with body `[]`**,
   because a `:many` query never produces `sql.ErrNoRows` — it produces an empty slice with a nil
   error. The handler's `errors.Is(err, sql.ErrNoRows)` 404 branch has therefore never been
   reachable.

## Decision

1. `RevokeUserRole`'s route registration now checks `revoke:role:any` instead of `assign:role:any`.
   The resolved TODO comment is removed.
2. `UpdateRole` now parses `{id}` from the path (same `uuid.Parse` + 400-on-error pattern as
   `GetRoleByID`) and overwrites the decoded body's `ID` field with it before calling the repository,
   so the path segment — not the body — determines which role is updated.
3. `database/queries/permissions.sql`'s `GetPermissionByID` is changed from `:many` to `:one`,
   matching `GetRoleByID`'s existing, correct pattern exactly. Regenerated via `sqlc generate`; no
   changes needed in `permission_handler.go` itself, since it was already written assuming `:one`
   semantics — fixing the query annotation alone makes that existing code reachable and correct.

Fix (3) is a deliberate wire-format change for `GET /permissions/{id}`: found responses change from a
1-element array to a single object, and not-found responses change from `200 []` to `404
{"error": "..."}`. This endpoint requires `read:permission:any` (admin-only, low traffic), and the
change corrects a confirmed dead code path rather than altering an intentional contract, so it's
treated as a bug fix.

## Consequences

**What becomes easier:**
- `revoke:role:any` is now a meaningful, enforced permission instead of dead seed data — an admin
  can be granted "assign" without "revoke," or vice versa, as originally intended by having two
  separate permissions at all.
- `UpdateRole` can no longer silently update the wrong role via a mismatched body id.
- `GET /permissions/{id}` now actually 404s for a nonexistent permission, and returns a response
  shape consistent with every other `{id}`-based GET endpoint in the codebase.

**What becomes harder or requires attention:**
- Any caller currently holding only `assign:role:any` (not `revoke:role:any`) will lose the ability
  to call the revoke-role endpoint, since that combined behavior was never intentional — Administrator
  and system roles already hold both (every permission is auto-granted to them via the existing
  triggers), so this only affects a custom role that was deliberately scoped to assign-only, which
  would now need `revoke:role:any` granted explicitly if revoke access is actually wanted for it.
- Any client parsing `GET /permissions/{id}` as an array needs to be updated to expect a single
  object instead.
