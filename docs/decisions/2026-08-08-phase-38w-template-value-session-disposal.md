# Decision: Phase 38W Template Value Session Exit and Disposal

## Status

Accepted for Phase 38W Issue #5 (shared templates and responsive template
sessions). Applies to the worksheet-local Template Value Session on the query
workbench UI; backend authorization and template-execute routing are unchanged.

## Context

Loading a parameterized saved statement (personal or shared) enters template
mode: the worksheet shows typed parameter fields and routes Run and paging
through
`POST /query-targets/{id}/saved-statements/{statementId}/execute` with values
only. Parameter values live only in worksheet memory — never browser storage,
ordinary audit payloads, or history rows.

An earlier candidate path offered a standalone **Close template session**
control that cleared `templateStatementId` and values while leaving the loaded
placeholder SQL (for example `... :minimum_id`) in the editor. That path is
unsafe: it can restore ordinary
`POST /query-targets/{id}/execute` against placeholder text, or leave operators
with a mode exit that does not match a real SQL change. Historical Phase 38R
documentation must not be rewritten to invent or override this Phase 38W rule.

## Decision

1. **No standalone close-template-session UI.** There is no Close template
   session button, command, or equivalent control. Locale keys and props for
   that path must not exist.

2. **Template mode exits only through a real SQL edit.** That includes a manual
   statement change and a formatting operation that changes the SQL text. Exit
   clears parameter declarations, entered values, field errors, and
   `templateStatementId`, rotates stale-response protection (`requestId`), and
   only then restores ordinary Run via `POST /query-targets/{id}/execute`.

3. **“Template close” means closing the worksheet.** Closing a non-last
   worksheet destroys that worksheet’s in-memory template session. There is no
   second worksheet-state store and no persistence of values to local storage.

4. **Other disposal triggers discard values without requiring SQL edit:**
   - worksheet switch clears departing worksheet values (returning must not
     restore the prior entered values);
   - target switch creates or activates a clean non-template worksheet and must
     not restore the old values;
   - page refresh discards all worksheet memory;
   - sign-out discards all worksheet memory.

5. **Untouched loaded placeholder SQL must never reach ordinary execute.** While
   `templateStatementId` is set, Run and paging use only the saved-statement
   execute route. Placeholder text is not a valid ad hoc statement.

6. **Documentation scope.** This decision is recorded here as a tracked Phase
   38W ADR. Do not modify historical Phase 38R specs to define or override it.
   Untracked root Phase 38W WIP remains out of band for this candidate.

## Consequences

- Operators who want ordinary ad hoc execution after loading a template must
  intentionally edit (or format-change) the SQL; convenience of a one-click
  “close session” is rejected in favor of a clear security boundary.
- Frontend tests must prove SQL-edit exit, worksheet close/switch, target
  switch, refresh, and sign-out disposal — and must assert the absence of a
  close-session control.
- Shared-template runners (including non-admin actors) share the same session
  disposal contract as personal-template runners.
- Backend template-execute authorization remains the enforcement of who may run
  a given statement ID; this ADR only bounds browser-side session lifecycle and
  ordinary-execute routing after load.

## References

- Backend Issue #5: 38W-4 Govern shared templates and responsive template
  sessions (`Fanduzi/ControlHub-Backend#5`)
- Parent Phase 38W: Governed Parameterized Saved Templates
  (`Fanduzi/ControlHub-Backend#1`)
- Related product rule: ordinary audit and history must not store parameter
  values (Phase 38W parent specification; separate retention ADR when present
  on main).
- Frontend candidate branch proving the contract:
  `issue-5-38w4-20260807` (ControlHub frontend worktree)
