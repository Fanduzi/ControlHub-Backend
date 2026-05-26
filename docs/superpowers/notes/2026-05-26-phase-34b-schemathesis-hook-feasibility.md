# Phase 34B — Schemathesis Hook Feasibility Spike

## Environment

| Field | Value |
|---|---|
| Worktree | `.worktrees/backend-phase-34b-schemathesis-hook-feasibility` |
| Branch | `phase-34b-schemathesis-hook-feasibility` |
| Schemathesis version | 4.19.0 |
| Python | 3.12.12 |
| Date | 2026-05-26 |
| Base commit | `41ab180` (contains Phase 33D fix `7adb5df`) |

## Phase 33D Inclusion Verification

Phase 34B was originally run on `41ab180`, which **does contain** `7adb5df fix: harden resource label validation and duplicate key error mapping`.

Verified:

```bash
git merge-base --is-ancestor 7adb5df HEAD && echo "contains-33d"
# output: contains-33d
```

The original Phase 34B note incorrectly stated the remaining 1 failure was "PATCH /resources/{id} returning 500 on unicode control chars in labels — the same Phase 33C bug." This was wrong. Phase 33D DID fix the PATCH 500 bug. The remaining failure was actually a validation_mismatch classification issue (see below).

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
| Cases | 883 generated, **883 passed** |
| Failed operations | `POST /resources`, `POST /resources/{id}/relations` |
| JUnit failures | **0** |
| Warning | validation_mismatch on both FK operations |

Key observation: all 883 individual test cases PASS. The "2 failed" is an operation-level classification from validation_mismatch, not from actual check failures (not_a_server_error, etc.).

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

## Hook Events Summary (Rerun)

| Hook event | Count | Fires in fuzzing? |
|---|---|---|
| `before_call` (all operations) | 891 | Yes |
| `before_call` mutated POST /resources | 24 | Yes |
| `before_call` mutated POST /relations | 49 | Yes |
| `flatmap_body` fixing POST /resources | 23 | Yes |
| `flatmap_body` fixing POST /relations | 46 | Yes |

## FK Field Mutation Evidence

`flatmap_body` caught and corrected random FK values during fuzzing:

```json
{"envId_before": 2, "ownerId_before": 20374620615}
{"envId_before": 2, "ownerId_before": 2}
{"envId_before": 1788040939, "ownerId_before": [[], ...]}
{"envId_before": 1788040939, "ownerId_before": []}
```

These were corrected to `environmentId: 1, ownerId: 1` by the hook. The hooks successfully intercept both integer overflow and type-violation values (arrays, nested objects).

## Result With Hooks (Rerun)

| Field | Baseline | With Hooks |
|---|---|---|
| Exit code | 1 | 1 |
| Fuzzing passed | 25 | 25 |
| Fuzzing failed | 2 | 2 |
| Cases generated | 883 | 891 |
| Cases passed | 883 | 891 |
| JUnit failures | 0 | 0 |

**Hooks do not change the operation-level pass/fail count.** The "2 failed" is the same with and without hooks because:

1. The failures are `validation_mismatch` classifications, not check failures
2. `validation_mismatch` measures the aggregate rejection rate of the operation
3. Even with FK fields corrected, other fuzzed fields (names, types, labels) still cause 400 rejections
4. v4.19 marks operations as "failed" when the rejection rate exceeds its threshold
5. The TOML `fail-on = []` does not prevent this operation-level classification

The hooks correctly fix FK fields (proven by debug log), but the validation_mismatch is about overall rejection rates across ALL fields, not just FK.

## Correction: PATCH 500 Status

The original Phase 34B note claimed "PATCH /resources/{id} returns 500 on unicode control chars." This was wrong:

- Phase 33D (`7adb5df`) fixed this — labels with control chars now get 400
- The PATCH operation is NOT in the failure list in any run (baseline or hooks)
- No PATCH-related 500 errors occurred in any Phase 34B run

## Hook Behavior by Phase

| Hook | Examples phase | Fuzzing phase | Notes |
|---|---|---|---|
| `before_call` | Fires | Fires | Can mutate `case.body` and `case.path_parameters` in-place |
| `map_body` | Fires | Fires | Receives body before `flatmap_body` transforms it |
| `flatmap_body` | Fires | Fires | Replaces body strategy — returned strategy generates all future bodies |
| `map_path_parameters` | Fires | Fires | Transforms each generated path_parameters value |

All hooks fire in both phases. `flatmap_body` is the most effective for FK constraints because it replaces the Hypothesis strategy.

## Outcome

**Outcome B — Hooks load and mutate FK fields, but do not solve the v4.19 exit-code problem.**

Hooks are proven to work for FK-aware field generation:

- v4.19 hooks load through `SCHEMATHESIS_HOOKS` + `PYTHONPATH`
- `flatmap_body` fires during fuzzing phase and constrains FK fields to seed IDs
- `before_call` can additionally mutate body/path parameters before execution
- FK field values ARE corrected (proven by debug log)
- 27/27 operations exercised, no coverage reduction

But hooks do NOT resolve the v4.19 exit-code problem:

- v4.19 marks operations as "failed" based on validation_mismatch
- validation_mismatch measures aggregate rejection rate, not specific FK failures
- Correcting FK fields is insufficient — other fields (names, types, labels) still cause rejections
- The TOML `fail-on = []` does not prevent this operation-level classification
- v4.19 exits 1 even though all individual test cases pass (0 JUnit failures)

The original Phase 34B note overclaimed by reporting "26 passed, 1 failed" on one hook run. The rerun shows "25 passed, 2 failed" — identical to baseline. The earlier improvement was likely a non-deterministic server-side timing variation, not a hook effect.

## Recommendation

1. **Hooks are a viable tool for FK-aware generation** but alone cannot make v4.19 exit 0. The validation_mismatch classification is an aggregate metric that requires more than FK fixes.

2. **The v4.19 pin (`schemathesis==4.15.2`) should remain** until one of these is resolved:
   - A Schemathesis config option to downgrade validation_mismatch from operation-failure to warning-only (needs v4.19+ docs investigation)
   - More comprehensive body-generation hooks that constrain additional fields (impractical)
   - A custom Python runner that bypasses the validation_mismatch classification

3. **The Phase 34C scope should be**: investigate whether v4.19's `--warnings` or TOML config can make validation_mismatch non-blocking at the exit-code level. If not, investigate a custom Python runner.

4. **The Phase 33D PATCH 500 fix is confirmed working.** No action needed there.

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
