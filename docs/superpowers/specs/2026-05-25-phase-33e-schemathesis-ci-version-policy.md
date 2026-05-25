# Phase 33E Schemathesis CI Version Policy Design

## Background

Backend heavy CI exposed two real product defects:

- archive reason length was not validated before hitting a `VARCHAR(512)` column
- resource labels with Unicode control characters could reach MySQL JSON storage
  and surface as 500

Those defects were fixed in Phase 33 and Phase 33D.

After those product bugs were fixed, the remaining backend heavy CI failure is a
tooling compatibility issue:

```text
local Schemathesis:  v4.15.2 -> validation_mismatch is warning-like, exit 0
remote Schemathesis: v4.19.0 -> validation_mismatch marks operation failure, exit 1
```

The remaining validation mismatch operations are tied to database-backed
referential integrity:

```text
POST /resources
POST /resources/{id}/relations
```

Schemathesis v4.19 fuzzing mutates FK-like integer fields such as
`environmentId`, `ownerId`, and `toResourceId` to values that do not exist in
the disposable MySQL seed data. The backend correctly rejects those values.

## Goal

Restore stable backend heavy CI without weakening the API contract:

```text
make release-docker-gates
GitHub Actions manual heavy CI
```

Phase 33E should explicitly pin Schemathesis in CI to the currently validated
version while documenting the reason and deferring FK-aware generation to a
separate phase.

## Non-Goals

- Do not encode database FK seed IDs as OpenAPI enum values.
- Do not suppress warnings.
- Do not skip operations.
- Do not reduce Schemathesis checks.
- Do not reduce `MAX_EXAMPLES`.
- Do not swallow Schemathesis exit codes in the wrapper.
- Do not pin as a substitute for fixing 500s; the real 500 defects are already
  fixed.
- Do not tag, release, or deploy.

## Decision

Pin Schemathesis to `4.15.2` for the backend CI heavy gate and document why.

Rationale:

- v4.15.2 is the version used by the established local release gate and existing
  RC evidence.
- v4.15.2 still catches real hard contract failures. It caught the archive and
  labels defects once generated input reached those paths.
- The remaining v4.19 failure is operation-level `validation_mismatch` caused by
  runtime FK validation, not a 5xx/status/content-type/schema response failure.
- Encoding seed IDs such as `environmentId: [1, 2, 3]` and `ownerId:
  [1, 2, 3, 4, 5]` into OpenAPI would make the public API contract false.
- Wrapper-level exit-code filtering is riskier than a version pin because it may
  hide real future failures.

## Implementation Shape

Pin the CI installation step:

```bash
python -m pip install --upgrade "schemathesis==4.15.2"
```

Do not change local `scripts/openapi-fuzz.sh` checks:

```text
not_a_server_error
status_code_conformance
content_type_conformance
response_schema_conformance
```

Do not change:

```text
MAX_EXAMPLES=50
--mode all
--phases examples,fuzzing
operation set
```

## Documentation Requirements

Update release evidence and checklist to state:

- backend heavy CI pins Schemathesis to `4.15.2`
- reason: v4.19 changes `validation_mismatch` operation-level exit behavior for
  DB-backed FK fields
- this is not warning suppression
- this is not a product bug bypass
- Phase 34 should investigate FK-aware Schemathesis data generation for v4.19+

## Phase 34 Deferred Work

Future direction:

```text
Phase 34: Schemathesis FK-aware data generation
```

Goals:

- keep newer Schemathesis versions
- generate valid DB-backed IDs for FK-like fields
- avoid OpenAPI enum pollution
- avoid wrapper exit-code filtering
- keep validation mismatch visible as audit signal

Possible approaches:

- Python runner if Schemathesis exposes a supported API for case mutation
- supported config/hooks if available in a future Schemathesis version
- dedicated seed-aware data generation step

## Success Criteria

Phase 33E is complete when:

- backend CI heavy workflow installs Schemathesis `4.15.2`
- local backend gates still pass
- backend workflow change is reviewed and merged
- backend main is pushed
- remote manual heavy CI passes
- RC evidence records:
  - previous v4.19 failure
  - version pin rationale
  - successful heavy CI run URL
  - remaining accepted warning status
- no tag/release/deploy occurs

