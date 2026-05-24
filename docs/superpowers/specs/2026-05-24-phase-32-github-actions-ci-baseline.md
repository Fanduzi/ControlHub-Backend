# Phase 32 GitHub Actions CI Baseline Design

## Background

ControlHub now has local release gates:

- backend: `make release-readiness-gates`
- frontend: `npm run release:check`
- RC evidence: `docs/releases/candidates/2026-05-24-controlhub-rc-local.md`

The current baseline is good enough for a local RC, but it is still local-only.
Both repositories are private GitHub repositories. That changes cost and
security tradeoffs, but it does not prevent using GitHub Actions.

Phase 32 introduces a minimal CI baseline so future changes are checked before
merge and release evidence is not dependent only on local terminal history.

## Official GitHub Actions Constraints

These design choices are based on GitHub Actions docs:

- Workflow events include `push`, `pull_request`, and `workflow_dispatch`.
  `workflow_dispatch` enables manually triggered workflows from the API, CLI, or
  UI.
  Source: <https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch>
- Workflow artifacts can be uploaded with `actions/upload-artifact`, and
  `retention-days` can set a custom retention period up to repository,
  organization, or enterprise limits.
  Source: <https://docs.github.com/en/actions/tutorials/store-and-share-data>
- GitHub-hosted runner minutes, artifact storage, and cache storage count
  against the repository owner's allowance in private repositories.
  Source: <https://docs.github.com/en/billing/concepts/product-billing/github-actions>
- Secrets can be created at repository, environment, or organization level and
  referenced through the `secrets` context. Secrets are not available in all
  event contexts, and sensitive values should not be printed to logs.
  Source: <https://docs.github.com/en/actions/how-tos/write-workflows/choose-what-workflows-do/use-secrets>

## Goal

Add a two-layer GitHub Actions CI baseline for both private repositories:

```text
fast CI:
  run on push and pull_request
  no Docker-heavy gates
  catches typecheck/lint/unit/build/OpenAPI validation failures quickly

manual heavy CI:
  run via workflow_dispatch
  runs Docker-backed backend gates or full frontend E2E
  uploads diagnostic artifacts with short retention
```

## Non-Goals

- Do not deploy.
- Do not publish packages.
- Do not create tags or GitHub releases.
- Do not add automatic release jobs.
- Do not require CDP live smoke in CI.
- Do not run every heavy gate on every push by default.
- Do not add secrets unless a workflow actually needs them.
- Do not add broad retries or failure suppression.
- Do not weaken local release gates.

## Repository Split

Backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

Each repo should own its own workflow file. Do not create a cross-repository
workflow in Phase 32.

## Backend CI Design

Workflow file:

```text
.github/workflows/backend-ci.yml
```

Fast CI triggers:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
```

Fast CI command:

```bash
make release-local-gates
```

Manual heavy trigger:

```yaml
workflow_dispatch:
```

Manual heavy command:

```bash
make release-docker-gates
```

Backend artifact policy:

- Upload `.schemathesis-reports/` only if it exists.
- Use short retention, for example `retention-days: 7`.
- Do not upload logs that may contain secrets.

Docker policy:

- Docker-backed gates should be manual first.
- Do not make `make release-docker-gates` mandatory on every private-repo push
  until cost/runtime is observed.

## Frontend CI Design

Workflow file:

```text
.github/workflows/frontend-ci.yml
```

Fast CI triggers:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
```

Fast CI command:

```bash
npm run release:local
```

Manual heavy trigger:

```yaml
workflow_dispatch:
```

Manual heavy command:

```bash
npm run release:e2e
```

Frontend artifact policy:

- Upload Playwright reports and test artifacts only on failure or manual E2E
  runs.
- Use short retention, for example `retention-days: 7`.

Backend dependency policy:

- `npm run release:local` does not require the backend.
- `npm run release:e2e` requires a backend-compatible environment. Phase 32
  should either:
  - start the backend through the frontend E2E harness if already supported, or
  - keep full frontend E2E manual/local until a reliable CI backend service is
    designed.

Do not fake backend responses in release E2E.

## Secrets Policy

Phase 32 should not need secrets for fast CI.

If a workflow later needs secrets:

- use repository or environment secrets
- access values only through `${{ secrets.NAME }}`
- do not print secret values
- do not pass secrets on command lines when an environment variable works
- document every required secret in the workflow and checklist

## Cost Policy For Private Repositories

Because the repos are private, GitHub-hosted runner minutes and artifact storage
matter. Phase 32 should keep default push/PR jobs fast:

```text
backend push/PR:
  make release-local-gates

frontend push/PR:
  npm run release:local
```

Heavy gates should start as `workflow_dispatch`:

```text
backend manual:
  make release-docker-gates

frontend manual:
  npm run release:e2e
```

After observing duration and cost, a later phase may promote selected heavy
gates to scheduled or required branch-protection checks.

## Required Status Checks

Initial recommended required checks:

```text
backend-ci / release-local-gates
frontend-ci / release-local
```

Do not require manual heavy jobs as branch protection checks yet.

## Release Evidence Impact

RC evidence should distinguish:

```text
local gate result
GitHub Actions fast CI result
GitHub Actions manual heavy CI result
```

Until CI exists and has passed, local RC evidence remains valid but should say
that CI is not yet configured.

## Success Criteria

Phase 32 is complete when:

- backend workflow file exists and runs local backend gates on push/PR
- frontend workflow file exists and runs local frontend gates on push/PR
- manual heavy workflows are available or explicitly deferred with reason
- artifacts are uploaded only where useful, with short retention
- docs explain private-repo cost, secrets, and manual-heavy-gate policy
- no deployment/tag/release jobs are added

