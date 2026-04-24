# 2026-04-23 Resource CRUD Redesign Closure

This note records the audited closeout state for the resource CRUD redesign as of 2026-04-23.

## Outcome

- The backend portion of the redesign is substantially implemented.
- The initiative should not be treated as “not started” or “still design-only”.
- The initiative is not fully closed because a small number of execution and spec-alignment gaps remain.
- The correct final state for this round is **partial, near-complete**.

## What Shipped

- Resource subtype dictionary support and subtype validation were added.
- `GET /resource-subtypes` exists and is wired into the backend dictionary surface.
- Profile write APIs exist for resource profile create/replace and patch-style updates.
- `POST /resources` accepts embedded `profile` input at the service/model layer.
- `PATCH /resources/{id}` supports editable `name` while keeping immutable fields protected.
- Structured field-level validation errors were added to the resource write flow.

## Evidence

Key implementation evidence:

- `internal/model/taxonomy.go`
- `internal/api/dictionary_handler.go`
- `internal/api/router.go`
- `internal/service/profile_service.go`
- `internal/service/resource_service.go`
- `internal/api/resource_handler.go`
- `internal/openapi/openapi.yaml`

Key related commits from this workstream:

- `b46871f` — add GET `/resource-subtypes` API endpoint
- `a38988a` — add profile write repository methods + profile summary support
- `0c93c98` — add profile write service and PUT/PATCH API endpoints
- `e3a11f4` — support embedded profile in create + make name editable in PATCH
- `154def3` — structured validation error responses with field-level details

## What Did Not Fully Close

- Real server DI wiring still needs explicit confirmation/fix so embedded `profile` on create always persists in production wiring.
- OpenAPI still needs explicit closure-time verification that editable `name` is fully reflected in the PATCH contract.
- Frontend create/edit redesign evidence exists outside this Go repo flow and was not durably closed here with an initiative-level execution ledger during implementation.

## Primary Remaining Gap

The main backend closure blocker is the runtime wiring path:

- `cmd/server/main.go:40`
- `cmd/server/main.go:53`
- `internal/service/resource_service.go:97`

`ResourceService.Create` only writes embedded profile data when `profileSvc != nil`. The audited server wiring constructs `ResourceService` without injecting that dependency, so this path is not yet safe to mark fully closed.

## Why Partial Closure Is Acceptable

- The initiative’s main backend goals were implemented and are present in code, tests, and commit history.
- The remaining items are targeted follow-up closure items, not evidence that the redesign failed or never landed.
- Marking this initiative as partial prevents fake completion while also avoiding the opposite mistake of treating finished work as absent.

## Rollback / Fallback

- Keep the current backend implementation as the baseline.
- If the embedded create-profile path proves incorrect in runtime testing, the fallback is to continue using explicit profile write endpoints until the DI wiring patch lands.
- No schema rollback is required for the remaining closure work.

## Follow-Up

A short follow-up pass should:

- wire `ProfileService` into the real server `ResourceService`
- re-verify the OpenAPI PATCH contract for editable `name`
- attach or create durable closure evidence for the corresponding frontend form work

## Final Status

- Final status: `partial`
- Recommended label: `near-complete`
