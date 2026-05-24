# Phase 31 OpenAPI Fuzz Warning Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce or eliminate the OpenAPI fuzz warnings preserved in Phase 30 while keeping backend validation and API behavior intact.

**Architecture:** Work in a dedicated backend worktree. First reproduce and capture the exact warnings, then apply the smallest contract-quality fix: either provide valid Schemathesis data for seeded path IDs or tighten OpenAPI request schemas to match existing validation. Verify with OpenAPI validation, integration tests, and Schemathesis.

**Tech Stack:** Go, chi, MySQL/Testcontainers, OpenAPI 3.1 YAML, Schemathesis, Make, shell harness.

---

## Required Reading

```text
docs/superpowers/specs/2026-05-24-phase-31-openapi-fuzz-warning-cleanup.md
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
docs/superpowers/prompts/backend-phase-11-7-openapi-schema-tightening-worker.md
internal/integration/openapi_fuzz_test.go
scripts/openapi-fuzz.sh
internal/openapi/openapi.yaml
```

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-31-openapi-fuzz-warning-cleanup -b phase-31-openapi-fuzz-warning-cleanup main
cd .worktrees/backend-phase-31-openapi-fuzz-warning-cleanup
git status --short --branch
```

Expected:

```text
clean branch phase-31-openapi-fuzz-warning-cleanup
```

Do not edit the main worktree directly.

## Constraints

- No product UI changes.
- No backend behavior changes unless a focused test proves the current behavior
  is a contract bug.
- Do not relax backend validation.
- Do not skip warned operations.
- Do not suppress Schemathesis warnings globally.
- Do not reduce checks or examples to hide warnings.
- No SQL or migrations.
- No publish, deploy, tag, or push.
- No AI co-author.

---

## Task 1: Capture Current Fuzz Warnings

**Files:**

```text
no code changes expected
```

- [ ] Run the fuzz gate:

```bash
make test-openapi-fuzz
```

Expected baseline:

```text
PASS
960 generated, 960 passed
warnings mention:
  PATCH /resources/{id} missing valid test data / repeated 404
  schema validation mismatch for PATCH /resources/{id}, POST /auth/login, POST /resources
```

- [ ] Save the warning text in your notes for before/after comparison.

Do not commit generated `.schemathesis-reports/` artifacts.

---

## Task 2: Inspect Actual Validation Sources

**Files to inspect:**

```text
internal/openapi/openapi.yaml
internal/api/auth_handler.go
internal/api/resource_handler.go
internal/model/resource.go
internal/model/resource_write.go
internal/service/resource_service.go
internal/integration/openapi_fuzz_test.go
scripts/openapi-fuzz.sh
```

- [ ] Find request schema names for:

```text
POST /auth/login
POST /resources
PATCH /resources/{id}
```

Use:

```bash
rg -n "post:|patch:|/auth/login|/resources/\\{id\\}|Login|Resource" internal/openapi/openapi.yaml internal/api internal/model internal/service
```

- [ ] For every function, method, or exported type you intend to modify, run GitNexus impact analysis first, per repository rules.

Examples:

```text
gitnexus_impact({target: "UpdateResourceRequest", direction: "upstream"})
gitnexus_impact({target: "handlePatchResource", direction: "upstream"})
```

If impact is HIGH or CRITICAL, stop and report before editing.

- [ ] Classify each warning:

```text
data problem:
  generated path ID does not exist

schema problem:
  OpenAPI allows values or shapes current validation rejects

runtime bug:
  backend behavior violates declared contract
```

Do not start editing until this classification is clear.

---

## Task 3: Fix `PATCH /resources/{id}` Valid ID Coverage

**Likely files:**

```text
internal/openapi/openapi.yaml
scripts/openapi-fuzz.sh
internal/integration/openapi_fuzz_test.go
```

Preferred path:

- [ ] Check whether the seeded integration database has stable resource IDs.

Use existing integration tests and seed files:

```bash
rg -n "INSERT INTO resources|analytics-ch-cluster|resource_id|id" internal db migrations
```

- [ ] If a stable seed ID exists, add an OpenAPI example for the path parameter:

```yaml
parameters:
  - name: id
    in: path
    required: true
    schema:
      type: integer
      minimum: 1
    example: 14
```

Use the actual OpenAPI structure already present in `internal/openapi/openapi.yaml`.

- [ ] If examples do not influence Schemathesis path generation enough, add the smallest local harness support. Acceptable shape:

```text
scripts/openapi-fuzz.sh passes a Schemathesis config or hook file
the hook supplies known path parameter values only for /resources/{id}
```

Do not add a broad mocking layer.

- [ ] Add a focused test only if there is a testable helper or config file. Do not create brittle tests around Schemathesis internals.

---

## Task 4: Tighten `POST /auth/login` Schema

**Likely files:**

```text
internal/openapi/openapi.yaml
internal/api/auth_handler_test.go
```

- [ ] Inspect current login validation behavior.

Use:

```bash
rg -n "Login|email|password|auth" internal/api internal/model internal/service
```

- [ ] Tighten schema only to match existing behavior:

```yaml
email:
  type: string
  minLength: 1
password:
  type: string
  minLength: 1
required:
  - email
  - password
```

Add `format: email` only if current validation actually requires email-like
input. Do not add it just because the field is named email.

- [ ] If existing tests already cover missing/malformed fields, do not duplicate them. If no coverage exists, add one focused handler test.

---

## Task 5: Tighten `POST /resources` Schema

**Likely files:**

```text
internal/openapi/openapi.yaml
internal/api/resource_handler_test.go
```

- [ ] Inspect current create-resource request validation.

Use:

```bash
rg -n "CreateResource|resourceType|lifecycleStatus|healthStatus|source|labels" internal/api internal/model internal/service internal/openapi/openapi.yaml
```

- [ ] Tighten schema to match existing behavior:

```text
required fields match backend required fields
enum values match backend accepted values
string minLength constraints match backend validation
labels shape matches backend behavior
```

- [ ] Do not invent new enum values.

- [ ] If backend validation rejects unknown/immutable fields, document it in schema only if OpenAPI supports it without breaking existing accepted requests.

---

## Task 6: Tighten `PATCH /resources/{id}` Schema

**Likely files:**

```text
internal/openapi/openapi.yaml
internal/api/resource_handler_test.go
```

- [ ] Inspect current patch request validation.

Use:

```bash
rg -n "PatchResource|UpdateResource|PatchMutable|immutable|labels|healthStatus|lifecycleStatus" internal/api internal/model internal/service internal/openapi/openapi.yaml
```

- [ ] Tighten schema to match existing mutable fields:

```text
only mutable fields are described
mutable enum fields use enum lists
string fields have minLength where enforced
labels object shape matches backend behavior
path id has integer/minimum/example or harness data support
```

- [ ] Do not change behavior around immutable fields unless existing behavior is a real bug and a focused regression test is added first.

---

## Task 7: Verify Contract Gates

Run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Expected:

```text
all pass
OpenAPI fuzz warnings reduced or eliminated
```

If `make test-openapi-fuzz` still prints warnings:

- [ ] Copy the exact remaining warning list.
- [ ] Explain whether each remaining warning is:

```text
accepted
follow-up
blocking
```

Do not claim cleanup success if warning text is unchanged from Phase 30.

---

## Task 8: Update Evidence Docs If Warning Status Changes

**Files:**

```text
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
docs/release-hardening-checklist.md
```

Only update docs if Phase 31 materially changes the warning status.

Examples:

```text
If warnings are eliminated:
  add a short follow-up note that Phase 31 resolved the Phase 30 OpenAPI fuzz warnings.

If warnings are reduced but remain:
  update classification with exact remaining warnings.
```

Do not rewrite the full RC evidence file unless needed.

---

## Task 9: Pre-Commit Checks

Run:

```bash
git status --short --branch
git diff --check
```

Run GitNexus change detection before commit:

```text
gitnexus_detect_changes({scope: "all"})
```

Review affected symbols and processes. If risk is HIGH or CRITICAL, stop and
report before committing.

Do not stage generated reports:

```text
.schemathesis-reports/
```

---

## Task 10: Commit

Stage only directly related files.

Suggested commit messages:

```text
test: improve openapi fuzz input coverage
docs: document openapi fuzz warning cleanup
```

Use one commit if the change is small; split into code/test and docs commits if
both are non-trivial.

No `Co-Authored-By`.

---

## Final Report Requirements

Report:

```text
worktree
branch
commit hash(es)
files changed
warnings before
warnings after
schema constraints changed
fuzz input/data support changed
tests added or updated
verification matrix
GitNexus impact/detect_changes summary
final git status
```

Scope confirmation:

```text
No product UI changes
No backend behavior relaxation
No backend API behavior changes unless explicitly tested and reported
No SQL or migrations
No topology behavior changes
No skipped/deleted fuzz operations
No reduced Schemathesis checks/examples
No publish/deploy/tag/push
No broad warning suppression
No AI co-author
```

