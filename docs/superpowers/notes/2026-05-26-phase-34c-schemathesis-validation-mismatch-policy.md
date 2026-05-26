# Phase 34C — Schemathesis Validation Mismatch Policy Evidence

## Environment

| Field | Value |
|---|---|
| Worktree | `.claude/worktrees/backend-phase-34c-schemathesis-validation-mismatch-policy` |
| Branch | `phase-34c-schemathesis-validation-mismatch-policy` |
| Schemathesis version | 4.19.0 |
| Python | 3.12.12 |
| Date | 2026-05-27 |
| Base commit | `12841c9` (Phase 34C docs-only push) |

## Required Reading

- `docs/superpowers/specs/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md`
- `docs/superpowers/plans/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md`
- `docs/superpowers/notes/2026-05-26-phase-34b-schemathesis-hook-feasibility.md`
- `docs/superpowers/specs/2026-05-25-phase-33e-schemathesis-ci-version-policy.md`

## v4.19 Reproduction

### Environment Setup

```bash
python3 -m venv .venv-schemathesis-419
source .venv-schemathesis-419/bin/activate
python -m pip install --upgrade pip
python -m pip install "schemathesis==4.19.0"
schemathesis --version
```

Version confirmed: `schemathesis, version 4.19.0`

### Command

```bash
source .venv-schemathesis-419/bin/activate
make test-openapi-fuzz
```

### Result

| Field | Value |
|---|---|
| Exit code | 1 (Schemathesis) / 2 (go test wrapper) |
| Operations selected | 27/27 |
| Examples phase | 5 passed, 22 skipped |
| Fuzzing phase | 25 passed, **2 failed** |
| Cases generated | 883 |
| Cases passed | 883 |
| JUnit failures | **0** |
| JUnit errors | **0** |
| Failed operations | `POST /resources`, `POST /resources/{id}/relations` |
| Warning | Schema validation mismatch (2 operations) |

Key observation: identical to Phase 34B baseline. All 883 individual test cases pass all configured checks. The "2 failed" is an operation-level classification from `validation_mismatch`, not from actual check failures.

### Console Output (Abbreviated)

```text
Schemathesis v4.19.0

Operations:       27 selected / 27 total

 ✅  Examples (in 0.79s)
     ✅  5 passed  ⏭  22 skipped

 ❌  Fuzzing (in 7.41s)
     ✅ 25 passed  ❌  2 failed

=================================== WARNINGS ===================================

Schema validation mismatch: 2 operations mostly rejected generated data due to
validation errors, indicating schema constraints don't match API validation

  - POST /resources
  - POST /resources/{id}/relations

Test cases:
  883 generated, 883 passed

============================== 1 warning in 8.23s ==============================

Schemathesis: found contract violations (exit 1).
```

## v4.19 Configuration Surface Analysis

### CLI Options (`schemathesis run --help`)

Relevant flags inspected:

| Flag | Relevance to validation_mismatch |
|---|---|
| `--warnings WARNINGS` | Controls display only (`off` or list of types to enable). Does NOT prevent operation-level failure. |
| `--checks` | Already set to 4 checks. No validation_mismatch-related check exists. |
| `--mode` | `all` generates positive + negative data. Changing to `positive` might reduce rejections but would weaken coverage. |
| `--phases` | Already set to `examples,fuzzing`. |
| `--max-examples` | Already set to 50. |
| `--generation-deterministic` | Only affects reproducibility, not failure classification. |
| `--continue-on-failure` | Continues past failures but doesn't prevent exit 1. |

No CLI flag exists to downgrade `validation_mismatch` from operation-failure to pass-with-warning.

### TOML Configuration (`schemathesis.toml`)

Current config:

```toml
[warnings]
display = ["missing_auth", "missing_test_data", "validation_mismatch"]
fail-on = []
```

### Source Code Evidence (v4.19.0)

Exit code 1 is set through TWO independent mechanisms in v4.19:

**Mechanism 1 — PhaseFinished (the actual cause):**

File: `cli/commands/run/context.py`

```python
def on_event(self, event):
    ...
    elif isinstance(event, events.PhaseFinished)
        and event.phase.is_enabled
        and event.status in (Status.FAILURE, Status.ERROR):
        self.exit_code = 1
```

The fuzzing phase gets `Status.FAILURE` because 2 operations have `ScenarioFinished` with `status=FAILURE`. This propagates to `PhaseFinished` with `status=FAILURE`, which sets `exit_code = 1`.

**Mechanism 2 — Warning fail-on (not the cause here):**

File: `cli/commands/run/handlers/output.py`

```python
def _handle_warning(self, ctx, kind, record_callback):
    if not self.config.warnings.should_display(kind):
        return
    record_callback()
    if self.config.warnings.should_fail(kind):
        ctx.exit_code = 1
```

Our TOML `fail-on = []` makes `should_fail(VALIDATION_MISMATCH)` return `False`, so this path does NOT set `exit_code = 1`. But it doesn't matter because Mechanism 1 already sets it.

**validation_mismatch threshold:**

File: `cli/commands/run/handlers/output.py`

```python
OTHER_CLIENT_ERRORS_THRESHOLD = 0.1

def should_warn_about_validation_mismatch(self):
    ...
    return (count_other_4xx / total_4xx) >= OTHER_CLIENT_ERRORS_THRESHOLD
```

The threshold is 10% — if >= 10% of responses are non-404 4xx, the operation triggers validation_mismatch. For POST /resources and POST /resources/{id}/relations, fuzzed data with invalid FK values (environmentId, ownerId, toResourceId) causes many 400 responses, easily exceeding this threshold.

**Key conclusion:** In v4.19, `validation_mismatch` causes operation-level `Status.FAILURE` in the engine. This is independent of the `fail-on` TOML configuration. The `fail-on` only controls a secondary warning-based exit code path. There is NO configuration option to prevent the engine from classifying these operations as failed.

## Option Comparison

### Option A: Keep 4.15.2 Pin

| Criterion | Assessment |
|---|---|
| Implementation complexity | Zero — already in place |
| Risk of hiding real 5xx/schema failures | None — v4.15.2 catches real defects (proven by Phase 33 archive/labels fixes) |
| Risk of distorting public OpenAPI contract | None — no contract changes needed |
| Remote heavy CI reliability | Proven — multiple successful runs on GitHub Actions |
| Maintenance burden | Low — revisit when Schemathesis adds a config option for validation_mismatch |

### Option B: Custom Python Runner

| Criterion | Assessment |
|---|---|
| Implementation complexity | High — would need to reimplement test execution, report generation, and result classification |
| Risk of hiding real 5xx/schema failures | Medium — custom classification logic could miss edge cases |
| Risk of distorting public OpenAPI contract | None |
| Remote heavy CI reliability | Uncertain — custom runner would need its own CI integration |
| Maintenance burden | High — must track Schemathesis API changes across versions |

### Option C: Contract / Test Data Reshaping

| Criterion | Assessment |
|---|---|
| Implementation complexity | Medium — would require OpenAPI schema changes and possibly seed data coordination |
| Risk of hiding real 5xx/schema failures | Low if done correctly |
| Risk of distorting public OpenAPI contract | High — adding FK enums or request constraints for seed IDs would make the public contract misleading |
| Remote heavy CI reliability | Medium — depends on contract stability |
| Maintenance burden | Medium — contract changes must track schema evolution |

## Decision

```
Decision: Option A
Reason: v4.19 validation_mismatch causes operation-level Status.FAILURE through the engine's
        ScenarioFinished event, independent of the TOML fail-on configuration. All 883 test
        cases pass all configured checks (not_a_server_error, status_code_conformance,
        content_type_conformance, response_schema_conformance) with 0 JUnit failures. The
        "2 failed" is purely an aggregate classification: POST /resources and
        POST /resources/{id}/relations reject fuzzed FK-like values (environmentId, ownerId,
        toResourceId) that don't exist in the disposable seed database. No CLI flag or TOML
        option in v4.19 can downgrade this classification without reducing coverage or
        distorting the OpenAPI contract. The 4.15.2 pin remains intentional, not accidental.
CI pin: retained
```

## CI Pin Status

The `schemathesis==4.15.2` pin in `.github/workflows/backend-ci.yml` remains unchanged and intentional.

The pin can be reconsidered when:

1. Schemathesis adds a configuration option to make `validation_mismatch` non-blocking at the engine level
2. The ControlHub API adds request-level validation that returns 422 before hitting FK checks
3. A future Schemathesis version changes the validation_mismatch threshold or classification

## Next Recommended Phase

No immediate follow-up phase is required. The pin is stable and intentional.

If the team wants to revisit in the future, potential directions:

- Monitor Schemathesis releases for a `validation_mismatch` engine-level toggle
- Consider adding request-body validation middleware that returns 422 for clearly invalid FK values before database lookup
- Evaluate Schemathesis Python API for a minimal custom runner if the team wants to adopt v4.19+ features

## Scope Confirmation

| Constraint | Status |
|---|---|
| No product code changes | Confirmed |
| No OpenAPI FK enum | Confirmed |
| No workflow changes | Confirmed |
| No CI pin changes | Confirmed — pin remains at 4.15.2 |
| No warning suppression | Confirmed — `display` list unchanged |
| No skipped/deleted operations | Confirmed — 27/27 |
| No reduced checks/examples | Confirmed — same 4 checks, 50 examples, seed 42 |
| No exit-code swallowing | Confirmed — `scripts/openapi-fuzz.sh` unchanged |
| No tag/release/deploy | Confirmed |
| No push | Confirmed — commit stays local in worktree |
| No AI co-author | Confirmed |
