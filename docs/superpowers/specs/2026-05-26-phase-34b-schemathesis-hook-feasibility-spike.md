# Phase 34B Schemathesis Hook Feasibility Spike Design

## Background

Phase 34 investigated FK-aware Schemathesis generation and selected deferral.
However, the hook conclusion is not strong enough yet.

The current evidence says that attempted hooks did not solve v4.19 FK generation,
but it does not conclusively prove that Schemathesis hooks cannot work. The
failure may be due to:

- hook file not loaded correctly
- wrong hook type
- operation selector mismatch
- hook firing only in examples phase
- body mutation happening too late or not affecting fuzzing-phase generation
- using `map_body` when `flatmap_body` or `before_call` is required

Phase 34B is a narrow spike to verify hook feasibility correctly.

## Goal

Determine whether Schemathesis v4.19 hooks can constrain FK-like fields during
the fuzzing phase:

```text
POST /resources:
  environmentId
  ownerId

POST /resources/{id}/relations:
  path.id
  body.toResourceId
```

The spike must produce evidence, not assumptions.

## Non-Goals

- Do not remove the `schemathesis==4.15.2` CI pin.
- Do not change backend product behavior.
- Do not change SQL or migrations.
- Do not add OpenAPI FK enums.
- Do not suppress warnings.
- Do not skip operations.
- Do not reduce checks or examples.
- Do not swallow Schemathesis exit codes.
- Do not merge unactivated scripts into `scripts/`.
- Do not tag, release, or deploy.

## Official Hook Behaviors To Verify

Based on Schemathesis documentation, hooks may be registered globally with:

```python
@schemathesis.hook
```

or:

```python
@schemathesis.hook("hook_name")
```

CLI hook loading is done with:

```bash
export SCHEMATHESIS_HOOKS=hooks
schemathesis run ...
```

Potentially relevant hooks:

```text
map_path_parameters
map_body
flatmap_body
before_call
```

The spike must verify these assumptions against v4.19.0 behavior.

## Evidence Requirements

For each attempted hook, record:

```text
hook name
registration style
SCHEMATHESIS_HOOKS value
operation selector used
whether hook loaded
whether hook fired during examples phase
whether hook fired during fuzzing phase
fields before mutation
fields after mutation
whether generated request used mutated values
resulting operation status
```

Do not summarize as "hooks do not work" without this evidence.

## Hook Attempts

### Attempt 1: `map_path_parameters`

Purpose:

```text
Constrain path.id for POST /resources/{id}/relations and PATCH /resources/{id}
```

Expected:

```text
path.id becomes a known seed resource ID
```

### Attempt 2: `before_call`

Purpose:

```text
Mutate case.body immediately before request execution
```

Expected:

```text
POST /resources body uses known environmentId and ownerId
POST /resources/{id}/relations body uses known toResourceId
```

### Attempt 3: `flatmap_body`

Purpose:

```text
Alter generated body strategy instead of mutating only final examples
```

Expected:

```text
fuzzing phase body generation stays FK-aware
```

### Attempt 4: `map_body`

Purpose:

```text
Confirm whether map_body applies to REST JSON bodies in v4.19 fuzzing phase
```

Expected:

```text
If map_body is GraphQL-only or examples-only for this use case, document it.
```

## Success Criteria

The spike succeeds if it proves one of these outcomes:

### Outcome A: Hooks Work

Evidence:

- v4.19 hook loads through `SCHEMATHESIS_HOOKS`
- hook fires in fuzzing phase
- FK fields are mutated to seed IDs
- v4.19 exits 0
- 27/27 operations are still exercised
- checks/examples are not reduced

### Outcome B: Hooks Do Not Solve This Case

Evidence:

- hook loading is confirmed
- attempted hooks fire only in examples phase or do not affect body generation
- v4.19 still marks FK operations failed
- exact limitation is documented

### Outcome C: More Work Needed

Evidence:

- official API suggests a path, but implementation would require a custom Python
  runner or deeper integration
- Phase 34C should be created for that work

## Documentation Output

Write a note:

```text
docs/superpowers/notes/2026-05-26-phase-34b-schemathesis-hook-feasibility.md
```

The note must include the hook evidence table and recommendation.

If a hook module is created for the experiment, do not leave it under
`scripts/` unless it is actively used. Prefer:

```text
temporary file in worktree, cleaned before final
or code snippet embedded in the note
```

## Current CI Policy

Until Outcome A is proven and reviewed, backend heavy CI remains pinned to:

```text
schemathesis==4.15.2
```

