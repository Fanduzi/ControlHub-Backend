# Backend Phase 32 GitHub Actions CI Baseline Worker Prompt

You are implementing the backend side of Phase 32 for ControlHub.

Backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repository is separate and must not be edited by this worker:

```text
/Users/fan/JsProjects/ControlHub
```

## Objective

Add a minimal GitHub Actions CI baseline for the private backend repo.

This phase is CI/release-governance only. Do not deploy, publish, tag, push, or
change product behavior.

## Required Reading

```text
docs/superpowers/specs/2026-05-24-phase-32-github-actions-ci-baseline.md
docs/superpowers/plans/2026-05-24-phase-32-github-actions-ci-baseline.md
docs/release-hardening-checklist.md
docs/quality-baseline.md
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
```

Official GitHub Actions references:

```text
https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch
https://docs.github.com/en/actions/tutorials/store-and-share-data
https://docs.github.com/en/billing/concepts/product-billing/github-actions
https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets
```

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-32-github-actions-ci-baseline -b phase-32-github-actions-ci-baseline main
cd .worktrees/backend-phase-32-github-actions-ci-baseline
git status --short --branch
```

Expected: clean branch `phase-32-github-actions-ci-baseline`.

Do not edit the main worktree directly.

## Scope

Allowed:

```text
.github/workflows/backend-ci.yml
docs/release-hardening-checklist.md
docs/quality-baseline.md
docs/releases/candidates/2026-05-24-controlhub-rc-local.md only if evidence wording needs CI status
```

Not allowed:

```text
backend API code
SQL or migrations
product behavior
release/tag/deploy jobs
push to remote
secrets unless strictly required
```

## Required Workflow Behavior

Create:

```text
.github/workflows/backend-ci.yml
```

Requirements:

- `push` to `main` runs fast backend CI.
- `pull_request` to `main` runs fast backend CI.
- `workflow_dispatch` supports a manual heavy gate input.
- Fast backend CI runs:

```bash
make release-local-gates
```

- Manual heavy backend CI runs only when explicitly requested:

```bash
make release-docker-gates
```

- Manual heavy job installs Schemathesis before running Docker gates.
- Upload `.schemathesis-reports/` only if it exists.
- Artifact retention should be short, for example 7 days.
- Workflow permissions should be minimal:

```yaml
permissions:
  contents: read
```

Recommended workflow shape:

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

Before committing, verify whether `actions/checkout@v6` and `actions/setup-go@v6`
are valid current versions in this environment. If not, use the current stable
major version and state the reason in the final report.

## Docs Updates

Update `docs/release-hardening-checklist.md`:

- Add backend GitHub Actions fast CI:

```text
.github/workflows/backend-ci.yml
push/pull_request -> make release-local-gates
```

- Add backend manual heavy CI:

```text
workflow_dispatch with run_docker_gates=true -> make release-docker-gates
uploads .schemathesis-reports/ for 7 days when present
```

Update `docs/quality-baseline.md`:

- Add backend CI gate row.
- Mention private repository minutes/storage cost.
- Mention manual heavy gate policy.

Only update RC evidence if needed to state GitHub Actions CI is configured but
not yet run until pushed.

## Verification

Run:

```bash
git diff --check
make release-local-gates
```

If a YAML checker already exists in the repo, run it. Do not add a new
dependency just for YAML lint in this phase.

Do not run `make release-docker-gates` unless you choose to verify the manual
heavy command locally and Docker is available.

Before commit:

```bash
git status --short --branch
gitnexus_detect_changes({scope: "all"})
```

If GitNexus reports HIGH or CRITICAL risk, stop and report.

## Commit

Stage only directly related files:

```bash
git add .github/workflows/backend-ci.yml docs/release-hardening-checklist.md docs/quality-baseline.md
```

Commit:

```bash
git commit -m "ci: add backend release gate workflow"
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
manual heavy CI trigger
artifact policy
actions versions used
local verification results
GitNexus detect_changes summary
docs updated
final git status
```

Scope confirmation:

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

