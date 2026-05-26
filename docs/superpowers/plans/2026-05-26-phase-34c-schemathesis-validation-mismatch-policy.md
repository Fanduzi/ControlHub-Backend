# Phase 34C Schemathesis Validation Mismatch Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decide how ControlHub should handle Schemathesis v4.19+ `validation_mismatch` without weakening backend heavy CI.

**Architecture:** Work in a dedicated backend worktree. Reproduce v4.19 behavior, evaluate configuration/runner/contract options, then commit an evidence note and, only if justified, a minimal implementation plan for the selected follow-up.

**Tech Stack:** Go, Make, Testcontainers, Schemathesis v4.19+, OpenAPI 3.1, Python optional runner, GitHub Actions evidence.

---

## Required Reading

```text
docs/superpowers/specs/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md
docs/superpowers/notes/2026-05-26-phase-34b-schemathesis-hook-feasibility.md
docs/superpowers/specs/2026-05-25-phase-33e-schemathesis-ci-version-policy.md
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
git worktree add .worktrees/backend-phase-34c-schemathesis-validation-mismatch-policy -b phase-34c-schemathesis-validation-mismatch-policy main
cd .worktrees/backend-phase-34c-schemathesis-validation-mismatch-policy
git status --short --branch
```

Expected: clean branch.

## Constraints

- Do not remove the `schemathesis==4.15.2` CI pin during investigation.
- Do not add OpenAPI FK enums.
- Do not suppress all warnings.
- Do not skip operations.
- Do not reduce checks or examples.
- Do not swallow exit codes in `scripts/openapi-fuzz.sh`.
- Do not change product validation behavior.
- Do not change SQL or migrations.
- Do not tag, release, or deploy.
- No AI co-author.

---

## Task 1: Reproduce Current v4.19 Behavior

**Files:** no committed file changes.

- [ ] Create an isolated Schemathesis v4.19 environment:

```bash
python3 -m venv .venv-schemathesis-419
source .venv-schemathesis-419/bin/activate
python -m pip install --upgrade pip
python -m pip install "schemathesis==4.19.0"
schemathesis --version || sth --version
```

- [ ] Run the backend fuzz gate with v4.19:

```bash
make test-openapi-fuzz
```

- [ ] Record:

```text
exit code
operations passed / failed
generated cases passed / total
failed operation names
whether any JUnit/check failures exist
validation_mismatch text
```

- [ ] Remove generated reports after recording evidence:

```bash
rm -rf .schemathesis-reports
```

## Task 2: Check v4.19 Configuration Surface

**Files:** no committed file changes unless a small trial config is needed and later removed.

- [ ] Inspect CLI and config support:

```bash
schemathesis run --help || sth run --help
schemathesis --help || sth --help
```

- [ ] Check official docs or installed package help for controls related to:

```text
validation_mismatch
warnings
fail-on
checks
phases
positive/negative generation
```

- [ ] Record whether any supported option can make validation_mismatch non-blocking without hiding hard failures.

- [ ] If trying a temporary config, keep it uncommitted and remove it before final status.

## Task 3: Evaluate Policy Options

**Files:** notes only unless user approves implementation.

Classify each option from the spec:

```text
Option A: keep 4.15.2 pin
Option B: custom Python runner
Option C: contract / test data reshaping
```

For each option, record:

```text
implementation complexity
risk of hiding real 5xx/schema failures
risk of distorting public OpenAPI contract
whether remote heavy CI would be reliable
maintenance burden
```

Decision rules:

```text
Choose A if v4.19 cannot safely separate hard failures from expected business-validation rejections.
Choose B only if a small runner can preserve all checks and produce trustworthy exit codes.
Choose C only if schema/test-data changes reflect true API behavior, not seed-specific fiction.
```

## Task 4: Produce Evidence Note

**Create:**

```text
docs/superpowers/notes/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md
```

The note must include:

```text
worktree / branch
Schemathesis versions tested
commands run
exit codes
operation pass/fail counts
case counts
failed operation names
JUnit/check failure status
option comparison
chosen policy
CI pin status
next recommended phase
scope confirmation
```

Required conclusion format:

```text
Decision: Option A / Option B / Option C
Reason: summarize reproduction evidence and option comparison
CI pin: retained / candidate for removal after follow-up
```

## Task 5: Verification

For docs-only outcome, run:

```bash
git diff --check
python3 - <<'PY'
from pathlib import Path

path = Path("docs/superpowers/notes/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md")
markers = ["TO" + "DO", "TB" + "D", "PLACE" + "HOLDER"]
text = path.read_text(encoding="utf-8")
for marker in markers:
    if marker in text:
        raise SystemExit(f"unresolved marker found: {marker}")
PY
```

Expected:

```text
git diff --check has no output
placeholder scan has no unresolved placeholders
```

If any product code, OpenAPI, workflow, or scripts changed, also run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

## Task 6: GitNexus And Commit

- [ ] Run GitNexus detect changes before commit:

```text
gitnexus_detect_changes({repo: "ControlHub", scope: "all"})
```

Expected for docs-only:

```text
no Go symbols changed, risk none/low
```

- [ ] Commit:

```bash
git add docs/superpowers/notes/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md
git commit -m "docs: record schemathesis validation mismatch policy"
```

Do not add `Co-Authored-By`.

## Final Report Requirements

Report:

```text
worktree / branch
commit hash
files changed
v4.19 reproduction result
policy option chosen
CI pin status
verification commands and results
GitNexus detect_changes summary
final git status
```

Scope confirmation:

```text
no product code changes unless explicitly approved
no OpenAPI FK enum
no warning suppression
no skipped/deleted operations
no reduced checks/examples
no exit-code swallowing
no tag/release/deploy
no push unless explicitly approved
no AI co-author
```
