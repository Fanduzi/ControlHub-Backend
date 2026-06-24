# Phase 38A Query Credential Metadata Management — Backend Evidence

Scope: backend only (Tasks B1–B6). Frontend (F1–F3) is a separate repo and out of
scope for this worker.

Worktree: `.worktrees/backend-phase-38a-query-credential-metadata`
Branch: `phase-38a-query-credential-metadata` (base: `main` @ `e7e2a50`)

## Backend Commits

| Task | Commit | Subject |
|------|--------|---------|
| B1 | `9b503be` | feat: add query credential metadata model |
| B2 | `f8ef4c6` | feat: add query credential metadata repository operations |
| B3 | `fa6bd2c` | feat: add query credential metadata service |
| B4 | `0b5eb61` | feat: add query credential metadata API |
| B5 | `ffba953` | docs: document query credential metadata API |
| B6 | `bc58b74` | test: cover query credential metadata API |

Frontend commits: none (out of scope).

## API Paths (verified from `internal/api/router.go`)

All three routes are mounted under `requireFreshQueryActor` (fresh bearer token).
PUT/DELETE additionally require the `admin` role, enforced in the handler. No
`/api/v1` prefix exists in this codebase.

- `GET    /query-targets/{id}/credential`
- `PUT    /query-targets/{id}/credential`
- `DELETE /query-targets/{id}/credential`

### Request shape (PUT) — metadata only

```json
{
  "credentialRef": "ORDER_MYSQL_RO",
  "enabled": true,
  "environmentPolicy": "non_prod_only",
  "confirmAllEnvironments": false
}
```

`all_environments` requires `confirmAllEnvironments: true`. Unknown fields
(including `dsn`, `password`, `host`, `port`, `actorUserId`) are rejected with 400
by strict JSON decoding (`DisallowUnknownFields`).

### Response shape (GET / PUT) — metadata only

```json
{
  "resourceId": 22,
  "configured": true,
  "engine": "mysql",
  "credentialRef": "ORDER_MYSQL_RO",
  "enabled": true,
  "environmentPolicy": "non_prod_only",
  "runtimeStatus": "secret_resolved",
  "executionEligible": true,
  "message": "Read-only credential is configured and bound to this target."
}
```

DELETE returns `204 No Content`.

## Auth / Admin Behavior

- All three routes require a fresh bearer token (same bounded-TTL policy as query
  execution, `requireFreshQueryActor`).
- GET is available to any authenticated actor.
- PUT/DELETE require `role == "admin"`. The handler enforces it via
  `actorRoleFromContext` (403 for non-admin, service never called); the service
  re-enforces (`ErrQueryCredentialForbidden`) as defense in depth.
- The actor user id comes from the verified token (stored in context by the
  middleware); it is never accepted from the request body.
- The metadata engine is derived from the selected target, never from the request.

## Readiness Correction Proof

Before Phase 38A, `completeQueryTarget` marked a target ready on metadata + policy
alone — it never checked whether the secret actually resolved or bound.

The correction (`internal/service/query_target_service.go`):

- `QueryTargetService.WithCredentialResolver` wires an optional resolver
  (production wires it in `cmd/server/main.go`; the dev-seed path wires only the
  reader, preserving Phase 37 behavior).
- When the resolver is wired, `List` computes
  `InspectCredentialRuntime(...)` and calls `completeQueryTargetWithRuntime`,
  which marks a target ready ONLY when the runtime status is `secret_resolved`.
- Metadata present but secret unresolved → `secret_missing` (locked). Metadata
  present but binding mismatched → `binding_mismatch` (locked). Neither exposes
  `availableActions.run = true`.

Proof tests (all green):

- `internal/service/query_target_service_test.go`:
  `TestCompleteQueryTargetWithRuntime_ReadyOnlyOnSecretResolved`.
- `internal/integration/query_credential_api_test.go`:
  `TestQueryCredentialAPI_SecretResolvedMakesTargetReady` (real MySQL: not ready
  before metadata, not ready while secret missing, ready once secret resolves and
  binds), `TestQueryCredentialAPI_BindingMismatchLocksTarget`.

## No-Secret Proof

The DSN/password never enters request JSON, response JSON, the metadata table,
audit rows, or logs. The resolved DSN exists only transiently inside
`InspectCredentialRuntime` → `resolver.Resolve` → `validateDSNBinding`, and is
never stored, returned, or echoed in errors.

- No DSN/password in request JSON: `QueryCredentialUpsertRequest` has only
  `credentialRef`, `enabled`, `environmentPolicy`, `confirmAllEnvironments`.
  Strict decoding rejects any other field
  (`TestQueryCredential_PutRejectsSecretAndActorFields`). A DSN-shaped
  `credentialRef` value is rejected by `ValidateCredentialRef` (`[A-Z0-9_]+`).
- No DSN/password in response JSON: `QueryCredentialStatusResponse` carries only
  metadata + a fixed `message`; asserted in
  `qcAssertBodyNoDSN` (integration) and the OpenAPI schema
  (`additionalProperties: false`).
- No DSN/password in the metadata table: `UpsertCredentialMetadata` stores only
  `resource_id, engine, credential_ref, enabled, environment_policy`; asserted in
  `TestQueryCredentialRepository_MetadataNeverStoresDSN` and
  `qcAssertMetadataStoresNoDSN`.
- No DSN/password in audit rows: audit events store only
  `actor_user_id, target_resource_id, event_type, result`; asserted in
  `qcAssertAuditStoresNoDSN` and `TestQueryCredentialAPI_AuditAndNoDSN`.

## Backend Verification Matrix

Run from the worktree (Docker available; no gate skipped):

| Command | Result |
|---------|--------|
| `git diff --check` | clean |
| `go test -count=1 ./...` | PASS (all packages) |
| `go vet ./...` | ok |
| `go build ./...` | ok |
| `make openapi-validate` | PASS (`TestOpenAPIYAMLIsValid`) |
| `make test-integration` | PASS (Testcontainers MySQL; dev-seed legacy path unaffected) |
| `make test-openapi-fuzz` | PASS (33/33 operations, 1273/1273 cases) |

## OpenAPI Fuzz Result

`make test-openapi-fuzz` PASSED. 33/33 operations tested (30 + 3 new credential
operations), 1273 generated cases, all checks passed. Two warnings are expected
conformance, not failures:

- "Missing authentication: 5 operations returned only 401/403" — the 3 credential
  endpoints + 2 query-execution endpoints. Declared 401/403 responses, treated as
  conformance per the Phase 38A design.
- "Schema validation mismatch: 3 operations mostly rejected generated data" —
  strict `additionalProperties: false` + `credentialRef` pattern rejecting
  generated data (the intended no-secret/strict-decode behavior).

## GitNexus Result

After refreshing the index from the worktree (`npx gitnexus analyze`), ran change
detection against `main`:

- 20 files, 193 symbols changed, 58 affected execution flows.
- Risk: critical — driven by the readiness correction touching the central
  `GET /query-targets` flow (`HandleListQueryTargets` / `DeriveReadiness`).
- Affected flows are exactly the intended ones: the query-target readiness flows
  (via `List` + `InspectCredentialRuntime` + `completeQueryTargetWithRuntime`),
  `NewRouter` (route additions), and the new credential handler flows
  (`HandleGet/Put/DeleteQueryCredential`). No unexpected flows.

Caveat: GitNexus reports the readiness-correction blast radius as critical. This
was flagged up front (impact on `completeQueryTarget` = HIGH) and handled
surgically: the legacy pure `completeQueryTarget` signature is unchanged (all 12
existing tests untouched); the correction is additive via an optional resolver;
the dev-seed path (resolver absent) preserves Phase 37 behavior; full unit +
integration + fuzz coverage confirms the product path.

## credentialState Frontend Contract (handoff note)

On the runtime-backed readiness path (resolver wired in production),
`QueryTargetGovernance.credentialState` mirrors `QueryCredentialRuntimeStatus`:
`missing_metadata`, `invalid_ref`, `disabled`, `policy_blocked`, `secret_missing`,
`binding_mismatch`, `secret_resolved`, `unsupported_target`,
`incomplete_connection`. The legacy metadata-only path additionally emits
`missing_readonly_credential` / `configured_readonly_credential`. All values are
documented in the OpenAPI `credentialState` enum. The frontend (separate repo, out
of scope) must extend `KNOWN_CREDENTIAL_STATES` and English/Chinese i18n labels
for these values and must never render the raw enum.

## Scope Confirmation

- No frontend edits (separate repo, untouched).
- No secret write API; no secret manager integration.
- No new query engines; no SQL guard relaxation.
- No migration (reused the existing `query_target_credentials` table).
- No export, saved-query, or approval feature.
- No workflow (GitHub Actions) changes.
- No tag, push, release, or deployment.
- No AI co-author in any commit.
