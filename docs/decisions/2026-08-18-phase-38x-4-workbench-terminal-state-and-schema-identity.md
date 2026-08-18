# Decision: Query Workbench Terminal State and Schema Metadata Identity

## Status

Accepted for Phase 38X-4 Issue #10. This decision governs current-generation
Saved Sheets requests and schema completion metadata in the Console Query
Workbench; backend saved-statement authorization and query execution are
unchanged.

## Context

Saved Sheets list failures can leave the interface loading indefinitely, and
delete failures are not visible. Schema completion metadata is held at the
workbench-shell level, so a target or database transition can briefly expose
suggestions from the prior context. Default database selection also causes a
second database-list request even though the first response already contains
the default and the available databases.

## Decision

1. Every current Saved Sheets request settles visibly as authorized content,
   an empty result, or a controlled error. A superseded request is ignored.
2. A forbidden or missing Saved Sheets list is a non-retryable controlled
   terminal state. Network and backend failures are retryable. Errors are
   announced inline without moving focus automatically.
3. During same-target search, pagination, or retry, the current list may remain
   visible but cannot be acted on. A target change clears it immediately.
4. Delete confirmation remains open while deletion is pending. Dismissal and
   duplicate submission are disabled until the request settles; failure is
   announced in the dialog and then permits retry or cancel.
5. A delete returning not found closes the dialog, refreshes the current list,
   and announces that the item no longer exists without claiming that this
   request deleted it. A forbidden delete remains visible in the dialog as a
   non-retryable error and permits cancel only.
6. A target change invalidates the Saved Sheets request generation, clears the
   list, search text, debounced search, pagination, and every Saved Sheets
   dialog. Responses from the prior target cannot change the new target's
   interface.
7. Schema metadata has one active Schema Metadata Identity:
   `(targetResourceId, database)`. Worksheets sharing that identity may share
   metadata. A target or database change immediately clears prior metadata;
   the workbench does not retain a multi-target metadata cache.
8. One database-list response owns both available-database completion metadata
   and default database selection. Selecting the returned default must not
   trigger a duplicate database-list request.
9. When no default database is returned, the workbench does not select one by
   inference. Database-name completion remains available, but object metadata
   waits for an explicit database selection.
10. Schema metadata failure leaves the editor usable with keyword-only
    completion and presents a non-blocking, accessible retry surface for the
    current identity. Prior-identity metadata is never used as fallback.
11. During a same-target request, the prior Saved Sheets list may remain visible
    but disabled. If the request fails, that list is hidden and the controlled
    error becomes the terminal state.
12. Search remains available during same-target loading so a new search can
    supersede the request. Create and row mutations are disabled until the
    current list is ready. In an error terminal state, only Retry for a
    retryable error is actionable.
13. The metadata warning is adjacent to editor/completion controls, is
    announced without stealing focus, and does not disable Run. Retry reloads
    database and object metadata together for the current identity rather than
    exposing a partial metadata state.
14. Deleting the final item on a page after the first automatically loads the
    preceding page and announces the result.
15. At 375px, Saved Sheets search occupies its own row and creation controls
    use a wrapping second row without horizontal scrolling. Desktop English,
    mobile English, and desktop zh-CN are release verification surfaces.

## Consequences

- Same-identity worksheets can avoid duplicate metadata work without allowing
  suggestions to cross a protected target or database boundary.
- A transition may temporarily offer keyword-only completion while metadata
  for the new identity loads; it never falls back to prior-identity objects.
- The interface distinguishes non-retryable authorization/existence outcomes
  from retryable transport/backend failures without changing backend status or
  authorization contracts.
- No browser persistence, new cache service, or background prefetch layer is
  introduced.

## References

- Backend Issue #10: 38X-4 Make Query Workbench failures terminal and schema
  metadata isolated (`Fanduzi/ControlHub-Backend#10`)
