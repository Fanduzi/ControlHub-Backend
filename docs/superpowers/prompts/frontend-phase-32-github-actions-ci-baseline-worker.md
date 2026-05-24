# Frontend Phase 32 GitHub Actions CI Baseline Worker Prompt

You are implementing the frontend side of Phase 32 for ControlHub.

Frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repository is separate and must not be edited by this worker:

```text
/Users/fan/GolangProjects/ControlHub
```

## Objective

Add a minimal GitHub Actions CI baseline for the private frontend repo.

This phase is CI/release-governance only. Do not deploy, publish, tag, push, or
change product UI.

## Required Reading

In the backend/docs repo:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-24-phase-32-github-actions-ci-baseline.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-24-phase-32-github-actions-ci-baseline.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/phase-32-github-actions-ci-coordination.md
```

In the frontend repo:

```text
package.json
playwright.config.ts
scripts/check-e2e-preflight.mjs
```

Official GitHub Actions references:

```text
https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch
https://docs.github.com/en/actions/tutorials/store-and-share-data
https://docs.github.com/en/billing/concepts/product-billing/github-actions
https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets
```

## Worktree

Create a frontend worktree:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-32-github-actions-ci-baseline -b feat/phase-32-github-actions-ci-baseline main
cd .worktrees/frontend-phase-32-github-actions-ci-baseline
git status --short --branch
```

Expected: clean branch `feat/phase-32-github-actions-ci-baseline`.

Do not edit the main worktree directly.

## Scope

Allowed:

```text
.github/workflows/frontend-ci.yml
package.json only if an existing script needs a CI-safe alias
docs only if frontend repo has relevant docs
```

Not allowed:

```text
product UI code
backend repo edits
release/tag/deploy jobs
push to remote
mock backend for release E2E
secrets unless strictly required
```

## Required Workflow Behavior

Create:

```text
.github/workflows/frontend-ci.yml
```

Requirements:

- `push` to `main` runs fast frontend CI.
- `pull_request` to `main` runs fast frontend CI.
- `workflow_dispatch` supports a manual E2E input.
- Fast frontend CI runs:

```bash
npm run release:local
```

- Manual frontend E2E must not be added as a fake-green job.
- If `npm run release:e2e` can run in GitHub Actions with a reliable backend
  bootstrap, implement it.
- If backend bootstrap is not reliable yet, create only the fast CI workflow and
  document manual E2E as deferred in the final report.

Recommended fast workflow shape:

```yaml
name: Frontend CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  workflow_dispatch:
    inputs:
      run_e2e:
        description: "Run full frontend E2E gates"
        required: false
        default: "false"
        type: choice
        options:
          - "false"
          - "true"

permissions:
  contents: read

jobs:
  release-local:
    name: release-local
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-node@v6
        with:
          node-version-file: package.json
          cache: npm
      - name: Install dependencies
        run: npm ci
      - name: Run frontend local release gates
        run: npm run release:local
```

Optional manual E2E job only if backend bootstrap is real:

```yaml
  release-e2e:
    name: release-e2e
    runs-on: ubuntu-latest
    if: github.event_name == 'workflow_dispatch' && inputs.run_e2e == 'true'
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-node@v6
        with:
          node-version-file: package.json
          cache: npm
      - name: Install dependencies
        run: npm ci
      - name: Install Playwright browsers
        run: npx playwright install --with-deps chromium
      - name: Run frontend E2E release gates
        run: npm run release:e2e
      - name: Upload Playwright report
        if: always() && hashFiles('playwright-report/**') != ''
        uses: actions/upload-artifact@v4
        with:
          name: playwright-report
          path: playwright-report/
          retention-days: 7
```

Before committing, verify whether `actions/checkout@v6` and `actions/setup-node@v6`
are valid current versions in this environment. If not, use the current stable
major version and state the reason in the final report.

## Backend Dependency Check

Inspect:

```bash
cat package.json
sed -n '1,220p' playwright.config.ts
```

Determine whether `npm run release:e2e` starts or depends on:

```text
frontend dev server
E2E API proxy
backend server
seed data
```

If backend server startup is not encoded in the frontend repo, do not add manual
E2E CI yet. Report:

```text
manual release:e2e workflow deferred because backend bootstrap is cross-repo and not yet encoded for Actions
```

## Verification

Run:

```bash
git diff --check
npm run release:local
```

If manual E2E workflow was added, also run locally:

```bash
npm run release:e2e
```

If YAML checker already exists, run it. Do not add a new dependency just for
YAML lint in this phase.

## Commit

Stage only directly related files:

```bash
git add .github/workflows/frontend-ci.yml
```

Commit:

```bash
git commit -m "ci: add frontend release gate workflow"
```

No `Co-Authored-By`.

## Final Report

Report:

```text
worktree
branch
commit hash
workflow file path
fast CI triggers
manual E2E status: implemented or deferred
backend dependency finding
artifact policy
actions versions used
local verification results
final git status
```

Scope confirmation:

```text
No deployment
No release
No tag
No push
No product UI changes
No backend repo changes
No broad retries/output suppression
No skipped/deleted tests
No AI co-author
```

