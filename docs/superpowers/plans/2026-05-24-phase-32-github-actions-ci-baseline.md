# Phase 32 GitHub Actions CI Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a minimal GitHub Actions CI baseline for the private frontend and backend repositories without adding deployment, release, tag, or publish automation.

**Architecture:** Implement separate workflows in each repository. Fast push/PR workflows run local release gates. Heavy Docker/E2E workflows are manual first through `workflow_dispatch` to control private-repo minutes and artifact storage. Backend/docs owns the canonical release-checklist updates.

**Tech Stack:** GitHub Actions, Go, Make, Node.js/npm, Next.js, Playwright, Testcontainers, Schemathesis, workflow artifacts.

---

## Required Reading

```text
docs/superpowers/specs/2026-05-24-phase-32-github-actions-ci-baseline.md
docs/superpowers/prompts/phase-32-github-actions-ci-coordination.md
docs/superpowers/prompts/backend-phase-32-github-actions-ci-baseline-worker.md
docs/superpowers/prompts/frontend-phase-32-github-actions-ci-baseline-worker.md
docs/release-hardening-checklist.md
docs/quality-baseline.md
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
```

Official references:

```text
https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch
https://docs.github.com/en/actions/tutorials/store-and-share-data
https://docs.github.com/en/billing/concepts/product-billing/github-actions
https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets
```

## Worktrees

Backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-32-github-actions-ci-baseline -b phase-32-github-actions-ci-baseline main
cd .worktrees/backend-phase-32-github-actions-ci-baseline
git status --short --branch
```

Frontend worktree:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-32-github-actions-ci-baseline -b feat/phase-32-github-actions-ci-baseline main
cd .worktrees/frontend-phase-32-github-actions-ci-baseline
git status --short --branch
```

Do not edit either main worktree directly.

## Constraints

- No deployment.
- No release.
- No tag.
- No push.
- No product UI changes.
- No backend API behavior changes.
- No SQL or migrations.
- No broad retries or output suppression.
- No skipped/deleted tests.
- No AI co-author.

---

## Task 1: Backend Fast CI Workflow

**Backend files:**

```text
.github/workflows/backend-ci.yml
docs/release-hardening-checklist.md
docs/quality-baseline.md
```

- [ ] Create `.github/workflows/backend-ci.yml`.

Required behavior:

```yaml
name: Backend CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  workflow_dispatch:
    inputs:
      run_docker_gates:
        description: "Run Docker-backed integration and OpenAPI fuzz gates"
        required: false
        default: "false"
        type: choice
        options:
          - "false"
          - "true"

permissions:
  contents: read

jobs:
  release-local-gates:
    name: release-local-gates
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true
      - name: Run backend local release gates
        run: make release-local-gates

  release-docker-gates:
    name: release-docker-gates
    runs-on: ubuntu-latest
    if: github.event_name == 'workflow_dispatch' && inputs.run_docker_gates == 'true'
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true
      - name: Install Schemathesis
        run: python -m pip install --upgrade schemathesis
      - name: Run backend Docker release gates
        run: make release-docker-gates
      - name: Upload Schemathesis reports
        if: always() && hashFiles('.schemathesis-reports/**') != ''
        uses: actions/upload-artifact@v4
        with:
          name: schemathesis-reports
          path: .schemathesis-reports/
          retention-days: 7
```

- [ ] Adjust action versions only if the repo already uses a different current
  standard. Do not pin arbitrary old versions.

- [ ] Run YAML sanity checks if available:

```bash
git diff --check
```

- [ ] Run local backend gates to confirm the workflow command still works:

```bash
make release-local-gates
```

- [ ] Update `docs/release-hardening-checklist.md` with:

```text
Backend GitHub Actions fast CI:
  .github/workflows/backend-ci.yml
  push/pull_request -> make release-local-gates

Backend GitHub Actions manual heavy CI:
  workflow_dispatch with run_docker_gates=true -> make release-docker-gates
  uploads .schemathesis-reports/ for 7 days if present
```

- [ ] Update `docs/quality-baseline.md` with a backend CI row.

- [ ] Commit backend changes:

```bash
git add .github/workflows/backend-ci.yml docs/release-hardening-checklist.md docs/quality-baseline.md
git commit -m "ci: add backend release gate workflow"
```

---

## Task 2: Frontend Fast CI Workflow

**Frontend files:**

```text
.github/workflows/frontend-ci.yml
```

- [ ] Create `.github/workflows/frontend-ci.yml`.

Required behavior:

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

- [ ] Before finalizing, inspect `package.json` and Playwright config. If
  `release:e2e` requires a backend that CI does not start, do not pretend it is
  ready. Either:

```text
Option A:
  keep release-e2e job disabled/deferred in docs only

Option B:
  add exact backend startup steps only if current repo already has a reliable
  backend bootstrap for CI
```

No mocked backend for release E2E.

- [ ] Run local frontend gate:

```bash
npm run release:local
```

- [ ] Commit frontend changes:

```bash
git add .github/workflows/frontend-ci.yml
git commit -m "ci: add frontend release gate workflow"
```

---

## Task 3: Backend Docs Sync With Frontend Result

After frontend worker reports the exact workflow shape, update backend docs if
needed:

```text
docs/release-hardening-checklist.md
docs/quality-baseline.md
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
```

Record:

```text
frontend fast CI workflow path
whether manual E2E workflow is implemented or deferred
artifact retention policy
private-repo minutes/storage policy
```

If the docs were already accurate, do not make a noisy commit.

Suggested commit:

```bash
git commit -m "docs: document github actions ci baseline"
```

---

## Task 4: Verification

Backend:

```bash
git diff --check
make release-local-gates
```

Frontend:

```bash
git diff --check
npm run release:local
```

If YAML lint tooling exists in either repo, run it. Do not add a new dependency
only for YAML lint in this phase.

Manual GitHub verification after push is out of scope unless the user
explicitly authorizes push.

---

## Task 5: Final Report

Backend report:

```text
worktree
branch
commit hash(es)
workflow file path
fast CI triggers
manual heavy CI trigger
artifact policy
local verification results
docs updated
final git status
```

Frontend report:

```text
worktree
branch
commit hash
workflow file path
fast CI triggers
manual E2E status: implemented or deferred
artifact policy
local verification results
final git status
```

Both reports must include scope confirmation:

```text
No deployment
No release
No tag
No push
No product UI changes
No backend API behavior changes
No SQL or migrations
No broad retries/output suppression
No skipped/deleted tests
No AI co-author
```
