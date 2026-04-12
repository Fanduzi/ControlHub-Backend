# Backend Phase 4 Integration Smoke Worker Prompt

```text
You are the backend phase-4 integration smoke worker for ControlHub.

Repository:
/Users/fan/GolangProjects/ControlHub

Current goal:
Only close the frontend/backend integration loop. Do not develop new product features.

Context:
- The backend metadata store is MySQL 8.0+.
- The backend uses database/sql + github.com/go-sql-driver/mysql.
- Runtime config uses DATABASE_DSN.
- The frontend calls http://localhost:8080 by default.
- The frontend has already moved to real backend APIs and added loading/error/empty states.
- The HTTP/OpenAPI wire contract uses camelCase.
- Audit events remain a phase-1 bootstrap/demo API backed by MySQL for now; long-term audit storage target is ClickHouse.

Read these files first:
1. /Users/fan/GolangProjects/ControlHub/README.md
2. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml
3. /Users/fan/GolangProjects/ControlHub/migrations/0001_initial_schema.sql
4. /Users/fan/GolangProjects/ControlHub/migrations/0002_seed_reference_data.sql
5. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md

Hard scope boundary:
- Do not add SQL work orders.
- Do not add SQL query workbench.
- Do not add asset edit/create flows.
- Do not add advanced permissions.
- Do not add topology graph.
- Do not add ClickHouse ingestion.
- Do not change the established API response JSON unless a verified bug proves the current contract cannot work.

Tasks:
1. Check repository state first:
   - git status --short
   - If the tree is dirty, report it before editing. Do not overwrite unrelated changes.

2. Start the backend against real MySQL:
   - Use the already verified DSN form:
     controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4
   - Redact the password in reports.
   - Confirm the backend listens on http://localhost:8080.

3. Complete the backend smoke gaps from phase 3:
   - GET /resources?type=database_instance
   - GET /resources?environmentId=<real environment UUID from seed data>
   - GET /resources/{nonexistent-valid-id}
   - GET /resources/{invalid-id}
   - GET /unknown-route
   - If OpenAPI documents additional resource filters, smoke-test those too.

4. Verify browser-facing requirements:
   - Confirm whether a frontend running at http://localhost:3000 can call the backend at http://localhost:8080.
   - If real browser/front-end calls fail because of CORS, implement the minimal local-dev CORS fix only:
     - allow Origin: http://localhost:3000
     - allow methods: GET, POST, OPTIONS
     - allow headers: Authorization, Content-Type
   - Do not implement a broad production CORS policy unless it is already documented.
   - If there is no CORS problem, do not change CORS.

5. Preserve API contract:
   - Keep camelCase response fields.
   - Do not add actorName, targetResourceName, environmentName, or ownerName to backend responses.
   - Do not add pagination in this round.
   - Do not change /resources, /audit-events, /auth/login JSON shapes.

6. Run verification:
   - go test ./internal/api -v
   - go test ./internal/model -v
   - go test ./internal/service -v
   - make test
   - go vet ./...
   - go build ./...

7. Commit rules:
   - If changes are limited to README/.env.example/runbook updates or a necessary local-dev CORS fix, commit them.
   - Do not include AI co-author metadata in the commit message.
   - If the needed fix would change the public API contract, do not commit. Report the exact endpoint, field, and reason for approval first.

Completion report must include:
- changed files
- whether code was modified
- whether CORS was added or changed
- exact smoke-tested URLs and HTTP statuses
- filter smoke-test results
- error-path smoke-test results
- verification command results
- commit hash if committed
- remaining gaps

Do not stop after analysis. Start verification and only fix issues found inside this scope.
```
