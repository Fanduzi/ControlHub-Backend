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
- Treat project-local `.worktrees/` as an isolation directory, not source input.
- Tooling must not scan `.worktrees/**` from the main checkout. This includes TypeScript, Vitest, ESLint, Playwright, Next.js, and any build/test glob.
- If adding or changing frontend/backend tooling, explicitly preserve `.worktrees/**` excludes so the main checkout cannot pick up worktree source files, tests, or nested `node_modules`.

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

## Parallel Phase Coordination

Do not assume frontend and backend workers can communicate with each other during execution.

- Treat every worker prompt as self-sufficient.
- If a frontend phase depends on backend truth, the frontend prompt must say exactly which parts may proceed in parallel and which parts must wait for backend completion.
- If a backend phase is expected to unblock frontend work, the backend prompt must describe the contract that will be considered frozen for downstream work.
- Do not rely on "coordinate with the other worker", "sync with backend", "ask frontend", or similar instructions as the primary control mechanism.
- Put dependency and merge-order rules directly into each prompt.
- If parallel development is allowed, the prompt must explicitly say:
  - what can be implemented independently
  - what cannot be considered final until the other side lands
  - what must be revalidated after rebasing or merging the updated `main`
- If the final behavior depends on another repo's latest truth, the prompt must require a final live verification pass against that updated truth before claiming completion.

Recommended pattern for parallel phases:

1. allow each side to implement the independent subset
2. freeze the source-of-truth side first
3. require the dependent side to sync latest `main`
4. rerun full verification and live checks
5. only then allow completion claims

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

## Closeout Gate

Do not call stage-level work:

- complete
- done
- final
- ready to merge
- closeout complete

unless all of the following are true:

1. the implementation has been committed
2. `git status --short --branch` is clean
3. all required verification commands named in the prompt have been run
4. any required live/manual verification named in the prompt has been completed
5. the final report includes the exact commit hash

If any of the above is missing:

- do not write `Final Report`
- do not write `complete`
- do not write `done`
- do not write `ready to merge`
- instead write `Implementation Progress Report` and explicitly list what is still missing

Before writing a final report for a stage-level task, the worker must:

1. stage the intended files
2. commit them
3. run `git status --short --branch`
4. confirm the tree is clean
5. only then write the final report

A report without commit hash, clean tree, and required verification is not a closeout report. It is only a progress update.

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

When planning future phases, consult:

- `docs/superpowers/notes/2026-04-14-agent-friendly-integration-testing-roadmap.md`

Use it to decide when to introduce Testcontainers, Schemathesis, and Playwright API/E2E hardening.
