# Backend Phase 6 Local Dev Environment Worker Prompt

```text
You are the backend phase-6 worker for ControlHub.

Repository:
/Users/fan/GolangProjects/ControlHub

Current goal:
Fix the local development startup gap so that the backend can automatically load `.env` during local development, and make the documented startup path actually work for a normal developer.

Context:
- The backend currently reads config only from process environment variables.
- There is already a `.env.example`, and README currently suggests copying it to `.env`.
- In practice, `make run` does not load `.env`, so developers easily start the server with empty `DATABASE_DSN` / `JWT_SECRET`.
- That leads to frontend requests failing with 500s even though the backend process is running.
- This is a local-dev/runtime ergonomics bug, not a business feature.

Scope:
- backend only
- local development startup hardening only
- no API contract changes
- no schema changes
- no auth model changes
- no resource model changes
- no topology work

Required outcome:
- `make run` should work in local development when `.env` exists
- `.env` loading should be automatic in local/dev mode
- production-style explicit environment variables must still work and take precedence
- README and `.env.example` must match actual behavior

Read these files first:
1. /Users/fan/GolangProjects/ControlHub/cmd/server/main.go
2. /Users/fan/GolangProjects/ControlHub/internal/config/config.go
3. /Users/fan/GolangProjects/ControlHub/Makefile
4. /Users/fan/GolangProjects/ControlHub/README.md
5. /Users/fan/GolangProjects/ControlHub/.env.example

Implementation direction:
- use a simple, standard `.env` loader for local development, e.g. `github.com/joho/godotenv`
- load `.env` early during startup, before config is read
- do not fail hard if `.env` is missing
- explicit exported env vars must continue to override `.env` values
- keep behavior simple and obvious

Recommended behavior:
1. On startup, attempt to load `.env`
2. If `.env` is missing, continue normally
3. If `.env` exists, populate process env for missing keys
4. Then read config through the existing config layer

Also fix the documentation gap:
- README must clearly describe:
  - MySQL setup
  - migration application
  - `cp .env.example .env`
  - `make run`
  - health check
  - seeded login credentials
- README must no longer imply contradictory startup behavior

Tests and verification:
1. Run:
   - go test ./internal/api -v
   - go test ./internal/model -v
   - go test ./internal/service -v
   - make test
   - go vet ./...
   - go build ./...
2. Add at least one focused config/startup test if the current structure allows it cleanly
3. If local MySQL is available, verify:
   - `.env` exists
   - plain `make run` starts successfully
   - `curl http://localhost:8080/health`
   - `curl -X POST http://localhost:8080/auth/login ...`
4. Also verify that inline env vars still work and are not broken by `.env` loading

Commit rules:
- backend repo only
- no AI co-author metadata
- do not commit `.env`
- do not widen scope

Final report must include:
- changed files
- whether `.env` is now automatically loaded
- precedence rule between exported env vars and `.env`
- test results
- live verification results
- commit hash
- remaining risks

Do not stop at analysis. Implement and verify.
```
