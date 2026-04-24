# 2026-04-23 Subagent-Driven Execution Standard

This document defines the required execution layer for subagent-driven work in this repo.

It exists because design/spec and implementation plan alone do not provide a reliable answer to these operational questions:

- what is actually in progress right now
- which task has really passed review
- which work is implemented but not yet closed
- what evidence supports closure
- what was explicitly deferred by user decision

Use this standard for any future initiative that is large enough to be planned and executed in multiple tasks.

## Required Artifact Set

Every major initiative should have four layers:

1. `design/spec`
   - defines what should be built
2. `implementation plan`
   - defines how the work is decomposed
3. `execution ledger`
   - defines what actually happened task by task
4. `closure note`
   - defines where the initiative stopped and why that stop is valid

If layer 3 is missing, the initiative is not execution-safe.

## When The Execution Ledger Must Be Created

Create the execution ledger immediately after the user approves implementation and before the first implementation subagent starts work.

Do not wait until the middle of execution.
Do not reconstruct it from memory later.
Do not treat the task tool alone as a substitute for the ledger.

## File Placement

- Ledger location: `docs/superpowers/notes/`
- File name: `YYYY-MM-DD-<initiative>-execution-ledger.md`

Examples:

- `2026-04-23-resource-crud-redesign-execution-ledger.md`
- `2026-04-23-topology-orchestrator-upgrade-execution-ledger.md`

## Ownership

The main coordinating agent owns the ledger.

That means the coordinator is responsible for:

- creating it before implementation begins
- updating it when task state changes
- recording gate outcomes from subagent reviews
- recording validation evidence
- writing the final closure decision

Subagents may produce evidence, but they do not own ledger truth.

## Decision Boundary Rules

The ledger must always carry the current decision boundary in plain language:

- in scope now
- explicitly out of scope now
- deferred by user instruction
- blocked pending user decision

This prevents silent scope expansion.

## Required Task Board Fields

Every tracked task must include all of the following:

- task number
- summary
- status
- implemented
- spec review
- code review
- marked done
- notes

Recommended statuses:

- `pending`
- `in_progress`
- `blocked`
- `completed`
- `abandoned`

## Status Transition Rules

Allowed transitions:

- `pending -> in_progress`
- `in_progress -> blocked`
- `blocked -> in_progress`
- `in_progress -> completed`
- `pending -> abandoned`
- `in_progress -> abandoned`
- `blocked -> abandoned`

Do not skip directly from `pending` to `completed` unless the user explicitly removed the need for implementation.

## Gate Rules Per Task

A task is only closed when all of these are true in order:

1. implementation exists
2. spec review passed
3. code review passed
4. task is marked done in the ledger

Minimum gate columns:

- `Implemented`: `yes/no`
- `Spec Review`: `yes/no`
- `Code Review`: `yes/no`
- `Marked Done`: `yes/no`

If one gate is missing, the task is not closed.

## Review Recording Rules

For every task review, the ledger should record:

- reviewer type
- outcome
- key issue summary if review failed
- what changed before re-review
- final approval state

The goal is not full transcript storage; the goal is closure evidence.

## Execution Log Rules

The execution log should be updated when any of these happen:

- a task starts
- a task becomes blocked
- a review fails
- a review passes
- validation finishes
- user changes scope or sequence
- closure decision is made

Each log entry should contain:

- action
- evidence
- outcome
- next

Do not write vague log entries like "worked on task 3".
Use concrete evidence such as file paths, commands, reviewer outcomes, or validation results.

## Validation Evidence Rules

The ledger must distinguish between:

- tests that passed
- tests not run
- tests intentionally skipped
- runtime/manual validation performed
- runtime/manual validation not possible

Never imply validation happened if it did not.

## Closure Rules

Before an initiative can be called closed, the ledger must state:

- final status
- what shipped
- what did not ship
- why closure is acceptable
- rollback/fallback position
- follow-up initiative if any

If closure is partial, say partial.
Do not flatten partial closure into a fake completion state.

## Relationship To Task Tool

The task tool is useful live operational state.
The execution ledger is durable execution truth.

Use both:

- task tool for active session coordination
- execution ledger for initiative-level traceability

If they diverge, update the ledger to match reality immediately.

## Minimum Operating Discipline

- Update state at the moment it changes.
- Record evidence, not intention.
- Keep deferred items explicit.
- Record user decisions that change sequence or scope.
- Never rely on memory for closure.
- Never mark a task done before review gates pass.

## Required Template

```md
# <Initiative Name> Execution Ledger

## Scope

- Initiative:
- Primary spec:
- Primary implementation plan:
- Working branch/worktree:
- Start date:
- Owner:

## Current Decision Boundary

- In scope:
- Explicitly out of scope:
- Deferred by user:
- Blocked pending decision:

## Task Board

| Task | Summary | Status | Implemented | Spec Review | Code Review | Marked Done | Notes |
|------|---------|--------|-------------|-------------|-------------|-------------|-------|
| 1 | ... | pending | no | no | no | no | ... |
| 2 | ... | in_progress | yes/no | yes/no | yes/no | yes/no | ... |

## Execution Log

### <YYYY-MM-DD HH:MM>

- Action:
- Evidence:
- Outcome:
- Next:

## Validation Evidence

- Unit / package tests:
- Integration tests:
- Runtime/manual smoke:
- Contract/schema validation:
- Known skipped validation:

## Open Issues

- Issue:
  - Severity:
  - Owner:
  - Blocking:
  - Plan:

## Closure Decision

- Final status: completed / partial / blocked / abandoned
- What shipped:
- What did not ship:
- Why closure is acceptable:
- Rollback / fallback:
- Follow-up initiative:
```

## Immediate Application Rule

Before starting either of the next major initiatives, create the ledger first:

- `resource-crud-redesign`
- `topology-orchestrator-upgrade`

That ledger creation is part of execution start, not optional project hygiene.
