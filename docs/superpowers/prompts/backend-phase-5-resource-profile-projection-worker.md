# Backend Phase 5 Resource Profile Projection Worker Prompt

```text
You are the backend phase-5 worker for ControlHub.

Repository:
/Users/fan/GolangProjects/ControlHub

Current goal:
Add a read-only resource profile projection contract so the frontend can stop relying on static resourceDetails/profile maps for database engine, version, host, topology, and similar typed profile fields.

Context:
- ControlHub is still in the asset/resource foundation phase.
- Backend stack is Go + chi + database/sql + github.com/go-sql-driver/mysql.
- Metadata store is MySQL 8.0+.
- API wire contract uses camelCase.
- The core resource model is Resource core + typed profile tables + relations.
- The frontend has already completed real API integration and currently enriches profile data from static frontend maps.
- Frontend phase 5 is working on Monochrome Resource Console styling, light/dark/system themes, and zh-CN/en i18n. It should not wait for this backend task.

Hard scope boundary:
- Do not add SQL work orders.
- Do not add SQL query workbench.
- Do not add asset create/edit/delete flows.
- Do not add advanced permissions.
- Do not add topology graph.
- Do not add ClickHouse ingestion.
- Do not introduce EAV.
- Do not put every profile field into the resources table.
- Do not rewrite architecture into heavy DDD.
- Do not change existing /resources or /resources/{id} response JSON.
- Add a new read-only endpoint instead of breaking current frontend integration.

Read these files first:
1. /Users/fan/GolangProjects/ControlHub/README.md
2. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml
3. /Users/fan/GolangProjects/ControlHub/migrations/0001_initial_schema.sql
4. /Users/fan/GolangProjects/ControlHub/migrations/0002_seed_reference_data.sql
5. /Users/fan/GolangProjects/ControlHub/internal/model/resource.go
6. /Users/fan/GolangProjects/ControlHub/internal/repository/mysql/resource_repository.go
7. /Users/fan/GolangProjects/ControlHub/internal/api/resource_handler.go
8. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md

Task 0: inspect current schema and repository shape
1. Run:
   - git status --short
   - grep or rg profile table definitions in migrations/0001_initial_schema.sql
   - inspect seed rows in migrations/0002_seed_reference_data.sql
2. Confirm exact typed profile tables and columns before writing code.
3. If the schema cannot support a profile projection without migration changes, stop and report the smallest required migration. Do not invent new schema casually.

Task 1: define the read-only API contract
Implement:

GET /resources/{id}/profile

Recommended response shape:
{
  "resourceId": "40000000-0000-0000-0000-000000000002",
  "resourceType": "database_instance",
  "resourceSubtype": "mysql",
  "profile": {
    "engine": "MySQL",
    "version": "8.0",
    "host": "prod-db-host-01",
    "port": 3306,
    "clusterId": "40000000-0000-0000-0000-000000000001"
  }
}

Contract rules:
- Use camelCase JSON fields.
- resourceId, resourceType, and resourceSubtype come from the core resources row.
- profile is an object whose keys vary by resourceType/resourceSubtype.
- profile values may be string, number, boolean, or null.
- Avoid nested profile objects for this phase unless the existing schema already requires it.
- If the resource exists but no typed profile row exists, return 200 with profile: {}.
- If the resource does not exist, return 404.
- Keep invalid ID behavior aligned with existing resource handlers. Do not broaden scope just to return 400.
- Do not add actorName, targetResourceName, environmentName, or ownerName.

Task 2: implement model/service/repository/API
Suggested implementation shape:
- internal/model/resource.go:
  - Add ResourceProfileResponse or equivalent.
- internal/repository/mysql/resource_repository.go:
  - Add a read-only method to fetch the core resource by id plus the matching typed profile.
  - Query the typed profile table according to resourceType.
  - Use existing typed profile table columns from migration files.
- internal/service/resource_service.go:
  - Add a thin method that calls repository and preserves business-light boundaries.
- internal/api/resource_handler.go:
  - Add handler for GET /resources/{id}/profile.
- internal/api/router.go:
  - Add route.
- internal/openapi/openapi.yaml:
  - Add /resources/{id}/profile path.
  - Add schema for ResourceProfileResponse.
  - Document that profile keys vary by resourceType/resourceSubtype.
- README.md:
  - Add endpoint to API table and one curl example.

Do not create a generic dynamic SQL layer.
Do not use map[string]any to bypass all structure until you have inspected the typed profile fields. It is acceptable for the public response profile to be map[string]any, but the repository should still be clear and explicit about which table and columns it reads.

Task 3: tests
Add tests covering:
- database_instance profile returns expected fields from seed/fake data.
- database_cluster profile returns expected fields from seed/fake data.
- host profile returns expected fields from seed/fake data.
- service profile returns expected fields from seed/fake data.
- existing resource with no profile returns 200 and profile: {}.
- missing resource returns 404.

Keep tests aligned with existing test style:
- Use fake repositories for API tests where the current codebase does that.
- Add model/service tests only where useful and consistent with current package patterns.
- Do not require live MySQL for unit tests.

Task 4: verification
Run:
- go test ./internal/api -v
- go test ./internal/model -v
- go test ./internal/service -v
- make test
- go vet ./...
- go build ./...

If local MySQL is available, also run live smoke tests:
- GET /resources/{database_instance_id}/profile
- GET /resources/{database_cluster_id}/profile
- GET /resources/{host_id}/profile
- GET /resources/{service_id}/profile
- GET /resources/{missing_valid_id}/profile

Task 5: commit
Commit only backend repository changes.

Commit rules:
- Do not commit .env.
- Do not commit .playwright-mcp.
- Do not commit .superpowers.
- Do not include AI co-author metadata in the commit message.
- If existing /resources or /resources/{id} JSON must change, stop and report before committing.

Completion report must include:
- changed files
- new endpoint path
- JSON examples for at least database_instance and host
- whether existing API contracts changed
- OpenAPI update summary
- tests run and results
- live smoke test results if available
- commit hash if committed
- remaining risks

Do not stop after analysis. Start implementation, but stay inside this read-only profile projection scope.
```
