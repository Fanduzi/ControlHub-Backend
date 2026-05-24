# Phase 32 GitHub Actions CI Coordination Prompt

Phase 32 spans two private GitHub repositories. Treat them as separate delivery
units.

Backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Rule

Do not implement backend and frontend CI in the same git branch or worktree.

Use:

```text
backend branch:
  phase-32-github-actions-ci-baseline

frontend branch:
  feat/phase-32-github-actions-ci-baseline
```

## Recommended Order

1. Backend worker first.
2. Frontend worker second.
3. Backend docs sync after frontend worker reports whether manual E2E is
   implemented or deferred.

Reason:

- Backend fast CI is self-contained: `make release-local-gates`.
- Frontend fast CI is self-contained: `npm run release:local`.
- Frontend manual E2E may need backend bootstrap. Do not fake it.

## Shared Policy

Both workers must preserve:

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

## Expected Outputs

Backend worker:

```text
.github/workflows/backend-ci.yml
docs/release-hardening-checklist.md
docs/quality-baseline.md
```

Frontend worker:

```text
.github/workflows/frontend-ci.yml
```

Backend docs sync, if needed:

```text
docs/release-hardening-checklist.md
docs/quality-baseline.md
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
```

## Merge Order

1. Review and merge backend CI branch.
2. Review and merge frontend CI branch.
3. If backend docs need final sync after frontend result, use a small backend
   docs-only follow-up branch.

Do not push branches or tags unless explicitly authorized by the user.

