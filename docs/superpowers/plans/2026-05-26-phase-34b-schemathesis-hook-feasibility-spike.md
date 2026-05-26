# Phase 34B Schemathesis Hook Feasibility Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Verify whether Schemathesis v4.19 hooks can mutate FK-like path/body fields during the fuzzing phase, using evidence rather than assumptions.

**Architecture:** Run an isolated investigation in a backend worktree. Create temporary hook modules, execute Schemathesis v4.19 with explicit `SCHEMATHESIS_HOOKS`, record hook load/fire behavior, then clean all temporary files and commit only a notes document.

**Tech Stack:** Schemathesis v4.19.0, Python hooks, Hypothesis strategies, Go/Testcontainers backend fuzz harness, Markdown notes.

---

## Required Reading

```text
docs/superpowers/specs/2026-05-26-phase-34b-schemathesis-hook-feasibility-spike.md
docs/superpowers/specs/2026-05-26-phase-34-schemathesis-fk-aware-generation.md
docs/releases/candidates/phase-33c-investigation-report.md
scripts/openapi-fuzz.sh
scripts/schemathesis.toml
internal/integration/openapi_fuzz_test.go
```

Official docs to consult:

```text
Schemathesis hooks reference
Schemathesis extending guide
Schemathesis Python API reference
```

Use official docs or CLI help for hook names. Do not rely on memory.

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-34b-schemathesis-hook-feasibility -b phase-34b-schemathesis-hook-feasibility main
cd .worktrees/backend-phase-34b-schemathesis-hook-feasibility
git status --short --branch
```

Expected: clean branch.

## Constraints

- Do not modify product Go code.
- Do not modify OpenAPI schema.
- Do not modify `.github/workflows/backend-ci.yml`.
- Do not remove the Schemathesis 4.15.2 CI pin.
- Do not add OpenAPI FK enums.
- Do not suppress warnings.
- Do not skip operations.
- Do not reduce checks/examples.
- Do not commit experimental hook modules under `scripts/`.
- Commit only documentation unless the user explicitly approves implementation.

---

## Task 1: Set Up v4.19 Environment

**Files:**

```text
temporary venv only
```

- [ ] Create venv:

```bash
python3 -m venv .venv-schemathesis-419
source .venv-schemathesis-419/bin/activate
python -m pip install --upgrade pip
python -m pip install "schemathesis==4.19.0"
schemathesis --version || sth --version
```

- [ ] Record exact version.

- [ ] Inspect CLI help:

```bash
schemathesis run --help || sth run --help
schemathesis --help || sth --help
```

- [ ] Record whether hooks are documented in CLI help or only docs.

## Task 2: Baseline v4.19 Failure

- [ ] Run:

```bash
make test-openapi-fuzz
```

- [ ] Record:

```text
exit code
case count
failed operation list
warnings
whether POST /resources and POST /resources/{id}/relations fail
```

- [ ] Remove reports after recording key evidence:

```bash
rm -rf .schemathesis-reports
```

## Task 3: Create Temporary Hook Module

**Temporary file:**

```text
tmp_schemathesis_hooks.py
```

Do not commit this file.

- [ ] Create hook file with visible debug markers.

Minimum structure:

```python
import json
import os
import schemathesis

LOG = os.environ.get("CONTROLHUB_SCHEMATHESIS_HOOK_LOG", "hook-debug.log")

def record(event, ctx=None, payload=None):
    with open(LOG, "a", encoding="utf-8") as handle:
        handle.write(json.dumps({
            "event": event,
            "operation": getattr(getattr(ctx, "operation", None), "operation_id", None),
            "payload": payload,
        }, ensure_ascii=False, default=str) + "\n")

@schemathesis.hook
def before_call(ctx, case, kwargs):
    body = getattr(case, "body", None)
    record("before_call", ctx, {"body": body})
    if getattr(ctx.operation, "operation_id", None) == "createResource":
        if isinstance(case.body, dict):
            case.body["environmentId"] = 1
            case.body["ownerId"] = 1
            record("before_call_mutated", ctx, {"body": case.body})
    if getattr(ctx.operation, "operation_id", None) == "createResourceRelation":
        if isinstance(case.body, dict):
            case.body["toResourceId"] = 2
            record("before_call_mutated", ctx, {"body": case.body})

@schemathesis.hook
def map_path_parameters(ctx, path_parameters):
    record("map_path_parameters", ctx, {"path_parameters": path_parameters})
    if getattr(ctx.operation, "operation_id", None) in {"createResourceRelation", "patchResource"}:
        params = dict(path_parameters or {})
        params["id"] = 1
        record("map_path_parameters_mutated", ctx, {"path_parameters": params})
        return params
    return path_parameters

@schemathesis.hook
def map_body(ctx, body):
    record("map_body", ctx, {"body": body})
    return body
```

If docs show different signatures for v4.19, use docs-correct signatures and
record the adjustment.

## Task 4: Run With Explicit Hook Loading

- [ ] Run with explicit hook env:

```bash
export SCHEMATHESIS_HOOKS=tmp_schemathesis_hooks
export CONTROLHUB_SCHEMATHESIS_HOOK_LOG=hook-debug.log
rm -f hook-debug.log
make test-openapi-fuzz
```

- [ ] Record:

```text
exit code
whether hook-debug.log exists
which hook events appear
which operations appear
whether createResource / createResourceRelation appear
whether mutated events appear
whether exit status changed
```

- [ ] Inspect log:

```bash
head -50 hook-debug.log
rg -n "createResource|createResourceRelation|before_call|map_body|map_path_parameters|mutated" hook-debug.log || true
```

## Task 5: Test `flatmap_body` If Officially Supported

Only do this if v4.19 docs or installed package confirms the hook name and
signature.

- [ ] Add a temporary `flatmap_body` hook using Hypothesis strategies.

Expected intent:

```text
turn generated POST /resources body into a strategy that always carries
environmentId=1 and ownerId=1
```

- [ ] Run with the hook.

- [ ] Record whether it fires during fuzzing phase and whether generated
  requests use FK-aware values.

Do not keep this code unless it is adopted in a later implementation phase.

## Task 6: Classify Outcome

Choose one:

```text
Outcome A: hooks work for fuzzing-phase FK-aware generation
Outcome B: hooks load but do not solve this case
Outcome C: official API suggests a path, but requires a custom runner / larger implementation
```

Do not overclaim.

If hooks were not loaded, classify as "invalid experiment" and fix hook loading
before concluding.

## Task 7: Write Note

Create:

```text
docs/superpowers/notes/2026-05-26-phase-34b-schemathesis-hook-feasibility.md
```

Include:

```text
Schemathesis version
command used
SCHEMATHESIS_HOOKS value
hook file content or relevant snippets
hook events table
examples vs fuzzing behavior
whether FK fields were mutated
exit code before/after
final outcome A/B/C
recommendation
```

If hook code is useful as reference, include it as fenced code in the note.
Do not commit `tmp_schemathesis_hooks.py`.

## Task 8: Cleanup And Verification

Clean temporary files:

```bash
deactivate || true
rm -rf .venv-schemathesis-419 .schemathesis-reports
rm -f tmp_schemathesis_hooks.py hook-debug.log
```

Verify:

```bash
git status --short --branch
git diff --check
```

Expected:

```text
only docs/superpowers/notes/2026-05-26-phase-34b-schemathesis-hook-feasibility.md is changed/untracked
```

Run GitNexus change detection:

```text
gitnexus_detect_changes({scope: "all"})
```

## Task 9: Commit

```bash
git add docs/superpowers/notes/2026-05-26-phase-34b-schemathesis-hook-feasibility.md
git commit -m "docs: record schemathesis hook feasibility spike"
```

No `Co-Authored-By`.

Do not merge or push unless user explicitly asks.

## Final Report

Report:

```text
worktree / branch
commit hash
Schemathesis version
baseline v4.19 result
hook loading result
hook events summary
chosen outcome A/B/C
recommendation
files committed
cleanup confirmation
final git status
```

Scope confirmation:

```text
No product code changes
No OpenAPI changes
No workflow changes
No CI pin changes
No warning suppression
No skipped/deleted operations
No reduced checks/examples
No committed experimental scripts
No tag/release/deploy
No push
No AI co-author
```

