# 7. Authorization ownership checks for account-scoped endpoints

Date: 2026-07-12

## Status

accepted

## Context

Two request-body-driven endpoints accept an arbitrary `account_id` with no check that it belongs to
the caller:

- `POST /users/activity/complete` (`streak_handler.go`'s `RecordUserActivity`) decodes
  `repository.RecordActivityCompletionParams{AccountID, ActivityID, Metadata}` straight from the body.
  Any authenticated user can record an activity completion — and the points it awards — against any
  other account.
- `POST`/`DELETE /institutions/account` (`institution_handler.go`'s `AddAcountInstitution`/
  `RemoveAccountInstitution`) decode `{AccountID, InstitutionID}` the same way. Any authenticated user
  can link or unlink *any* account to/from *any* institution. This one was already flagged by an
  existing TODO in the code: `// TODO: (erick) Add fine permissions for both admin and the user in
  question` — the wording itself implies the intended fix is self-service **or** admin override, not
  a strict lockdown.

Neither gap needs the RBAC/role system to fix — every caller already carries their own identity via
the JWT `Subject` claim (`middleware.ClaimsFromContext`), so an ownership check is sufficient and
independent of any permission grants. `account_handler.go` already establishes this exact pattern
(`if accData.ID.String() != claims.Subject`, `account_handler.go:630,734`).

## Decision

- **`RecordUserActivity`**: strict self-only. No other endpoint or workflow in the codebase implies a
  legitimate "record activity on behalf of another user" use case, so the fix rejects with 403 whenever
  `requestBody.AccountID.String() != claims.Subject`.
- **`AddAcountInstitution`/`RemoveAccountInstitution`**: self-service **or** admin override, per the
  TODO's own wording. A new permission, `manage:institutions:accounts:any`, gates the admin path — a
  request is allowed if `req.AccountID.String() == claims.Subject` **or** the caller's permissions
  (already available via `r.Context().Value(middleware.AuthUserPerms)`, the same in-handler pattern
  `service_token_handler.go` uses for its own `:any`-permission bypass checks) include
  `manage:institutions:accounts:any`. The permission is seeded via migration
  (`20260712120000_add_manage_institutions_accounts_permission.sql`); the two existing
  `AFTER INSERT ON permissions` triggers auto-grant it to `system` and `Administrator` — verified
  against a live database after running the migration. No backfill needed: those are the only two
  roles that should have it, and both already exist.
- The now-resolved TODO comment is removed.

Neither fix touches the route-level middleware (`IsAuthenticated` only, unchanged) — the check lives
in the handler body, matching how `service_token_handler.go` already does ownership-vs-admin
distinctions.

## Consequences

**What becomes easier:**
- Closes two straightforward IDOR (insecure direct object reference) gaps without needing the broader
  endpoint-permission-enforcement rollout (ADR 0006) or its default-role/backfill prerequisites — these
  two didn't need a role system at all, only an identity check every caller already has.
- Institution account management keeps working exactly as today for self-service callers (the
  overwhelming majority of traffic, presumably), while admin/bulk workflows continue to work for
  whoever holds `Administrator` (automatic) or a custom role explicitly granted the new permission.

**What becomes harder or requires attention:**
- Any client currently relying on being able to record activity completions or manage institution
  memberships for *other* accounts (if such a client exists and isn't `Administrator`/`system`) will
  start getting 403s. No such use case was found anywhere in the codebase, but this is worth confirming
  with whoever owns client integrations before this ships, since it's a real behavior removal for
  whatever was previously (unintentionally) possible.
