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
- Starting with the next stage after this rule was added, stage-level worktrees must be created under the project-local `.worktrees` directory.
- Backend worktrees must live under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Frontend worktrees must live under `/Users/fan/JsProjects/ControlHub/.worktrees`.
- Do not create new stage-level worktrees under `.claude/worktrees`.
- Do not implement large phase work directly in a dirty main worktree.
- Keep frontend and backend work isolated when they can proceed independently.

Exceptions:

- doc-only changes
- tiny one-file hotfixes

## Startup Check

Before changing files, verify and report:

- current working directory
- git branch
- recent commits, enough to prove the expected base is present
- `git status --short`
- expected worktree path

Stop and ask only if the branch/path/base commit is wrong or the worktree contains unrelated dirty changes.

## Scope Control

For narrow tasks, obey explicit file or directory allowlists in the prompt.

- Do not modify files outside the allowlist.
- If the task genuinely requires an out-of-scope file, stop and report the reason before editing it.
- Do not modify generated files, release docs, package metadata, or broad config unless the prompt explicitly allows it.
- Do not execute later tasks or adjacent phases early.

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

Tests should assert specific facts, not just broad behavior.

- API tests should assert status codes, response shapes, and key fields.
- UI tests should assert visible state, error state, and request payloads where practical.
- Read-model tests should assert ordering, de-duplication, filters, and edge cases.
- Bugfix tests should fail for the original bug and pass after the fix.

## Pre-Commit Scope Check

Before committing:

- stage explicit files only
- run `git diff --cached --stat`
- run `git diff --check --cached`
- report the staged file list
- verify staged files match the requested scope

Do not commit unrelated local files, IDE files, temporary backups, logs, or generated artifacts unless explicitly required.

If GitNexus is available in the repo, also run the configured change-impact check before committing.

## Completion Standard

Do not report “done” without separating:

- relevant tests
- lint/build
- manual verification
- E2E verification, if applicable

If any layer was skipped, say so explicitly.

Final reports must include negative scope confirmation for important boundaries, for example:

- did not change production code for characterization-only tasks
- did not add topology editing when building read-only topology
- did not add SQL work orders or query execution during CMDB phases
- did not add frontend-only mocks as final behavior
- did not tag, push, release, or add AI co-author unless explicitly requested

For stage-level work, include a short "next phase input" section:

- contract gaps found
- model limitations found
- test or E2E gaps remaining
- recommended next action
