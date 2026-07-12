# Historical Worker Prompt — Phase 38I Completion Package (Closed)

> **Calibrated status:** This worker prompt was executed. Phase 38I is complete
> on backend `main` (`24272b8`) and frontend `main` (`d1efc4d`). Do **not** run
> this prompt as an active ultrawork implementation job. Retain it as:
>
> - historical execution instructions for the completion package;
> - boundary reference (hard non-goals, no-secret rules);
> - seed material for a **new** Phase 38J+ worker prompt that covers only
>   accepted deferred work.

**Do not** recreate `phase-38i-schema-intelligence` worktrees to “finish” this
prompt. **Do not** implement product code from this file.

---

## Completion Pointers

```text
Evidence:
  docs/superpowers/notes/2026-07-12-phase-38i-schema-intelligence-remediation-evidence.md
  docs/superpowers/notes/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete-evidence.md

Calibrated product review:
  docs/superpowers/notes/2026-07-11-phase-38i-query-platform-product-design-review.md

Calibrated spec / plan:
  docs/superpowers/specs/2026-07-11-phase-38i-completion-query-workbench-productization.md
  docs/superpowers/plans/2026-07-11-phase-38i-completion-query-workbench-productization.md
```

### What this worker completed (Phase 38I)

- Object-detail database context (backend invariant + frontend adapter).
- Shared worksheet schema metadata and real schema-aware completion.
- Atomic worksheet target/database context; connection select → new worksheet.
- Workbench IA consolidation; mobile navigation and accessibility baseline.
- Credential administration scope honesty and modal simplification.
- Full gates + real E2E (41 passed, 0 failed, 0 skipped at `d1efc4d`).

### Accepted deferred work (Phase 38J+ — needs a new prompt)

- Result-grid copy/navigation under masking policy.
- Foreign-key record navigation.
- Backend-normalized Visual Explain.
- Global credential coverage/facets API.
- ER diagram; saved queries / governed collaboration.
- Additional schema inspector engines.

### Rejected / out of scope (still rejected)

- Browser DB connections; secrets/DSN/password/username in browser.
- SQL guard widening; new query engine; Monaco; editable grids.
- Credential secret write API; credential edit on `/query`.
- Ungoverned Tabularis clones (AI/MCP/notebooks/query builder/detachable windows).

---

## Original Worker Text (archival)

The following text is the original execution prompt, preserved for audit. All
“complete Phase 38I” imperatives below are **historical**. Active agents must
not treat open checklists or “not finishing-flow eligible” language as current
repository state.

```text
ultrawork

Working directories (historical):

- Backend implementation worktree:
  /Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-38i-schema-intelligence
- Backend branch: phase-38i-schema-intelligence
- Frontend implementation worktree:
  /Users/fan/JsProjects/ControlHub/.worktrees/phase-38i-schema-intelligence
- Frontend branch: phase-38i-schema-intelligence
- Backend docs root:
  /Users/fan/GolangProjects/ControlHub
- Tabularis reference repo:
  /Users/fan/JsProjects/tabularis

Objective (historical):

Complete Phase 38I and productize the Query Workbench. Fix real object-detail
failure, wire real schema-aware completion, make worksheet target/database
context safe, consolidate workbench IA, repair mobile/accessibility, simplify
credential administration, and close P1/P2 findings before returning.

Baseline heads at authoring: backend abf74fe, frontend 9fec6c0.

Hard boundaries (still controlling for any future query work):

- no SQL guard change;
- no new query engine;
- no browser database connection;
- no DSN/password/database username/secret value in browser state, request,
  response, display, cache key, audit, error, or log;
- no actorUserId request field;
- no credential secret write API;
- no credential edit controls on /query;
- no schema persistence migration or browser persistence;
- no saved query, export, approval, JIT, Visual Explain, ER, notebook, AI, MCP,
  query builder, editable grid, connection profile, split window, or Monaco
  implementation inside a 38I-shaped package;
- no global credential aggregate API faked client-side;
- no AI co-author trailer.

Required outcomes: see calibrated spec. Final real E2E proved the closed
contract against the dedicated query MySQL fixture.

Final status after execution: ready for human review; finishing evidence
committed; merged to main. Tag/release/deploy were never part of this worker.
```

---

## Guidance For Phase 38J+ Workers

1. Read remediation evidence and the deferred list in the calibrated completion
   spec—not this archival ultrawork body as an open ticket.
2. Write a new focused spec/plan/prompt that names only 38J+ scope.
3. Preserve hard boundaries above unless a dedicated design explicitly changes
   them.
4. Keep backend, frontend, and docs commits separate; no AI co-author trailer.
5. Do not regress Phase 38I worksheet safety, schema adapter, scope honesty, or
   no-secret invariants.
