# 9. Handler and service layering

Date: 2026-07-28

## Status

accepted

## Context

The service ran two incompatible handler architectures side by side, split
along the line of whether a handler predated the OAuth broker work.

The older one, nine files and roughly 5,500 lines covering 47 routes, reached
through `middleware.GetDBConnFromContext` for a `*pgxpool.Conn` planted in the
request context under a string key, hand-rolled `Begin`/`Rollback`/`Commit`,
called `repository.New(tx)` inside the handler body, and wrote errors as one of
141 `http.Error` calls or roughly 130 inline `map[string]string{"error": ...}`
blocks spanning 57 distinct strings. Business logic — validation, authorization,
event publishing, worker pools — lived in the handler bodies.

None of it had tests, for one precise reason: `repository.New(tx)` was
constructed a call frame below any available seam, so the already-generated
`MockQuerier` could not be injected. Coverage of those paths required a live
Postgres. Three separate source comments recorded this as a known, accepted gap.

ADR 0003 promoted `WriteJSON`, `WriteError` and `HandleError` into
`internal/core` and applied them to `internal/auth`, explicitly leaving the
rest of the handlers out of scope. This ADR is the continuation it invited.

## Decision

### Package layout

`internal/handlers` and `internal/service` are split one package per domain:
`handlers/{account,activity,device,health,institution,leaderboard,oauth,
permission,role,servicetoken,social,streak}` and `service/{device,grants,
permission,role}`.

This renames 16 of 51 OpenAPI definition keys (`handlers.X` to its new package,
`service.DeviceOutput` to `device.`, `service.GrantView` to `grants.`). That is
a spec-document change, not a wire change: every path, method, status code and
response body is byte-identical, so clients making HTTP calls directly are
unaffected and only OpenAPI code generators need to regenerate. It was done in
one commit so the rename is a single reviewable event.

### Handlers depend on a service abstraction

Each handler carries a `Service func(repository.Querier) Service` factory field,
wired at the composition root. This is not a new pattern; it is the `grants`
field `internal/auth` already used, promoted to the standard. The field is the
testing seam: a test supplies a factory returning a stub and needs no Querier,
no `pgx.Rows` and no database.

### Response types stay as the sqlc row types

Service methods return `repository.X` verbatim for existing endpoints. The
response bytes those endpoints emit today *are* those structs' JSON, and
maintaining a parallel hand-written DTO with field-for-field, tag-for-tag,
nil-for-nil parity across ~100 generated types is the largest silent-drift
surface available, for no present benefit.

New endpoints define their own output types, as `device.DeviceOutput` already
does. When an existing endpoint genuinely needs to diverge from its row shape,
that is a versioned change with its own client coordination, not a side effect
of a refactor.

### Errors: services return sentinels, handlers own the wording

Services return bare `core` sentinels. Handlers hold every user-facing string in
a `messages.go` of frozen constants, and pair one with a sentinel via
`core.Public`. Without that, routing a migrated 500 through `HandleError` would
silently replace whatever wording the endpoint shipped with "something went
wrong" — across 57 distinct strings.

Near-duplicate messages differing by one word are kept apart. Both ship today on
different endpoints; collapsing them would be a wire change dressed as tidying.

### Transactions

`core.InTx` owns acquire, begin, commit and release as a single call. Besides
removing boilerplate this closes a connection leak: because `WithTransaction`
owns the `Release`, any early return between `Acquire` and it leaked a pooled
connection permanently.

### `http.Error` becomes `core.WriteError`

Five handlers report failures through `http.Error`, which sets
`Content-Type: text/plain; charset=utf-8` and `X-Content-Type-Options: nosniff`.
Three of those sites pass JSON-shaped bodies, so the body already parses as JSON
while the header says otherwise.

Migrating them to `core.WriteError` changes those two headers. Status codes and
message text are unaffected. This is accepted as a deliberate one-time delta,
following the precedent ADR 0003 set for `internal/auth`'s three handlers. It is
pinned by byte-exact tests so it lands as a visible diff rather than silently.

## Consequences

Behaviour is unchanged except where stated. Two deltas apply to every migrated
endpoint and are accepted rather than hidden:

- Read paths previously ended in rollback and now commit. Identical over the
  wire, but a commit failure would surface as a 500 where it used to return
  200. On a healthy connection that cannot happen.
- Validation now runs before the connection is acquired, so a malformed body
  sent while the database is down returns 400 rather than 500.

One defect was fixed as a direct consequence. Every legacy method did
`tx, _ := conn.Begin(ctx)`, discarding the error, then `defer tx.Rollback(ctx)`
on the resulting nil interface. A failed `Begin` panicked, dropping the
connection instead of returning the 500 every other failure path produced.
`core.InTx` returns that error.

Removing `middleware.WithDBConnection` halves peak connection usage per
request. It held a connection for the whole request, while `IsAuthenticated`
released its own before invoking the handler; an authenticated request went
from two connections held concurrently to one, and an unauthenticated one from
one to none.

Three structural invariants now guard the remaining migrations. The OpenAPI
spec must regenerate to a zero diff, since it encodes every route, status code
and response type. The route table golden in `internal/app` must be unchanged.
The byte-exact wire tests must pass untouched. Each caught nothing during the
role and permission migrations, which is the evidence those moved nothing.

`role` and `permission` are migrated. The remaining seven handlers —
`social`, `leaderboard`, `activity`, `streak`, `institution`, `servicetoken`,
`account` — follow the same recipe. The permission migration required no new
primitive of any kind, which was the criterion for calling the recipe repeatable.
