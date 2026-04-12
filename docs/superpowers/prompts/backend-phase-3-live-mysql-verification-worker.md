# Backend Phase 3 Live MySQL Verification Worker Prompt

```text
You are the backend phase-3 worker for ControlHub.

Repository:
/Users/fan/GolangProjects/ControlHub

Read these files first:
1. /Users/fan/GolangProjects/ControlHub/README.md
2. /Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml
3. /Users/fan/GolangProjects/ControlHub/migrations/0001_initial_schema.sql
4. /Users/fan/GolangProjects/ControlHub/migrations/0002_seed_reference_data.sql
5. /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md

Current backend baseline:
- Metadata store has been switched to MySQL 8.0+.
- Go uses database/sql + github.com/go-sql-driver/mysql.
- DATABASE_DSN is the runtime configuration variable.
- Audit events are phase-1 bootstrap/demo placeholder data in MySQL; long-term target is ClickHouse.
- OpenAPI wire contract uses camelCase.

Your role:
- Verify the backend against a real MySQL 8.0+ database.
- Fix only issues discovered by live MySQL verification.
- Improve local runbook/docs if needed.

Scope for this round:
1. Confirm the current MySQL migration files execute successfully on MySQL 8.0+.
2. Confirm seed data loads successfully.
3. Start the backend with a real DATABASE_DSN.
4. Smoke-test these endpoints against the running server:
   - GET /health
   - POST /auth/login
   - GET /resources
   - GET /resources/{id}
   - GET /resources/{id}/relations
   - GET /resources/{id}/audit-events
   - GET /audit-events
   - GET /environments
   - GET /owners
   - GET /roles
5. Fix MySQL SQL syntax, repository scan, time handling, JSON handling, or DSN issues if they appear.
6. Update README with precise local MySQL setup/run/smoke-test commands.

Do not do:
- Do not touch frontend code.
- Do not add new product features.
- Do not implement SQL work orders, SQL review, query workbench, advanced permissions, topology graphs, or ClickHouse ingestion.
- Do not change the HTTP contract unless a live verification bug proves the current contract cannot work.

Expected local MySQL setup:
- MySQL 8.0+
- Database name: controlhub
- Example DSN:
  root:password@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4

If MySQL is not available:
- Try to use Docker only if available and reasonable.
- If Docker/MySQL is unavailable, do not fake success.
- Report exactly what could not be verified.
- Still run unit tests, go build, and go vet.

Required verification commands:
- go test ./internal/api -v
- go test ./internal/model -v
- go test ./internal/service -v
- make test
- go vet ./...
- go build ./...

If live MySQL is available, also run:
- mysql -e "create database if not exists controlhub character set utf8mb4 collate utf8mb4_0900_ai_ci;"
- mysql controlhub < migrations/0001_initial_schema.sql
- mysql controlhub < migrations/0002_seed_reference_data.sql
- DATABASE_DSN="root:password@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4" make run
- curl http://localhost:8080/health
- curl http://localhost:8080/resources
- curl http://localhost:8080/environments
- curl http://localhost:8080/owners
- curl http://localhost:8080/roles

Use the actual local user/password/DSN available in the environment. Do not hardcode root:password if it is not valid locally.

Completion report must include:
- changed files
- whether migrations executed against real MySQL
- exact DSN form used, with password redacted
- smoke-tested endpoints and status
- tests run
- remaining live-verification gaps
- commit hash

Do not stop after analysis. Start verification and fix issues found.
```
