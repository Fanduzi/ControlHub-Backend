# Phase 38X-4: Query Workbench Terminal State and Schema Isolation

## Problem Statement

Query Workbench requests do not always reach an honest visible terminal state.
Saved Sheets list requests can remain loading after access is revoked or a
target disappears, and delete failures are swallowed. During target and
database transitions, shell-level completion metadata can briefly offer
objects from the prior Schema Metadata Identity. Default database selection
also repeats a database-list request whose first response already contains the
required default and database names.

These failures make the workbench appear stuck, hide mutation outcomes, and
risk presenting stale protected metadata from the wrong query context.

## Solution

Give every current Saved Sheets request a visible Workbench Request Terminal
State: authorized content, an empty result, or a controlled error. Superseded
generations never update the interface. Deletion gains explicit pending,
success, no-longer-present, forbidden, and retryable-failure behavior.

Own schema completion metadata by one active Schema Metadata Identity,
`(targetResourceId, database)`. Worksheets may share metadata only while that
identity is unchanged. Changing target or database clears prior metadata
immediately. One database-list response owns both default database selection
and database-name completions, eliminating the duplicate request. Metadata
failure is visible and retryable but leaves manual SQL execution available with
keyword-only completion.

## User Stories

1. As a query runner, I want Saved Sheets loading to settle visibly, so that I
   never wait indefinitely after a request has already failed.
2. As a query runner, I want an authorized Saved Sheets list to show its
   current content, so that I can load the statements available to me.
3. As a query runner, I want an authorized empty list to show an explicit empty
   state, so that I do not confuse no statements with a loading failure.
4. As a query runner whose access was revoked, I want a controlled forbidden
   state, so that the interface does not keep spinning or expose raw backend
   details.
5. As a query runner whose target was removed, I want a controlled unavailable
   state, so that I understand the current Saved Sheets context no longer
   exists.
6. As a query runner experiencing a transient network or backend failure, I
   want a Retry action, so that I can recover without reloading the workbench.
7. As a query runner facing a forbidden or missing context, I do not want a
   meaningless Retry action, so that the interface does not promise recovery
   from a non-transient outcome.
8. As a query runner, I want stale list responses ignored after a newer search,
   page, or target request begins, so that old data cannot overwrite current
   intent.
9. As a query runner, I want my current same-target list to remain visible
   during refresh, so that loading does not cause unnecessary visual flicker.
10. As a query runner, I want the retained list disabled while refreshing, so
    that I cannot mutate data whose current status is unconfirmed.
11. As a query runner, I want a failed refresh to replace the retained list
    with its controlled error, so that stale content is not presented as
    current.
12. As a query runner, I want search to remain usable during same-target
    loading, so that a newer search can supersede an obsolete request.
13. As a query runner, I want target changes to clear Saved Sheets search,
    pagination, list data, and dialogs, so that old-target context never appears
    under the new target.
14. As a query runner deleting a Saved Sheet, I want the confirmation to show
    pending state and reject duplicate submission, so that one action cannot
    produce multiple delete requests.
15. As a query runner, I want a pending delete dialog to remain stable until the
    request settles, so that its outcome is not silently lost.
16. As a query runner whose delete is forbidden, I want a non-retryable error in
    the dialog and a Cancel action, so that the permission failure is explicit.
17. As a query runner deleting an item that is already absent, I want the list
    refreshed and an announcement that it no longer exists, so that the UI
    converges without falsely claiming this request deleted it.
18. As a query runner encountering a transient delete failure, I want to retry
    or cancel from the same dialog, so that the action remains recoverable.
19. As a query runner deleting the last item on a later page, I want to return
    automatically to the preceding page, so that I am not left on an invalid
    empty page.
20. As a screen-reader user, I want loading, errors, no-longer-present outcomes,
    and metadata warnings announced without forced focus movement, so that the
    state change is perceivable without disrupting navigation.
21. As a query runner switching targets, I want prior database, table, view, and
    column suggestions removed immediately, so that protected metadata never
    crosses target boundaries.
22. As a query runner switching databases, I want prior object and column
    suggestions removed immediately, so that completion matches the selected
    database.
23. As a query runner using multiple worksheets on the same target and
    database, I want them to share completion metadata, so that equivalent
    contexts do not repeat work.
24. As a query runner, I do not want metadata retained for multiple targets in
    browser memory, so that switching away ends access to the prior identity's
    suggestions.
25. As a query runner, I want the first database-list response to populate both
    database completions and the server-selected default, so that opening a
    worksheet does not repeat the request.
26. As a query runner when the server provides no default database, I want the
    workbench to wait for my explicit choice, so that the frontend does not
    guess execution context.
27. As a query runner, I want database-name completion even when no default is
    selected, so that I can discover and choose an available database.
28. As a query runner whose metadata request fails, I want a visible retryable
    warning while the editor remains usable, so that completion failure does
    not block manual SQL.
29. As a query runner retrying metadata, I want database and object metadata
    reloaded together for the current identity, so that the workbench does not
    expose a partial mixed state.
30. As a query runner, I want keyword completion to remain available when
    schema metadata is unavailable, so that basic editing still works.
31. As a mobile user at 375px, I want Search and creation controls to fit
    without horizontal scrolling, so that every action remains reachable.
32. As an English or zh-CN user, I want terminal states and controls localized
    without clipping, so that the behavior is usable in both supported locales.
33. As an administrator, I want existing Saved Sheet authorization rules to
    remain server-owned, so that frontend state improvements cannot widen
    access.
34. As a security reviewer, I want controlled errors and announcements to omit
    statement text, credentials, DSNs, raw server failures, and prior-target
    metadata, so that error handling is not a leakage channel.

## Implementation Decisions

- `Workbench Request Terminal State` and `Schema Metadata Identity` are the
  canonical domain terms defined in the root glossary.
- The accepted Query Workbench terminal-state and schema-identity decision is
  authoritative for this delivery.
- Saved Sheets list state distinguishes idle, loading, ready, and controlled
  error outcomes. Error state retains a stable category sufficient to decide
  whether Retry is available; raw server messages are not rendered.
- Saved-statement service errors continue to map backend statuses into stable
  controlled categories. Forbidden and not-found list outcomes must no longer
  fall through without settling component state.
- Every list request belongs to a generation identified by target, search, and
  page intent. Starting a newer request invalidates the older generation;
  aborting transport is an optimization, not the stale-response guarantee.
- Same-target loading may retain the prior list for continuity, but creation
  and row mutations are disabled. Search remains available to create a newer
  generation. A failed request hides retained rows.
- Target change resets list state, search and debounced search, pagination,
  create/edit/delete dialogs, mutation errors, and pending state before the new
  target can render.
- Forbidden and missing list outcomes are non-retryable. Network and backend
  failures are retryable. Existing global unauthenticated-session handling is
  unchanged.
- Delete state is explicit and scoped to the selected Saved Sheet. Pending
  prevents dismissal and duplicate submission. Transient failure restores
  Retry and Cancel; forbidden restores Cancel only.
- Delete not-found is not presented as successful deletion. It closes the
  dialog, refreshes the current generation, and announces that the item is no
  longer present.
- After deletion, an empty page greater than one reloads the preceding page.
  It does not jump to the first page.
- Controlled status and error messages are localized and announced inline.
  Errors use alert semantics; non-error progress and reconciliation messages
  use polite status semantics. Neither steals focus automatically.
- Search occupies its own row at 375px. Creation controls occupy a wrapping
  second row and must not cause horizontal overflow. Desktop behavior keeps
  the existing direct controls; no overflow-menu abstraction is introduced.
- Schema completion state has one active identity consisting exactly of query
  target ID and database. It is not keyed only by target and is not retained as
  a multi-target cache. Worksheet ID is not part of the identity.
- Worksheets sharing the active identity may share database, object, and
  loaded-column metadata. Switching target or database clears all metadata
  before requesting the new identity.
- Stale schema responses are rejected by exact identity/generation comparison
  even if transport abort arrives too late.
- One database-list request supplies available database names and
  `defaultDatabase`. Applying that default must not trigger another
  database-list request.
- A null default remains null. The frontend never selects the first database by
  inference. Database-name completion may be populated while object metadata
  waits for explicit selection.
- Database-list or object-list failure clears schema-derived completions for
  the identity, retains keyword-only completion, and exposes one non-blocking
  retry surface adjacent to editor/completion controls. Run remains enabled
  according to existing execution rules.
- Metadata Retry reloads database and object metadata as one current-identity
  operation. If no database is selected, it reloads only the database list and
  waits for explicit selection before object loading.
- No browser persistence, new cache service, background prefetch system, API
  endpoint, backend schema, public status, or authorization rule is added.
- Existing abort and generation protections are preserved or strengthened;
  no request result is accepted solely because it completed last in wall-clock
  time.
- This is primarily a frontend delivery. Backend changes are permitted only if
  fresh evidence proves the published API contract cannot satisfy the spec;
  such a finding must be surfaced rather than assumed.

## Testing Decisions

- Tests assert visible state, request counts, accepted identity, accessibility,
  and mutation outcomes rather than internal hook arrangement.
- The highest Saved Sheets seam is the existing `QuerySavedStatements`
  component with the saved-statement service mocked. Deferred promises drive
  deterministic generations without wall-clock sleeps.
- Saved Sheets component tests cover list success, empty, forbidden, missing,
  transient error and Retry; same-target retained loading; error replacement;
  target reset; stale response rejection; dialog reset; delete pending,
  duplicate prevention, forbidden, missing, transient retry, and previous-page
  fallback.
- Accessibility tests assert alert/status semantics, actionable controls,
  disabled states, and focus restoration. They assert controlled localized
  text rather than raw backend messages.
- The highest schema seam is the existing `QueryEditorShell` component with
  the schema service mocked. Deferred database/object responses prove exact
  identity and generation behavior.
- Schema component tests cover target and database transitions, immediate
  completion clearing, stale response rejection, same-identity worksheet
  sharing, null default behavior, one database-list request, metadata warning,
  retry, keyword-only degradation, and unchanged Run availability.
- Service tests are added only when status mapping or AbortSignal forwarding
  changes. They do not duplicate component state-machine assertions.
- Real Chromium tests use the existing Query Workbench suite and real backend
  fixtures. Route mocks, `page.evaluate` requests, forced clicks, skips, and
  fixmes are prohibited.
- Real Chromium covers desktop English, 375px English, and desktop zh-CN;
  verifies no horizontal overflow, accessible labels/announcements, real Saved
  Sheets behavior, and exactly one database-list request during default
  selection.
- Controlled 403/404 and timing races are proven at the deterministic component
  seams rather than fabricated in browser E2E.
- Existing service and component tests remain authoritative for request field
  safety: no actor, role, credential, DSN, or protected server state is added
  to browser requests.
- Release verification runs runtime checks, TypeScript, lint, unit/component
  tests, build, E2E governance/preflight, focused real Chromium, and the full
  release E2E suite with zero unaccounted failures or skips.
- Responsive verification checks actual viewport bounds at 375px rather than
  relying only on CSS class snapshots.

## Out of Scope

- Changing saved-statement ownership, scope, or authorization rules.
- Changing backend 401, 403, 404, validation, or execution response contracts.
- Browser-side SQL parsing, query execution, result manipulation, or metadata
  authorization shortcuts.
- Persisting schema completion metadata in localStorage, sessionStorage,
  IndexedDB, cookies, or a service worker.
- Retaining a cache for multiple query targets or databases after identity
  transitions.
- Adding a global toast framework, state-management library, request-cache
  library, or new backend endpoint.
- Redesigning create/edit forms, template parameter semantics, worksheet
  persistence, result grids, object explorer, inspector, relationship map, or
  execution governance.
- Optimistic Saved Sheet deletion and rollback.
- Automatically selecting a database when the backend returns no default.
- Hiding errors solely to preserve the previous list or metadata experience.

## Further Notes

- The existing Saved Sheets component already has an abort controller and
  generation counter; the defect is incomplete terminal-state handling, not a
  missing concurrency abstraction.
- The current delete path catches and discards controlled errors and has no
  pending state.
- The Query Workbench shell currently issues one database-list request to find
  the default and another to populate completion metadata. The response already
  contains both pieces of information.
- Schema completion collections are currently shell-global. Returning early
  when no active database is selected does not clear them, which is why
  explicit identity invalidation is required.
- The implementation should prefer one active metadata state over a generalized
  cache. A cache can be reconsidered only if measured latency justifies the
  additional authorization and invalidation complexity.
- The accepted Phase 38X-4 Query Workbench terminal-state and schema-identity
  decision is the source for the behavioral boundaries in this specification.
