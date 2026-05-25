# Phase 34 Schemathesis FK-Aware Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Investigate and, if practical, implement FK-aware Schemathesis data generation so backend heavy CI can move beyond the `schemathesis==4.15.2` pin without corrupting the OpenAPI contract.

**Architecture:** Start as an investigation phase in a dedicated backend worktree. Verify supported Schemathesis v4.19+ extension points, then either implement a small supported FK-aware runner/hook or document why the version pin remains.

**Tech Stack:** Go, Make, Testcontainers, Schemathesis v4.19+, OpenAPI, Python optional runner/hooks, GitHub Actions.

---

## Required Reading

```text
docs/superpowers/specs/2026-05-26-phase-34-schemathesis-fk-aware-generation.md
docs/superpowers/specs/2026-05-25-phase-33e-schemathesis-ci-version-policy.md
docs/releases/candidates/phase-33c-investigation-report.md
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
scripts/openapi-fuzz.sh
scripts/schemathesis.toml
.github/workflows/backend-ci.yml
internal/integration/openapi_fuzz_test.go
```

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-34-schemathesis-fk-aware-generation -b phase-34-schemathesis-fk-aware-generation main
cd .worktrees/backend-phase-34-schemathesis-fk-aware-generation
git status --short --branch
```

Expected: clean branch.

## Constraints

- Do not add OpenAPI FK enum values.
- Do not suppress warnings.
- Do not skip operations.
- Do not reduce checks/examples.
- Do not swallow Schemathesis exit codes.
- Do not remove the v4.15.2 CI pin unless v4.19+ replacement is proven locally.
- Do not change product behavior.
- Do not change SQL or migrations.
- Do not tag/release/deploy.
- No AI co-author.

---

## Task 1: Research Supported v4.19+ Extension Points

**Files:**

```text
no project file changes expected
```

- [ ] Create an isolated Python environment:

```bash
python3 -m venv .venv-schemathesis-419
source .venv-schemathesis-419/bin/activate
python -m pip install --upgrade pip
python -m pip install "schemathesis==4.19.0"
schemathesis --version || sth --version
```

- [ ] Inspect CLI support:

```bash
schemathesis run --help || sth run --help
schemathesis --help || sth --help
```

- [ ] Look specifically for supported mechanisms:

```text
hooks
data generation config
case mutation
custom strategy
Python API runner
operation filtering that does not skip execution
```

- [ ] Use official docs where possible. Record source links in notes or final
  report.

- [ ] Clean up before leaving the task:

```bash
deactivate || true
rm -rf .venv-schemathesis-419
```

Do not commit the venv.

## Task 2: Reproduce Current v4.19 Failure

**Files:**

```text
no project file changes expected
```

- [ ] In the isolated v4.19 environment, run:

```bash
make test-openapi-fuzz
```

- [ ] Capture:

```text
exit code
operation failure list
generated case count
warning list
whether POST /resources and POST /resources/{id}/relations still fail
```

- [ ] Remove generated reports after recording:

```bash
rm -rf .schemathesis-reports
```

## Task 3: Decide Implementation Path

Choose exactly one path:

### Path A: Supported Hook / Config

Use this only if v4.19+ officially supports constraining generated body fields.

Allowed files:

```text
scripts/schemathesis.toml
scripts/openapi-fuzz.sh
scripts/<small-hook-file-if-supported>
```

Requirements:

```text
FK-like fields use known seed IDs
operations still run
warnings still display
checks unchanged
exit code remains trusted
```

### Path B: Small Python Runner

Use only if the Python API is supported and a small runner can preserve the
existing checks.

Allowed files:

```text
scripts/openapi-fuzz.py
scripts/openapi-fuzz.sh
tests/scripts/openapi-fuzz-runner.test.* only if practical
```

Requirements:

```text
runner must fail on real checks
runner must not hide warnings
runner must keep 27 operations
runner must be maintainable
```

### Path C: Document Deferral

Use if supported FK-aware generation is unavailable or too complex.

Allowed files:

```text
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
docs/release-hardening-checklist.md
docs/superpowers/notes/2026-05-26-phase-34-schemathesis-fk-aware-generation.md
```

Requirements:

```text
state why v4.15.2 pin remains
state exact v4.19 limitation
state what would unblock upgrade later
```

Do not keep experimenting without selecting a path.

## Task 4: Verify Chosen Path

Always run:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

If Path A or B attempts v4.19 replacement, additionally verify with v4.19:

```bash
python3 -m venv .venv-schemathesis-419
source .venv-schemathesis-419/bin/activate
python -m pip install --upgrade pip
python -m pip install "schemathesis==4.19.0"
make test-openapi-fuzz
deactivate || true
rm -rf .venv-schemathesis-419 .schemathesis-reports
```

Expected for Path A/B success:

```text
v4.19+ exits 0
27/27 operations exercised
no reduced checks/examples
no skipped operations
```

Expected for Path C:

```text
current pinned gate remains green
deferral evidence is documented
```

## Task 5: GitNexus And Commit

Run:

```bash
git status --short --branch
git diff --check
```

Run GitNexus change detection:

```text
gitnexus_detect_changes({scope: "all"})
```

Commit message depends on path:

```text
test: add schemathesis fk-aware generation
```

or:

```text
docs: document schemathesis fk-aware generation deferral
```

No `Co-Authored-By`.

## Task 6: Merge / Push Policy

Do not push until local verification is complete.

If Path A/B removes the pin:

- merge
- push
- trigger backend manual heavy CI
- update RC evidence only after remote PASS

If Path C keeps the pin:

- merge docs-only result
- push is allowed only if user confirms
- do not trigger heavy CI unless workflow changed

## Final Report Requirements

Report:

```text
worktree / branch
chosen path: A, B, or C
Schemathesis version tested
v4.19 reproduction summary
implementation or deferral reason
files changed
verification matrix
whether CI pin remains
GitNexus summary
final git status
next recommended action
```

Scope confirmation:

```text
No OpenAPI FK enum
No warning suppression
No skipped/deleted operations
No reduced checks/examples
No wrapper exit-code swallowing
No product behavior change
No SQL or migrations
No tag/release/deploy
No AI co-author
```

