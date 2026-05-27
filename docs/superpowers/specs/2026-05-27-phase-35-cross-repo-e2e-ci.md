# Phase 35 Cross-Repo E2E CI Design

## Background

The frontend repository already has a quality gate workflow:

```text
.github/workflows/frontend-ci.yml
push / pull_request -> npm run release:local
workflow_dispatch input run_e2e=true -> currently only an input, no E2E job
```

The frontend E2E suite is ready to run against a real backend:

```text
npm run release:e2e
  npm run test:e2e:smoke
  npm run test:e2e:interaction
  npm run test:e2e
```

Playwright already starts:

```text
frontend dev server: http://localhost:3100
API proxy:           http://localhost:8081 -> http://localhost:8080
```

The missing piece is the backend service on `localhost:8080`. Today that
backend is started manually outside the frontend repo. Phase 35 makes the
frontend repo capable of running a manual cross-repo E2E gate by checking out
the private backend repo, starting MySQL, migrating seed data, starting the
backend server, and running the existing Playwright gates.

## Goal

Add a manual frontend GitHub Actions E2E job that verifies the real product path:

```text
private backend checkout
MySQL 8 service
backend migrations + seed data
backend server on :8080
frontend dev server on :3100
frontend API proxy on :8081
npm run release:e2e
```

This is a cross-repo release-readiness gate, not a deployment pipeline.

## Non-Goals

- Do not run full E2E on every push or pull request in Phase 35.
- Do not deploy.
- Do not create tags or releases.
- Do not add a mocked backend.
- Do not duplicate backend migrations or seed data in the frontend repo.
- Do not change product UI behavior.
- Do not change backend product behavior.
- Do not change frontend Playwright tests unless required for CI determinism.
- Do not remove the existing `release-local` fast CI job.
- Do not require manual heavy backend fuzz gates as part of frontend E2E.

## Repository Ownership

The workflow should live in the frontend repository because the E2E suite
validates browser-visible behavior and Playwright configuration.

Backend is a dependency of the frontend E2E job:

```text
frontend repo: Fanduzi/ControlHub-Frontend
backend repo:  Fanduzi/ControlHub-Backend
```

## Required Secret

Because both repositories are private, do not assume the frontend repository's
default `GITHUB_TOKEN` can read the backend repository.

Add one frontend repository secret before enabling the job:

```text
CONTROLHUB_BACKEND_CHECKOUT_TOKEN
```

Recommended properties:

```text
fine-grained personal access token or GitHub App installation token
read-only contents permission
access limited to Fanduzi/ControlHub-Backend
stored only in the frontend repository
```

If this secret is missing, the manual E2E job should fail early with a clear
message before attempting checkout.

## Runtime Architecture

The manual E2E job should use one GitHub-hosted Linux runner:

```text
ubuntu-latest
```

Services:

```text
mysql:8.0
  database: controlhub
  user:     controlhub
  password: controlhub_dev
  root password: root
```

Checked out directories:

```text
.
  frontend repo
controlhub-backend/
  backend repo
```

Backend environment:

```text
APP_PORT=8080
DATABASE_DSN=controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4
JWT_SECRET=ci-e2e-secret
```

Frontend / Playwright environment:

```text
PLAYWRIGHT_PROXY_TARGET=http://localhost:8080
PLAYWRIGHT_PROXY_PORT=8081
NEXT_PUBLIC_API_BASE_URL=/__api
CONTROLHUB_API_BASE_URL=http://localhost:8081
CONTROLHUB_API_PROXY_URL=http://localhost:8081
```

The frontend dev-server wrapper already sets the `CONTROLHUB_API_*` variables,
but the job should set them explicitly for clarity and for helper scripts.

## Backend Startup Contract

The backend does not auto-migrate on startup. The E2E job must run migrations
before `make run`:

```bash
cd controlhub-backend
go install github.com/pressly/goose/v3/cmd/goose@v3.27.0
make migrate-up
make run
```

Backend readiness should be based on:

```bash
curl -fsS http://localhost:8080/health
```

Do not rely on fixed sleeps alone.

## E2E Execution Contract

After backend readiness:

```bash
npm ci
npm run check:e2e-preflight
npm run release:e2e
```

`check:e2e-preflight` should run before Playwright so stale `:3100` or `:8081`
listeners fail with diagnostics.

The Playwright webServer config should continue to start:

```text
frontend dev server on :3100
api-proxy on :8081
```

Do not start those manually in the workflow.

## Artifact Policy

Always upload useful failure artifacts from manual E2E:

```text
playwright-report/
test-results/
backend.log
```

Retention:

```text
7 days
```

The backend log is important because most cross-repo E2E failures are startup,
migration, auth, or API response issues.

## First Implementation Boundary

Phase 35 should implement only this:

```text
workflow_dispatch + run_e2e=true -> release-e2e job
```

Do not add:

```text
push-triggered full E2E
pull-request full E2E
nightly schedule
deployment
release publishing
```

Those can be added after the manual gate proves stable.

## Success Criteria

Phase 35 is complete when:

```text
frontend workflow has a real release-e2e job
manual workflow_dispatch with run_e2e=true checks out backend
MySQL service starts and passes health checks
backend migrations run successfully
backend server responds on /health
npm run release:e2e passes in GitHub Actions
artifacts upload on failure
normal push / pull_request fast CI remains unchanged
```

## Failure Classification

Final reports must classify failures as one of:

```text
secret_missing
backend_checkout_failed
mysql_unhealthy
migration_failed
backend_start_failed
backend_health_timeout
frontend_install_failed
playwright_preflight_failed
playwright_test_failed
artifact_upload_failed
```

Do not summarize a failed run as "CI failed" without identifying the class.

## Rollback

If the manual E2E job is unstable, revert only the workflow change. Do not
disable existing frontend fast CI.
