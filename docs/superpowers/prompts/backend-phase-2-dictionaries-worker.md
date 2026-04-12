# Backend Phase 2 Dictionaries Worker Prompt

```text
You are the backend phase-2 worker for ControlHub.

Repository:
/Users/fan/GolangProjects/ControlHub

Read these files first:
1. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md
2. /Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-11-unified-resource-console-phase-1.md
3. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml
4. /Users/fan/GolangProjects/ControlHub/README.md

Current backend baseline:
- Phase-1 backend implementation exists.
- HTTP JSON/OpenAPI wire contract is frozen as camelCase.
- Go toolchain is aligned to Go 1.26.x, with local asdf default Go 1.26.1.
- Existing backend endpoints include:
  - POST /auth/login
  - GET /health
  - GET /resources
  - GET /resources/{id}
  - GET /resources/{id}/relations
  - GET /resources/{id}/audit-events
  - GET /audit-events

Your role:
- Implement the next backend slice only: basic dictionary/reference-data APIs for frontend view-model enrichment.
- Stay inside /Users/fan/GolangProjects/ControlHub.
- Keep the backend pragmatic and layered. Do not introduce DDD-heavy abstractions.

Scope for this round:
1. Add read-only environment, owner, and role endpoints:
   - GET /environments
   - GET /owners
   - GET /roles
2. Add or complete model/service/repository/api code needed for those endpoints.
3. Update OpenAPI so the contract is explicit and camelCase.
4. Add handler/service/repository tests appropriate for the existing project style.
5. Update README only if the new endpoints or verification steps need to be documented.

Do not do:
- Do not touch frontend code.
- Do not add SQL work orders, SQL review, query workbench, or change workflows.
- Do not implement fine-grained permissions.
- Do not add automatic discovery or batch import.
- Do not change the already frozen resource/audit/auth wire fields unless there is a correctness bug.

Required wire contracts:

GET /environments
Response:
{
  "items": [
    {
      "id": "env-prod",
      "name": "Production",
      "slug": "prod",
      "description": "Production environment",
      "createdAt": "2026-04-11T20:00:00Z"
    }
  ]
}

GET /owners
Response:
{
  "items": [
    {
      "id": "owner-dba",
      "name": "DBA Team",
      "email": "dba@example.com",
      "createdAt": "2026-04-11T20:00:00Z"
    }
  ]
}

GET /roles
Response:
{
  "items": [
    {
      "id": "role-admin",
      "name": "admin",
      "description": "Full platform access",
      "createdAt": "2026-04-11T20:00:00Z"
    }
  ]
}

Implementation constraints:
- Preserve database column names as snake_case.
- Preserve HTTP JSON as camelCase.
- Keep handlers thin.
- Keep repository SQL readable.
- Keep response envelopes as { "items": [...] } for list endpoints.
- If existing seed IDs differ from the example IDs above, use the existing seed IDs but preserve the field names and response shape.

Verification:
Run and report:
- go test ./internal/api -v
- go test ./internal/model -v
- go test ./internal/service -v
- make test

If possible, also run the server and smoke-test:
- curl http://localhost:8080/environments
- curl http://localhost:8080/owners
- curl http://localhost:8080/roles

If MySQL or the mysql client is unavailable, say exactly what could not be verified and why.

Completion report must include:
- changed files
- tests run
- final JSON examples for /environments, /owners, /roles
- commit hash
- any remaining contract assumptions

Do not stop after analysis. Start implementing.
```
