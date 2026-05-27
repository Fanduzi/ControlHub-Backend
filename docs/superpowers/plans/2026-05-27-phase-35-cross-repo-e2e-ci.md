# Phase 35 Cross-Repo E2E CI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manual frontend GitHub Actions E2E job that checks out the private backend repo, starts MySQL and the backend server, then runs the existing frontend Playwright release E2E gates.

**Architecture:** Keep fast frontend CI unchanged. Add a separate `release-e2e` job gated by `workflow_dispatch` input `run_e2e=true`. The job runs entirely on one GitHub-hosted runner: MySQL service, backend checkout/startup, frontend install, Playwright web servers, and artifact upload.

**Tech Stack:** GitHub Actions, Next.js, Playwright, Node 22, Go 1.26.1 from backend `go.mod`, MySQL 8.0 service container, goose migrations.

---

## Required Reading

```text
docs/superpowers/specs/2026-05-27-phase-35-cross-repo-e2e-ci.md
.github/workflows/frontend-ci.yml
package.json
playwright.config.ts
e2e/api-proxy.mjs
e2e/harness/dev-server-wrapper.sh
scripts/check-e2e-preflight.mjs
```

Backend references:

```text
/Users/fan/GolangProjects/ControlHub/README.md
/Users/fan/GolangProjects/ControlHub/Makefile
/Users/fan/GolangProjects/ControlHub/go.mod
/Users/fan/GolangProjects/ControlHub/internal/config/config.go
```

## Worktree

Create a frontend worktree:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-35-cross-repo-e2e-ci -b feat/phase-35-cross-repo-e2e-ci main
cd .worktrees/frontend-phase-35-cross-repo-e2e-ci
git status --short --branch
```

Expected: clean branch.

## Precondition: Repository Secret

Before expecting the remote manual E2E job to pass, the frontend GitHub repo
must have this secret:

```text
CONTROLHUB_BACKEND_CHECKOUT_TOKEN
```

Token requirements:

```text
read-only access to Fanduzi/ControlHub-Backend contents
no write permission
no package/deployment/admin permission
stored in Fanduzi/ControlHub-Frontend repository secrets
```

If the secret is not configured, the job should fail early with this message:

```text
CONTROLHUB_BACKEND_CHECKOUT_TOKEN is required to checkout the private backend repository.
```

## Constraints

- Do not change product UI.
- Do not change Playwright test semantics unless CI exposes a real deterministic issue.
- Do not change backend code.
- Do not change backend workflow.
- Do not run E2E on push or pull_request in this phase.
- Do not add a mocked backend.
- Do not duplicate backend migrations or seed data.
- Do not deploy, tag, or release.
- No AI co-author.

---

## Task 1: Add Manual `release-e2e` Job

**Modify:**

```text
.github/workflows/frontend-ci.yml
```

- [ ] Keep the existing `release-local` job unchanged.

- [ ] Add a new job after `release-local`:

```yaml
  release-e2e:
    name: release-e2e
    runs-on: ubuntu-latest
    if: github.event_name == 'workflow_dispatch' && inputs.run_e2e == 'true'
    timeout-minutes: 25
    services:
      mysql:
        image: mysql:8.0
        env:
          MYSQL_ROOT_PASSWORD: root
          MYSQL_DATABASE: controlhub
          MYSQL_USER: controlhub
          MYSQL_PASSWORD: controlhub_dev
        ports:
          - 3306:3306
        options: >-
          --health-cmd="mysqladmin ping -h 127.0.0.1 -uroot -proot"
          --health-interval=10s
          --health-timeout=5s
          --health-retries=12
    env:
      BACKEND_DIR: controlhub-backend
      DATABASE_DSN: controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4
      JWT_SECRET: ci-e2e-secret
      APP_PORT: "8080"
      PLAYWRIGHT_PROXY_TARGET: http://localhost:8080
      PLAYWRIGHT_PROXY_PORT: "8081"
      NEXT_PUBLIC_API_BASE_URL: /__api
      CONTROLHUB_API_BASE_URL: http://localhost:8081
      CONTROLHUB_API_PROXY_URL: http://localhost:8081
```

- [ ] Add an early secret guard step:

```yaml
      - name: Verify backend checkout token is configured
        env:
          BACKEND_CHECKOUT_TOKEN: ${{ secrets.CONTROLHUB_BACKEND_CHECKOUT_TOKEN }}
        run: |
          if [ -z "$BACKEND_CHECKOUT_TOKEN" ]; then
            echo "CONTROLHUB_BACKEND_CHECKOUT_TOKEN is required to checkout the private backend repository."
            exit 1
          fi
```

- [ ] Add frontend checkout:

```yaml
      - name: Checkout frontend
        uses: actions/checkout@v4
```

- [ ] Add backend checkout:

```yaml
      - name: Checkout backend
        uses: actions/checkout@v4
        with:
          repository: Fanduzi/ControlHub-Backend
          token: ${{ secrets.CONTROLHUB_BACKEND_CHECKOUT_TOKEN }}
          path: ${{ env.BACKEND_DIR }}
```

Expected: backend repo appears under `controlhub-backend/`.

## Task 2: Install Frontend And Backend Toolchains

**Modify:**

```text
.github/workflows/frontend-ci.yml
```

- [ ] Add Node setup:

```yaml
      - name: Setup Node
        uses: actions/setup-node@v4
        with:
          node-version: "22"
          cache: npm
```

- [ ] Add Go setup using the backend module:

```yaml
      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version-file: ${{ env.BACKEND_DIR }}/go.mod
          cache-dependency-path: ${{ env.BACKEND_DIR }}/go.sum
```

- [ ] Install frontend dependencies:

```yaml
      - name: Install frontend dependencies
        run: npm ci
```

- [ ] Install backend migration tool:

```yaml
      - name: Install goose
        working-directory: ${{ env.BACKEND_DIR }}
        run: go install github.com/pressly/goose/v3/cmd/goose@v3.27.0
```

Reason: backend `Makefile` expects `$(go env GOPATH)/bin/goose`.

## Task 3: Migrate And Start Backend

**Modify:**

```text
.github/workflows/frontend-ci.yml
```

- [ ] Add MySQL readiness check:

```yaml
      - name: Wait for MySQL
        run: |
          for i in {1..30}; do
            if mysqladmin ping -h 127.0.0.1 -uroot -proot --silent; then
              exit 0
            fi
            sleep 2
          done
          echo "MySQL did not become ready."
          exit 1
```

If `mysqladmin` is missing on `ubuntu-latest`, replace the step with:

```yaml
      - name: Install MySQL client
        run: sudo apt-get update && sudo apt-get install -y mysql-client
```

and keep the readiness step after it.

- [ ] Run backend migrations:

```yaml
      - name: Run backend migrations
        working-directory: ${{ env.BACKEND_DIR }}
        run: make migrate-up
```

- [ ] Start backend in the background:

```yaml
      - name: Start backend server
        working-directory: ${{ env.BACKEND_DIR }}
        run: |
          nohup make run > ../backend.log 2>&1 &
          echo $! > ../backend.pid
```

- [ ] Wait for backend health:

```yaml
      - name: Wait for backend health
        run: |
          for i in {1..60}; do
            if curl -fsS http://localhost:8080/health; then
              exit 0
            fi
            sleep 2
          done
          echo "Backend did not become healthy."
          echo "---- backend.log ----"
          cat backend.log || true
          exit 1
```

Expected: `curl` succeeds before Playwright starts.

## Task 4: Run Frontend E2E Gates

**Modify:**

```text
.github/workflows/frontend-ci.yml
```

- [ ] Run preflight:

```yaml
      - name: Run E2E preflight
        run: npm run check:e2e-preflight
```

- [ ] Install Playwright browsers:

```yaml
      - name: Install Playwright browsers
        run: npx playwright install --with-deps chromium
```

- [ ] Run release E2E:

```yaml
      - name: Run frontend E2E release gates
        run: npm run release:e2e
```

Do not manually start Next.js or `e2e/api-proxy.mjs`; Playwright already does
that through `playwright.config.ts`.

## Task 5: Upload Failure Artifacts

**Modify:**

```text
.github/workflows/frontend-ci.yml
```

- [ ] Add artifact upload step at the end of `release-e2e`:

```yaml
      - name: Upload E2E artifacts
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: frontend-cross-repo-e2e-artifacts
          path: |
            playwright-report/
            test-results/
            backend.log
          if-no-files-found: ignore
          retention-days: 7
```

Reason: failed E2E without logs is hard to debug. `backend.log` is especially
important for migration/startup/API failures.

## Task 6: Local Static Verification

**Run:**

```bash
git diff --check
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Expected:

```text
governance passes
typecheck passes
lint passes
unit/component tests pass
build succeeds
```

Do not run local full E2E unless a backend is already running locally and the
user asks for it. This phase's key proof is the manual GitHub Actions E2E run.

## Task 7: Commit

- [ ] Run GitNexus detect changes before commit:

```text
gitnexus_detect_changes({repo: "ControlHub", scope: "all"})
```

Expected:

```text
workflow-only change, no frontend symbols changed, risk none/low
```

- [ ] Commit:

```bash
git add .github/workflows/frontend-ci.yml
git commit -m "ci: add cross-repo frontend e2e gate"
```

Do not add `Co-Authored-By`.

## Task 8: Merge, Push, And Manual Run

Only after local verification passes:

```bash
cd /Users/fan/JsProjects/ControlHub
git merge --ff-only feat/phase-35-cross-repo-e2e-ci
git push origin main
```

Fast CI should run automatically. Then trigger manual E2E:

```bash
gh workflow run "Frontend CI" --ref main -f run_e2e=true
gh run list --workflow "Frontend CI" --branch main --limit 5
gh run watch RUN_ID --exit-status
```

If the manual E2E run fails:

```bash
gh run view RUN_ID --log-failed
```

Classify the failure as one of:

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

Do not make broad retries or suppress output. Fix the specific class.

## Final Report Requirements

Report:

```text
worktree / branch
commit hash
changed files
local verification results
fast CI run id / URL / result
manual E2E run id / URL / result
backend checkout method used
secret name configured or missing
artifact upload status
final git status
```

Scope confirmation:

```text
no product UI changes
no backend repo changes
no backend workflow changes
no OpenAPI changes
no SQL or migrations
no mocked backend
no push-triggered full E2E
no broad retries/output suppression
no skipped/deleted tests
no tag/release/deploy
no AI co-author
```
