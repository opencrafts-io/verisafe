# 2. Adopt Swagger/OpenAPI for API Documentation

Date: 2026-03-18

## Status

accepted

## Context

Verisafe currently relies on shared Postman collection exports as the primary means of communicating API contracts between backend developers, frontend consumers, and QA. This approach has surfaced several pain points as the project grows:

 - Scalability: Postman exports are manually maintained JSON files. As the number of endpoints grows (devices, auth, users, etc.), keeping exports in sync with the actual implementation becomes a significant overhead and a common source of drift.
 - Repository noise: Committing and updating large Postman JSON blobs inflates diffs, makes code reviews harder to parse, and muddies the git history with non-code changes.
 - Discoverability: New contributors and API consumers have no single, always-current reference. They must either run Postman, hunt down the latest export, or read source code.
 - No contract enforcement: Nothing in the current workflow prevents the implementation from diverging from the shared collection. Bugs caused by undocumented breaking changes are only caught at integration time.
 - Missing meta: Postman exports do not capture response schemas, error shapes, authentication requirements, or deprecation notices in a machine-readable way.

The team is at the point where the cost of maintaining Postman exports is starting to outweigh their value, and a more sustainable documentation strategy is needed.

## Decision

We will adopt **swaggo/swag** to generate Swagger 2.0 / OpenAPI documentation directly from annotation comments co-located with handler code.
 
The implementation will be done in two phases:
 
**Phase 1 – Tooling & scaffolding**
- Install `swaggo/swag` CLI and `swaggo/http-swagger` middleware.
- Add top-level API metadata annotations to `main.go` (title, version, host, base path, and `BearerAuth` security definition).
- Register a `GET /swagger/` route in each handler's `RegisterRoutes` method to serve the Swagger UI.
- Add `swag init` to the local development workflow and CI pipeline so docs are always regenerated from source before builds.
- Commit the generated `docs/` directory to the repository.
 
**Phase 2 – Handler annotation**
- Annotate all existing handlers with `godoc`-style Swagger comments covering: summary, description, tags, accepted content types, request body or query parameters, and all response codes with their schemas (including error shapes).
- Annotate request/response DTOs (`DeviceRegistrationInput`, `DeviceOutput`, etc.) with `example` struct tags to produce useful sample values in the UI.
- Fields that are set server-side (e.g. `IpAddress`, `Country`) will be explicitly excluded from request parameter annotations to accurately reflect the client contract.
 
Postman collections will be retired as the source of truth for API documentation. They may continue to be used informally for manual testing but will no longer be committed to the repository or shared as the canonical reference.
 
## Consequences

 **What becomes easier:**
- **Documentation stays in sync**: Annotations live next to the handler code. A PR that changes an endpoint must update its annotation in the same diff, making drift visible in review.
- **Smaller, cleaner diffs**: Swag-generated files in `docs/` are compact and consistent. Replacing Postman JSON exports removes hundreds of lines of noisy, hard-to-review blob changes from PRs.
- **Self-service for consumers**: Frontend developers and QA can visit `/swagger/` on any running instance and immediately see every available endpoint, its expected inputs, and all documented response shapes — no Postman required.
- **Machine-readable contract**: The generated `swagger.json` / `swagger.yaml` can be consumed by code generators, contract testing tools (e.g. Dredd), and API gateways.
- **Onboarding**: New contributors get a living reference without needing any local setup beyond running the server.
 
**What becomes harder or requires attention:**
- **Annotation discipline**: Developers must remember to write and update annotations. This is a cultural change that needs to be enforced through PR review checklists or CI linting (`swag fmt`).
- **CI step**: `swag init` must be added to CI. A failing generation step (e.g. due to a malformed annotation) will block the build, which is intentional but requires the team to be familiar with swag's annotation syntax.
- **Swagger 2.0 limitations**: `swaggo/swag` generates Swagger 2.0, not OpenAPI 3.0. More complex schemas (e.g. `oneOf`, `anyOf`, nullable fields) are harder to express. If OpenAPI 3.0 features become necessary, migration to `swaggo/swag` v2 or an alternative like `ogen` should be evaluated at that point.
- **Docs directory in source control**: The `docs/` directory is generated, but committing it ensures the Swagger UI works without a build step in development. The team should agree on whether this directory is always regenerated in CI or treated as a checked-in artifact.

