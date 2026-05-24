# Phase 31 OpenAPI Fuzz Warning Cleanup Design

## Background

Phase 30 produced a local release candidate evidence bundle with a `GO`
decision, but it also preserved two non-blocking OpenAPI fuzz warnings:

```text
1. PATCH /resources/{id} repeatedly returned 404 because Schemathesis generated
   IDs that did not match an existing resource.

2. Schema validation mismatch warnings remained for:
   - PATCH /resources/{id}
   - POST /auth/login
   - POST /resources
```

The backend release gate still passed:

```text
make test-openapi-fuzz
27/27 operations tested
960/960 generated cases passed
all configured checks passed:
  not_a_server_error
  status_code_conformance
  content_type_conformance
  response_schema_conformance
```

Phase 31 is a contract-quality cleanup phase. It should reduce warning noise
without weakening backend validation or changing product behavior.

## Goal

Make `make test-openapi-fuzz` cleaner by reducing or eliminating known
Schemathesis warnings while preserving the existing API contract and backend
validation behavior.

Success target:

```text
make test-openapi-fuzz
PASS
960/960 generated cases pass
fewer warnings than Phase 30, ideally zero warnings
```

If zero warnings are not practical, remaining warnings must be explicitly
classified with evidence.

## Non-Goals

- Do not change product behavior.
- Do not relax backend validation to satisfy generated fuzz input.
- Do not broaden OpenAPI schemas beyond what the backend accepts.
- Do not skip warned operations.
- Do not suppress Schemathesis warnings globally.
- Do not reduce fuzz checks.
- Do not lower `MAX_EXAMPLES` just to hide warnings.
- Do not add auth middleware or change login behavior.
- Do not alter SQL, migrations, seed data for production, or topology behavior.
- Do not publish, tag, push, or release.

## Current Warning Analysis

### Warning 1: Missing Valid Path Data For `PATCH /resources/{id}`

Schemathesis generates arbitrary path IDs. Most generated IDs do not exist in
the disposable integration database, so the endpoint legitimately returns 404.
This prevents generated requests from exercising the core update path.

Preferred fix:

```text
Provide Schemathesis with an explicit valid example for `{id}` or an operation
example that uses an existing seeded resource ID.
```

Acceptable implementation directions:

- Add OpenAPI examples for path parameter `id` on `PATCH /resources/{id}` using
  a stable integration seed ID.
- Add Schemathesis hook/config support to inject known path values, if it stays
  small and local to the fuzz harness.

Not acceptable:

- Make PATCH accept nonexistent IDs.
- Treat 404 as success by skipping the operation.
- Add broad suppression for 404 warnings.

### Warning 2: Schema Validation Mismatch

Schemathesis reports that generated invalid data is often rejected by runtime
validation before the API's core logic is reached. That usually means the
OpenAPI schema is broader than the actual request validation.

Warned operations:

```text
POST /auth/login
POST /resources
PATCH /resources/{id}
```

Preferred fix:

```text
Tighten OpenAPI request schemas to match existing backend validation.
```

Important boundary:

```text
OpenAPI should move toward backend validation. Backend validation should not be
relaxed to match a looser schema.
```

## Expected Schema Tightening Areas

The implementation worker must inspect the actual Go request models and
handlers before editing schema. These are likely areas to verify:

### `POST /auth/login`

Schema should represent existing behavior:

- required `email`
- required `password`
- both strings
- `minLength: 1` where empty values are rejected
- `format: email` only if current validation expects email-like input

### `POST /resources`

Schema should represent existing behavior:

- required fields that the backend requires
- enum values for constrained fields
- non-empty string constraints where enforced
- object shapes for labels and profile-like fields
- no invented resource types, health statuses, lifecycle statuses, or sources

### `PATCH /resources/{id}`

Schema should represent existing behavior:

- only mutable fields
- enum values for mutable enum fields
- non-empty constraints where enforced
- labels object shape
- path parameter example or Schemathesis data hook for a valid ID

If the backend currently ignores unknown immutable fields instead of rejecting
them, do not invent stricter behavior without a test and explicit review.

## Test Strategy

Required gates:

```text
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Contract-focused tests should be added only when they lock existing behavior or
prevent schema drift.

Useful tests:

- OpenAPI YAML validation still passes.
- Existing handler tests for malformed login/resource payloads still pass.
- If adding examples/hooks, add a test that verifies the example uses a seeded
  resource ID or that the hook injects a valid ID.

Do not add tests that merely assert implementation details of Schemathesis
unless those details are part of the release gate behavior.

## Warning Outcome Policy

Final report must compare:

```text
before:
  missing valid path data: PATCH /resources/{id}
  schema mismatch: PATCH /resources/{id}, POST /auth/login, POST /resources

after:
  exact warning list from make test-openapi-fuzz
```

Accepted outcomes:

1. **Ideal:** zero warnings, all gates pass.
2. **Acceptable:** fewer warnings, all gates pass, remaining warnings explained
   with specific technical blockers.
3. **Not acceptable:** same warnings remain without new evidence.
4. **Blocking:** any Schemathesis check fails, any required gate fails, or any
   warning is suppressed instead of explained.

## Scope Control

Phase 31 should be small. Prefer:

```text
OpenAPI schema edits
small fuzz harness data/example support
focused tests
documentation update to candidate warning status if warning count changes
```

Avoid:

```text
large validator refactors
new code generation
new contract testing framework
runtime behavior changes
new database migrations
```

## Success Criteria

Phase 31 is complete when:

- dedicated worktree and branch were used
- warning causes were verified from actual fuzz output
- changes are limited to OpenAPI/fuzz harness/tests/docs as needed
- `make test-openapi-fuzz` passes
- full backend release gates pass, or any skipped Docker gate is explicitly
  reported as a blocker
- before/after warnings are documented
- no backend validation was weakened
- no product/API behavior was changed without explicit tests and rationale

