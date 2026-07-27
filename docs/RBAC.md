# Roles and Permissions (RBAC)

This document describes Verisafe's role-based access control: the schema, how permission checks are
enforced, every permission string currently in use, and an important operational gap — see
[ADR 0006](adrs/0006-endpoint-permission-enforcement-rollout.md) for the plan to close it.

## Overview

- **Roles** (`roles` table) are named collections of permissions (e.g. `Administrator`, `bot`).
- **Permissions** (`permissions` table) are individual capability strings (e.g. `create:role`).
- **`role_permissions`** joins roles to the permissions they grant.
- **`user_roles`** joins accounts to the roles they hold. An account's effective permissions are the
  union of every permission granted by every role it holds.

Three read views back nearly every query in this subsystem:
- `user_roles_view` — accounts ⋈ `user_roles` ⋈ `roles`
- `user_permissions_view` — accounts ⋈ `user_roles` ⋈ `roles` ⋈ `role_permissions` ⋈ `permissions`
- `role_permissions_view` — `roles` ⋈ `role_permissions` ⋈ `permissions`

## Enforcement

`middleware.IsAuthenticated` (`internal/middleware/auth_middleware.go`) resolves the caller's account
on every request and queries `GetAllUserRoleNames`/`GetUserPermissionNames` **live from the database**
(no caching across requests), storing the results in the request context as `AuthUserRoles` /
`AuthUserPerms`.

`middleware.HasPermission([]string{...})` then checks the required permission(s) against
`AuthUserPerms`:
- **Exact string match only** — no wildcard, prefix, or glob support.
- **ALL-of (AND) semantics** — every string passed to `HasPermission` must be present; there is no
  OR/any-of form. In practice every call site in the codebase passes a single-element slice, so this
  is functionally "has this one permission" everywhere today, but a multi-element call would require
  all of them.
- **Safe by default** — if `AuthUserPerms` is missing from context entirely (e.g. `HasPermission` used
  without `IsAuthenticated` upstream), the check treats it as an empty list and denies.

## Automatic permission grants (database triggers, not visible in Go code)

Two `AFTER INSERT ON permissions` triggers fire on every new permission row:
- `auto_assign_to_system_role` (`database/migrations/20250711051529_roles_and_permissions.sql`) —
  grants the new permission to the `system` role.
- `auto_assign_to_admin_role` (`database/migrations/20250712203316_create_default_permissions_and_roles.sql`) —
  grants the new permission to the `Administrator` role.

Practically: **creating any new permission automatically grants it to `system` and `Administrator`**,
with no application code involved. Anyone calling `POST /permissions/create` should know this —
there's no way to create a permission that *isn't* immediately usable by those two roles.

## Permissions in use

| Permission | Gates |
|---|---|
| `create:role` | `POST /roles/create` |
| `read:role:any` | `GET /roles`, `GET /roles/{id}`, `GET /roles/user/{id}` |
| `read:role:permissions` | `GET /roles/permissions/{id}` |
| `update:role:any` | `PATCH /roles/{id}` |
| `assign:role:any` | `GET /roles/assign/{user_id}/{role_id}` |
| `revoke:role:any` | `DELETE /roles/revoke/{user_id}/{role_id}` |
| `create:permission` | `POST /permissions/create` |
| `read:permission:any` | `GET /permissions`, `GET /permissions/{id}` |
| `read:permission:user` | `GET /permissions/user/{id}` |
| `update:permission:any` | `PATCH /permissions/{id}` |
| `assign:permission:role` | `GET /permissions/assign/{perm_id}/{role_id}` |
| `revoke:permission:role` | `DELETE /permissions/revoke/{perm_id}/{role_id}` |
| `manage:institutions:accounts:any` | Admin override on `POST`/`DELETE /institutions/account` (see ADR 0007) |
| `create:account:any`, `read:account:any`, `read:account:own`, `update:account:own` | account handlers |
| `create:institutions:any`, `update:institutions:any`, `list:institutions:any`, `delete:institutions:any` | institution handlers |
| `create:service_token:own`, `list:service_token:own`/`:any`, `read:service_token:own`/`:any`, `update:service_token:own`/`:any`, `rotate:service_token:own`/`:any`, `revoke:service_token:own`/`:any` | service token handlers |

Note on the `service_token` permissions: the route-level `HasPermission` middleware only requires the
`:own` variant; the handler bodies separately check `AuthUserPerms` for the `:any` variant to decide
whether to bypass the ownership check. A caller holding only `:any` (not `:own`) would be rejected by
the middleware before ever reaching that in-handler bypass — worth knowing if you're granting these
permissions individually rather than via a role that has both.

## Default role at signup

**Every new account automatically receives the `user` role**, and with it
`read:account:own`, `update:account:own`, and `delete:account:own`. This happens in the database,
not in Go — which is why it is easy to miss when reading `upsertAccount`, and why this section
previously claimed the opposite.

Two triggers in
[`20250712210345_add_service_account_support.sql`](../database/migrations/20250712210345_add_service_account_support.sql)
do the work:

- `trigger_assign_default_roles_to_account` — `AFTER INSERT ON accounts`, assigns every role flagged
  `is_default = true`.
- `trigger_assign_default_role_to_all_accounts` — `AFTER INSERT ON roles`, backfills a newly created
  default role onto every existing account.

The same migration seeds `user` with `is_default = true`.

**Practical consequence:** a user-facing endpoint *can* be gated on a permission, provided that
permission is attached to the `user` role in the same migration:

```sql
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r, permissions p
WHERE r.name = 'user' AND p.name = 'your:new:permission'
ON CONFLICT DO NOTHING;
```

No `user_roles` backfill is needed — the triggers already guarantee coverage. The
`AFTER INSERT ON permissions` auto-grant triggers cover only `system` and `Administrator`, so any
other role (including `bot`) needs an explicit grant like the one above.

Verify the invariant before relying on it:

```sql
SELECT count(*) FROM accounts a WHERE NOT EXISTS (
  SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id
  WHERE ur.user_id = a.id AND r.name = 'user');
-- expect 0
```

[ADR 0006](adrs/0006-endpoint-permission-enforcement-rollout.md) predates this understanding and
describes a default-role rollout that the triggers already provide.
