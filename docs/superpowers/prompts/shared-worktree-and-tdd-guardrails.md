# Shared Worktree And TDD Guardrails

Use these rules for future stage-level worker sessions unless a prompt explicitly overrides them.

## Execution Bias

Stage-level worker prompts are implementation assignments, not open-ended design workshops.

- Do not re-run broad brainstorming or ask the user to choose between architecture options when the prompt already specifies the contract, scope, and constraints.
- Make reasonable implementation choices inside the requested scope and document them in the final report.
- Ask the user only when there is a real blocker that cannot be resolved from the prompt, existing docs, OpenAPI, or codebase.
- If you must ask, ask one concrete blocking question and include the recommended answer.
- Do not write a new spec before implementation unless the prompt explicitly asks for one.
- Do not delay implementation by presenting A/B/C方案 for decisions already made by the phase prompt.

## Worktree

- Use a dedicated git worktree for stage-level implementation by default.
- Do not implement large phase work directly in a dirty main worktree.
- Keep frontend and backend work isolated when they can proceed independently.

Exceptions:

- doc-only changes
- tiny one-file hotfixes

## TDD

Use test-first discipline for meaningful behavior changes.

Required cases:

- bugfixes with reproducible behavior
- API contract changes
- service/repository logic
- frontend interaction fixes
- view-model mapping changes

Expected loop:

1. add or update a failing test
2. implement the smallest fix
3. rerun the relevant tests
4. refactor only after passing

## Completion Standard

Do not report “done” without separating:

- relevant tests
- lint/build
- manual verification
- E2E verification, if applicable

If any layer was skipped, say so explicitly.
