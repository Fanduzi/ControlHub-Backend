# Backend Phase 7 Resource Taxonomy Worker Prompt

```text
You are the backend phase-7 worker for ControlHub.

Repository:
/Users/fan/GolangProjects/ControlHub

Current goal:
Lay the next layer of CMDB/resource-model foundation by formalizing resource-type and relation-type dictionaries, and expanding the backend schema to allow the next planned resource families without yet building CRUD or topology features on top of them.

Context:
- Current backend resource schema still restricts `resource_type` to:
  - host
  - database_instance
  - database_cluster
  - service
- Earlier design decisions already recorded in spec say future resource families should include:
  - domain_name
  - virtual_ip
  - database_proxy
  - control_plane_component
- Earlier design decisions also say future relation types should include:
  - points_to
  - fronts
  - manages
  - replicates_to
- This phase is about controlled vocabulary and schema readiness, not yet about building topology UI or new asset CRUD screens.

Scope:
- backend only
- schema and dictionary groundwork only
- no frontend changes
- no topology endpoint yet
- no SQL work orders
- no query workbench
- no asset editing UI/API beyond existing foundation
- no EAV

Required outcome:
1. Backend has a formal read-only dictionary surface for resource types and relation types.
2. Backend schema allows the next planned resource families.
3. Docs and OpenAPI match the new allowed vocabulary.
4. Existing contracts remain stable.

Recommended implementation direction:
- Add read-only endpoints such as:
  - `GET /resource-types`
  - `GET /relation-types`
- Return simple list payloads under `{ "items": [...] }`
- Item shape should be readable and stable, e.g.:
  - `key`
  - `label`
  - `description`
- Keep camelCase JSON

Recommended resource types to include:
- host
- database_instance
- database_cluster
- service
- domain_name
- virtual_ip
- database_proxy
- control_plane_component

Recommended relation types to include:
- depends_on
- member_of
- runs_on
- points_to
- fronts
- manages
- replicates_to

Schema work:
- Update the `resources` check constraint so the new resource types are allowed.
- Do not seed fake full resources for the new families unless required for a focused test.
- If relation types are currently unconstrained, it is acceptable to leave them unconstrained in schema for now and only formalize them in dictionaries.
- If adding a relation-type constraint is low risk and clean, you may do it, but do not widen scope into migration churn unless clearly worthwhile.

Read these files first:
1. /Users/fan/GolangProjects/ControlHub/migrations/0001_initial_schema.sql
2. /Users/fan/GolangProjects/ControlHub/migrations/0002_seed_reference_data.sql
3. /Users/fan/GolangProjects/ControlHub/internal/model/settings.go
4. /Users/fan/GolangProjects/ControlHub/internal/repository/mysql/dictionary_repository.go
5. /Users/fan/GolangProjects/ControlHub/internal/api/dictionary_handler.go
6. /Users/fan/GolangProjects/ControlHub/internal/api/router.go
7. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml
8. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md

Implementation hints:
- You already have dictionary endpoints for environments, owners, and roles.
- Reuse the same style and keep handlers thin.
- If `settings.go` needs new model structs for resource/relation dictionary items, add them there or a nearby focused file.
- Keep repository/service/api layering consistent with the existing backend style.

Verification requirements:
1. Run:
   - go test ./internal/api -v
   - go test ./internal/model -v
   - go test ./internal/service -v
   - make test
   - go vet ./...
   - go build ./...
2. If local MySQL is available, verify:
   - migrations still apply
   - existing seed data still loads
   - `GET /resource-types`
   - `GET /relation-types`
3. Confirm existing endpoints such as `/resources`, `/resources/{id}`, `/audit-events`, `/environments`, `/owners`, `/roles` remain unchanged.

Commit rules:
- backend repo only
- no AI co-author metadata
- do not widen scope into topology projections or asset CRUD

Final report must include:
- changed files
- exact new endpoints and JSON examples
- whether the `resources` check constraint was expanded
- whether relation types were constrained or dictionary-only
- test results
- live verification results
- commit hash
- remaining risks

Do not stop at analysis. Implement and verify.
```
