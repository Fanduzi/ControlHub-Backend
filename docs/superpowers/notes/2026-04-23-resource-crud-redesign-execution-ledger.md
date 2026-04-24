# Resource CRUD Redesign Execution Ledger

## Scope

- Initiative: `resource-crud-redesign`
- Primary spec: `docs/superpowers/specs/2026-04-22-resource-crud-redesign.md`
- Primary implementation plan: `docs/superpowers/plans/2026-04-22-resource-crud-redesign.md`
- Working branch/worktree: `main` in `/Users/fan/GolangProjects/ControlHub/.worktrees/bigint-primary-key-redesign`
- Start date: 2026-04-22
- Owner: main coordinating agent

## Current Decision Boundary

- In scope: audit the implemented backend/backend-adjacent work for subtype dictionary, profile write APIs, editable `name`, embedded create profile support, and structured validation errors; record current execution truth and closure readiness.
- Explicitly out of scope: new implementation work in this ledger pass; frontend reimplementation inside the Go repo; redesigning the approved spec.
- Deferred by user: any new development beyond documenting the current initiative state; any follow-up implementation to close remaining gaps.
- Blocked pending decision: whether to run another implementation round to close the remaining DI/OpenAPI/frontend evidence gaps.

## Task Board

| Task | Summary | Status | Implemented | Spec Review | Code Review | Marked Done | Notes |
|------|---------|--------|-------------|-------------|-------------|-------------|-------|
| 1 | Add resource subtype dictionary and validation | completed | yes | yes | yes | yes | Implemented in taxonomy, handler, router, service, and tests. |
| 2 | Add resource subtype API endpoint | completed | yes | yes | yes | yes | `GET /resource-subtypes` present and covered by handler tests. |
| 3 | Add profile write service + handlers | completed | yes | yes | yes | yes | PUT/PATCH profile flow implemented in service, handler, repository, and tests. |
| 4 | Support embedded profile on create | completed | yes | partial | partial | yes | Service create path calls profile write when `profileSvc != nil`; real server wiring still needs verification/fix. |
| 5 | Allow editable `name` in PATCH | completed | yes | yes | yes | yes | Mutable `name` path implemented in service/repository and covered by tests. |
| 6 | Return structured field-level validation errors | completed | yes | yes | yes | yes | Validation details exposed from service/handler and reflected in recent commit history. |
| 7 | Rewrite frontend create/edit forms and error mapping | partial | partial | no | no | yes | Frontend evidence exists in the separate frontend repo/worktree, but not closed in this backend initiative ledger. |
| 8 | Initiative audit and closure classification | completed | yes | yes | yes | yes | Audited on 2026-04-23 against current code and docs. |

## Execution Log

### 2026-04-23 21:38

- Action: Audited the approved plan/spec against the current backend worktree and known frontend companion repo state.
- Evidence: `internal/model/taxonomy.go`, `internal/api/dictionary_handler.go`, `internal/api/router.go`, `internal/service/profile_service.go`, `internal/service/resource_service.go`, `internal/api/resource_handler.go`, `internal/openapi/openapi.yaml`, recent commits `b46871f`, `a38988a`, `0c93c98`, `e3a11f4`, `154def3`.
- Outcome: Confirmed that the initiative is largely implemented, but not execution-closed.
- Next: Record the remaining gaps explicitly and mark the initiative as near-complete partial closure.

### 2026-04-23 21:38

- Action: Recorded the key unresolved gap discovered during audit.
- Evidence: `cmd/server/main.go:40`, `cmd/server/main.go:53`, `internal/service/resource_service.go:97`.
- Outcome: Real server DI wiring may skip embedded profile persistence on create because `ResourceService` is constructed without the `ProfileService` dependency.
- Next: Carry this into the closure note as the primary follow-up item.

### 2026-04-23 21:38

- Action: Classified frontend status conservatively.
- Evidence: approved plan points to `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign`; backend worktree does not contain the planned frontend files.
- Outcome: Frontend work is not treated as fully closed in this ledger because durable cross-repo execution evidence is incomplete here.
- Next: Keep frontend completion as partial until cross-repo closure evidence is attached or a separate frontend ledger exists.

## Validation Evidence

- Unit / package tests: backend tests for taxonomy, dictionary handler, resource/profile flows were present from implementation history; no new tests were run in this documentation pass.
- Integration tests: not run in this documentation pass.
- Runtime/manual smoke: not run in this documentation pass.
- Contract/schema validation: audit included `internal/openapi/openapi.yaml`; editable `name` documentation still needs explicit verification.
- Known skipped validation: no cross-repo frontend verification run during this ledger creation; no re-test of real server create-with-profile path in this pass.

## Open Issues

- Issue: Real server create path may not persist embedded `profile` because `ResourceService` is created without `ProfileService` injection.
  - Severity: high
  - Owner: next implementation round
  - Blocking: blocks full closure
  - Plan: wire `ProfileService` into `ResourceService` in `cmd/server/main.go` and re-verify create flow.

- Issue: OpenAPI contract needs explicit re-check that `PATCH /resources/{id}` documents editable `name`.
  - Severity: medium
  - Owner: next implementation round
  - Blocking: blocks full spec closure
  - Plan: verify/update the schema and run contract validation.

- Issue: Frontend create/edit dynamic form completion is not durably evidenced in this repo-level ledger.
  - Severity: medium
  - Owner: cross-repo follow-up
  - Blocking: blocks full initiative closure
  - Plan: either attach frontend repo evidence here or create a matching frontend closure note.

## Closure Decision

- Final status: partial
- What shipped: backend subtype dictionary, subtype API endpoint, profile write APIs, editable `name`, structured validation error responses, and most of the backend contract changes needed for the redesigned resource create/edit flow.
- What did not ship: verified full closure of the real server embedded-profile create path; complete repo-local evidence for frontend dynamic create/edit form closure; final execution-time gate tracking during original implementation.
- Why closure is acceptable: the implementation is materially present and no longer belongs in “not started” state; this ledger documents that the remaining work is follow-up closure and targeted fixes, not a fresh redesign.
- Rollback / fallback: continue using the current implemented backend behavior and treat embedded create-profile persistence plus contract verification as the next hardening patch before declaring the initiative fully completed.
- Follow-up initiative: targeted resource CRUD closure pass to fix DI wiring, re-verify OpenAPI, and attach frontend closure evidence.
