# Phase 34B — Schemathesis Hook Feasibility Spike

## Environment

| Field | Value |
|---|---|
| Worktree | `.worktrees/backend-phase-34b-schemathesis-hook-feasibility` |
| Branch | `phase-34b-schemathesis-hook-feasibility` |
| Schemathesis version | 4.19.0 |
| Python | 3.12.12 |
| Date | 2026-05-26 |

## Commands

Baseline:

```bash
source .venv-schemathesis-419/bin/activate
make test-openapi-fuzz
```

With hooks:

```bash
source .venv-schemathesis-419/bin/activate
SCHEMATHESIS_HOOKS=tmp_schemathesis_hooks \
  CONTROLHUB_SCHEMATHESIS_HOOK_LOG=hook-debug.log \
  PYTHONPATH=$(pwd):$PYTHONPATH \
  make test-openapi-fuzz
```

`PYTHONPATH` is required because `openapi-fuzz.sh` runs Schemathesis from the Go test CWD (`internal/integration/`), not the repo root where the hook module lives.

## Baseline v4.19 Result (No Hooks)

| Field | Value |
|---|---|
| Exit code | 1 (via script) / 2 (via go test) |
| Operations | 27/27 |
| Examples phase | 5 passed, 22 skipped |
| Fuzzing phase | 25 passed, **2 failed** |
| Cases | 883 generated |
| Failed operations | `POST /resources`, `POST /resources/{id}/relations` |
| Warning | validation_mismatch on both FK operations |

## Hook Loading

| Field | Value |
|---|---|
| `SCHEMATHESIS_HOOKS` | `tmp_schemathesis_hooks` |
| `PYTHONPATH` | repo root (required) |
| Load result | **Success** — no "Unable to load" error |
| Hook registration | `@schemathesis.hook` decorator |
| Operation label format | `"POST /resources"` (method + path, not operationId) |

## Hook File

```python
"""
Key structure of tmp_schemathesis_hooks.py (corrected version).
Operation labels use "METHOD /path" format, not operationId.
"""
import json, os
import schemathesis
from hypothesis import strategies as st

LOG = os.environ.get("CONTROLHUB_SCHEMATHESIS_HOOK_LOG", "hook-debug.log")

def _label_matches(ctx, *fragments):
    label = getattr(ctx.operation, "label", "") if ctx and ctx.operation else ""
    return all(f in label for f in fragments)

@schemathesis.hook
def before_call(ctx, case, kwargs):
    body = getattr(case, "body", None)
    if _label_matches(ctx, "POST", "/resources") and not _label_matches(ctx, "/{id}/"):
        if isinstance(body, dict):
            body["environmentId"] = 1
            body["ownerId"] = 1
    elif _label_matches(ctx, "POST", "/resources/{id}/relations"):
        if isinstance(body, dict):
            body["toResourceId"] = 2

@schemathesis.hook
def flatmap_body(ctx, body):
    if not isinstance(body, dict):
        return st.just(body)
    if _label_matches(ctx, "POST", "/resources") and not _label_matches(ctx, "/{id}/"):
        @st.composite
        def fix_fk(draw, orig):
            b = dict(orig)
            b["environmentId"] = 1
            b["ownerId"] = 1
            return b
        return fix_fk(body)
    if _label_matches(ctx, "POST", "/resources/{id}/relations"):
        @st.composite
        def fix_fk(draw, orig):
            b = dict(orig)
            b["toResourceId"] = 2
            return b
        return fix_fk(body)
    return st.just(body)

@schemathesis.hook
def map_path_parameters(ctx, path_parameters):
    if _label_matches(ctx, "/{id}/"):
        params = dict(path_parameters or {})
        params["id"] = 1
        return params
    return path_parameters
```

## Hook Events Summary

| Hook event | Count | Fires in fuzzing? |
|---|---|---|
| `before_call` (all operations) | 921 | Yes |
| `before_call` mutated POST /resources | 26 | Yes |
| `before_call` mutated POST /relations | 49 | Yes |
| `flatmap_body` fixing POST /resources | 25 | Yes |
| `flatmap_body` fixing POST /relations | 46 | Yes |
| `map_body` POST /resources | 25 | Yes |
| `map_body` POST /relations | 86 | Yes |
| `map_path_parameters` (all) | 1107 | Yes |
| `map_path_parameters` mutated | 656 | Yes |

## FK Field Mutation Evidence

`flatmap_body` caught and corrected random FK values during fuzzing:

```json
{"event": "flatmap_body_fixing_POST_resources", "envId_before": 2, "ownerId_before": 20374620615}
{"event": "flatmap_body_fixing_POST_resources", "envId_before": 2, "ownerId_before": 2}
```

These were corrected to `environmentId: 1, ownerId: 1` by the hook.

## Result With Hooks

| Field | Value |
|---|---|
| Exit code | 1 (via script) — **still non-zero** |
| Operations | 27/27 |
| Fuzzing phase | **26 passed, 1 failed** (was 25/2) |
| Cases | 921 generated |

The 1 remaining failure is `PATCH /resources/{id}` returning 500 on unicode control chars in `labels` — the same Phase 33C bug. This is a backend defensive-coding gap, not an FK issue.

The validation_mismatch warning still appears for both POST operations because other fuzzed fields (names, labels, types) generate values the backend rejects with 400. This is expected — hooks only constrain FK fields, not all validation.

## Hook Behavior by Phase

| Hook | Examples phase | Fuzzing phase | Notes |
|---|---|---|---|
| `before_call` | Fires | Fires | Can mutate `case.body` and `case.path_parameters` in-place |
| `map_body` | Fires | Fires | Receives body before `flatmap_body` transforms it |
| `flatmap_body` | Fires | Fires | Replaces body strategy — returned strategy generates all future bodies |
| `map_path_parameters` | Fires | Fires | Transforms each generated path_parameters value |

All hooks fire in both phases. `flatmap_body` is the most effective for FK constraints because it replaces the Hypothesis strategy, ensuring every generated body from that point has the corrected FK values.

## Outcome

**Outcome A — Hooks work for FK-aware generation** with one caveat.

Hooks successfully solve the FK-aware generation problem:

- v4.19 hooks load through `SCHEMATHESIS_HOOKS` + `PYTHONPATH`
- `flatmap_body` fires during fuzzing phase and constrains FK fields
- `before_call` can additionally mutate body/path parameters before execution
- FK-related failures dropped from 2 to 0 (both `POST /resources` and `POST /resources/{id}/relations` now pass)
- 27/27 operations exercised
- 921 cases generated (was 883 baseline — hooks don't reduce coverage)

Caveat: v4.19 does not exit 0 because of a separate backend bug — `PATCH /resources/{id}` returns 500 when labels contain unicode control characters. This bug exists regardless of hooks and needs a separate fix in `internal/api/resource_handler.go` or `internal/service/resource_service.go`.

## Recommendation

1. **The FK problem is solvable with hooks.** A hook module can be committed under `scripts/` as the official FK-aware generation hook for Schemathesis v4.19+.

2. **The `PYTHONPATH` requirement is the main friction point.** The hook module must be importable from the Go test's CWD. Options:
   - Set `PYTHONPATH` in `openapi-fuzz.sh` (simplest)
   - Place the hook module in `internal/integration/` (co-located with the test)
   - Use a Python path configuration file

3. **The PATCH 500 bug should be fixed separately** (Phase 34C or later). This is a backend input-validation gap, not a Schemathesis configuration issue.

4. **The CI pin (`schemathesis==4.15.2`) should not be removed yet.** Wait until:
   - The hook module is committed and integrated into `openapi-fuzz.sh`
   - The PATCH 500 bug is fixed
   - Backend heavy CI passes on v4.19 with hooks

## Hook Signatures Reference (v4.19)

```
before_call(context: HookContext, case: Case, kwargs: dict) -> None
map_body(context: HookContext, body: Any) -> Any
flatmap_body(context: HookContext, body: Any) -> Any  # returns SearchStrategy
map_path_parameters(context: HookContext, path_parameters: Any) -> Any
map_case(context: HookContext, case: Any) -> Any
```

`HookContext.operation.label` returns `"METHOD /path"` (e.g., `"POST /resources"`), not the OpenAPI `operationId`.

## Scope Confirmation

| Constraint | Status |
|---|---|
| No product code changes | Confirmed |
| No OpenAPI changes | Confirmed |
| No workflow changes | Confirmed |
| No CI pin changes | Confirmed — pin remains at 4.15.2 |
| No warning suppression | Confirmed |
| No skipped/deleted operations | Confirmed — 27/27 |
| No reduced checks/examples | Confirmed — same 4 checks, 50 examples, seed 42 |
| No committed experimental scripts | Confirmed — `tmp_schemathesis_hooks.py` not committed |
| No tag/release/deploy | Confirmed |
| No push | Confirmed — commit stays local |
| No AI co-author | Confirmed |
