# Query Workbench Preview Issues Status — 2026-07-07

## Purpose

This note is the single tracking index for the manual-preview issues discussed
around the Query Workbench and query credential administration UI.

The original numbered 1-7 preview findings are recorded in:

- `docs/superpowers/notes/2026-06-30-query-workbench-preview-findings.md`

Related design and execution references:

- `advisor-plans/001-query-workbench-bytebase-ux-realignment.md`
- `docs/superpowers/specs/2026-07-07-phase-38e-query-workbench-ia-and-admin-feedback.md`
- `docs/superpowers/plans/2026-07-07-phase-38e-query-workbench-ia-and-admin-feedback.md`
- `docs/superpowers/prompts/frontend-phase-38e-query-workbench-ia-admin-feedback-worker.md`

## Status Matrix

| # | Preview issue | Current status | Phase ownership |
| - | ------------- | -------------- | --------------- |
| 1 | Query target selector is too weak: cramped dropdown, truncated text, no search, filters visually separated from target identity. | Closed in Phase 38G frontend worktree: `/query` now uses a grouped/searchable connection navigator with filters in the connection surface and a persistent active-connection summary. Real backend-backed E2E and manual browser QA selected `Local MySQL Query Dev` through the navigator. | Fixed in Phase 38G |
| 2 | Credential settings detail panel had ICU formatting errors around literal `{ref}` text. | Fixed in Phase 38C frontend. Phase 38E keeps regression coverage and rewrites the credential terminology so refs are understandable. | Fixed; Phase 38E preserves |
| 3 | `SHOW TABLES` was blocked because the backend guard was SELECT-only. | Fixed in Phase 38C for parser-backed read-only metadata statements. Phase 38D later allowed `SHOW DATABASES` and cross-schema metadata statements. | Fixed in backend Phase 38C/38D |
| 4 | Governance and access panel is too large for its value. | Closed in Phase 38G frontend worktree: governance is blocker-first with compact badges, and placeholder education/action clutter is removed from the primary workbench surface. | Fixed in Phase 38G |
| 5 | Target facts are duplicated and occupy too much space: engine, environment, host, owner, language, readiness, cluster. | Closed in Phase 38G frontend worktree: active target identity lives in the connection navigator, while schema/governance panels avoid repeating the full target fact stack. | Fixed in Phase 38G |
| 6 | Query governance panel hydration mismatch from admin-link rendering. | Fixed in Phase 38C/38D by making admin role resolution hydration-safe and presentation-only. Phase 38E keeps the invariant: no render-time `window` or `sessionStorage` access. | Fixed; Phase 38E preserves |
| 7 | Worksheet editor is not IDE-like: no SQL highlighting, no formatting, no multiple worksheets. | Closed by Phase 38F/38G frontend work: Phase 38F added CodeMirror SQL editor, formatting, shortcuts, and multiple worksheets; Phase 38G added readable dark/high-contrast styling plus locally persisted editor height resizing. | Fixed in Phase 38F/38G |

## Later Preview Additions

| Issue | Current status | Phase ownership |
| ----- | -------------- | --------------- |
| `SHOW DATABASES` still could not execute. | Fixed in backend Phase 38D (`feat: allow readonly database metadata statements`). | Fixed in backend Phase 38D |
| Directly opening `/settings/query-credentials` as an admin showed "managed by administrators". | Fixed in frontend Phase 38D through admin role recovery from the encoded auth token/cookie, as a UI hint only; backend authorization remains authoritative. | Fixed in frontend Phase 38D |
| `/settings` did not expose a query credential settings entry. | Fixed in frontend Phase 38D. | Fixed in frontend Phase 38D |
| Clicking "edit credential metadata" looked like it did nothing. | Captured as Phase 38E F0: rename to save semantics, show success feedback, refresh the operations table row, preserve stale-target guards. | Phase 38E |
| Credential admin UI did not explain `LOCAL_QUERY_RO`, server secret refs, or where real username/password/DSN live. | Captured as Phase 38E F0b: explain that `LOCAL_QUERY_RO` is a server-side secret reference resolving to `CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO`; real DSN, database username, and password live in the backend runtime environment, not in browser state. | Phase 38E |

## Phase 38G Closure Update — 2026-07-09

Phase 38F delivered the SQL editor foundation, but manual preview still found
real-usability gaps around editor readability, target navigation, credential
detail placement, and placeholder action clutter. Phase 38G owns those frontend
usability items:

- dark/high-contrast CodeMirror readability and locally persisted editor height;
- grouped/searchable connection navigator replacing the cramped target picker;
- sticky query credential settings inspector with selected-row highlighting;
- primary worksheet toolbar reduced to implemented actions only;
- blocker-first governance and fewer duplicated target facts.

Phase 38G verification used the real backend on `:8080`, the same-origin E2E
proxy on `:8081`, the frontend dev server on `:3100`, and the dedicated query
MySQL fixture. Focused Query Workbench and Query Credential Settings E2E passed
against the real stack; manual browser QA confirmed `/query` did not render raw
DSN/password values or inline credential edit controls, and
`/settings/query-credentials` rendered credential metadata only.

## What Phase 38E Should Close

Phase 38E is expected to close:

- issue 1 target navigation usability;
- issue 4 governance panel visual weight;
- issue 5 duplicated target facts;
- credential save feedback;
- credential terminology and secret-location clarity;
- regression coverage for already-fixed ICU and hydration issues.

Phase 38E is not expected to close:

- issue 7 SQL editor foundation work: syntax highlighting, formatting, multiple
  worksheets, worksheet naming, or persisted drafts;
- backend SQL guard changes, because read-only metadata support already landed
  in Phase 38C/38D;
- any secret write API or secret-manager UI.

## Next Recommended Phase After 38E

If Phase 38E passes review and real E2E, the next highest-value work is a
separate SQL editor foundation phase:

- Monaco or CodeMirror SQL editor;
- read-only SQL formatting;
- keyboard shortcuts such as Cmd/Ctrl+Enter;
- multiple local worksheets with per-worksheet target, statement, result, and
  history state;
- clear target context per worksheet to avoid running stale SQL against the
  wrong target.
