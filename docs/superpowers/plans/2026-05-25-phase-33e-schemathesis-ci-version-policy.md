# Phase 33E Schemathesis CI Version Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restore backend GitHub Actions manual heavy CI by pinning Schemathesis to the locally validated version, without suppressing warnings or polluting OpenAPI with runtime FK IDs.

**Architecture:** Make one small workflow change in the backend repository, update release evidence and checklist with the version policy, then push and rerun backend manual heavy CI.

**Tech Stack:** GitHub Actions, Go, Make, Testcontainers, Schemathesis, OpenAPI, Markdown evidence.

---

## Required Reading

```text
docs/superpowers/specs/2026-05-25-phase-33e-schemathesis-ci-version-policy.md
docs/releases/candidates/phase-33c-investigation-report.md
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
.github/workflows/backend-ci.yml
scripts/openapi-fuzz.sh
scripts/schemathesis.toml
```

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-33e-schemathesis-ci-version-policy -b phase-33e-schemathesis-ci-version-policy main
cd .worktrees/backend-phase-33e-schemathesis-ci-version-policy
git status --short --branch
```

Expected: clean branch.

## Constraints

- No OpenAPI FK enum for `environmentId`, `ownerId`, or `toResourceId`.
- No warning suppression.
- No operation skip/exclude.
- No reduced Schemathesis checks/examples.
- No wrapper exit-code swallowing.
- No product behavior change.
- No SQL or migrations.
- No tag/release/deploy.
- No AI co-author.

---

## Task 1: Pin Schemathesis In Backend CI

**Files:**

```text
.github/workflows/backend-ci.yml
```

- [ ] Inspect current workflow:

```bash
sed -n '1,220p' .github/workflows/backend-ci.yml
```

- [ ] Change the heavy CI install step from unpinned Schemathesis to:

```bash
python -m pip install --upgrade "schemathesis==4.15.2"
```

- [ ] Do not change:

```text
make release-docker-gates
permissions
artifact upload
workflow triggers
```

- [ ] Do not add retries or `continue-on-error`.

## Task 2: Update Evidence And Checklist

**Files:**

```text
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
docs/release-hardening-checklist.md
```

- [ ] Update RC evidence to record:

```text
Phase 33E decision:
  Backend heavy CI pins Schemathesis to 4.15.2.

Reason:
  v4.19 treats DB-backed validation_mismatch as operation failure / exit 1.
  The remaining mismatch is runtime referential integrity, not a 5xx contract bug.
  OpenAPI must not encode seed FK IDs as enum.

Deferred:
  Phase 34 should investigate FK-aware data generation for newer Schemathesis.
```

- [ ] Ensure evidence still states remote heavy CI is not PASS until rerun
  succeeds.

- [ ] Update release checklist to mention:

```text
Backend CI heavy gate uses pinned Schemathesis 4.15.2.
Do not upgrade without running Phase 34-style FK-aware generation investigation.
```

## Task 3: Local Verification

Run:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Expected:

```text
all PASS
make test-openapi-fuzz uses local/default Schemathesis and exits 0
no operation/check/example reduction
```

## Task 4: GitNexus And Commit

Run:

```bash
git status --short --branch
git diff --check
```

Run GitNexus change detection:

```text
gitnexus_detect_changes({scope: "all"})
```

Commit:

```bash
git add .github/workflows/backend-ci.yml \
        docs/releases/candidates/2026-05-24-controlhub-rc-local.md \
        docs/release-hardening-checklist.md
git commit -m "ci: pin schemathesis for backend heavy gate"
```

No `Co-Authored-By`.

## Task 5: Merge, Push, And Remote Heavy CI

If local verification passes:

```bash
cd /Users/fan/GolangProjects/ControlHub
git merge --ff-only phase-33e-schemathesis-ci-version-policy
git worktree remove .worktrees/backend-phase-33e-schemathesis-ci-version-policy
git branch -d phase-33e-schemathesis-ci-version-policy
git push origin main
```

Trigger remote heavy CI:

```bash
gh workflow run "Backend CI" -f run_docker_gates=true -r main
gh run list --limit 5
gh run watch
```

If workflow name differs:

```bash
gh workflow run backend-ci.yml -f run_docker_gates=true -r main
```

## Task 6: Evidence After Remote Result

If remote heavy CI passes, update:

```text
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
```

Record:

```text
run ID / URL
commit SHA
release-local-gates PASS
release-docker-gates PASS
integration PASS
OpenAPI fuzz PASS
Schemathesis version pinned to 4.15.2
no tag/release/deploy
```

Commit and push:

```bash
git add docs/releases/candidates/2026-05-24-controlhub-rc-local.md
git commit -m "docs: record backend heavy ci pass"
git push origin main
```

Wait for backend fast CI on evidence update and record URL/status.

If remote heavy CI fails, do not mark evidence PASS. Collect failed logs:

```bash
gh run view <run-id> --log-failed
```

## Final Report Requirements

Report:

```text
worktree / branch
commit hash(es)
workflow change
version pin rationale
local verification matrix
merge/push result
remote heavy CI run URL/status
evidence update commit if created
final git status
final ahead/behind status
Phase 34 deferred item
```

Scope confirmation:

```text
No OpenAPI FK enum
No warning suppression
No skipped/deleted fuzz operations
No reduced checks/examples
No wrapper exit-code swallowing
No product behavior change
No SQL or migrations
No tag/release/deploy
No AI co-author
```

