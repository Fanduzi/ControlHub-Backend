# Decision: Phase 38K Renders Existing Governed Metadata Before Adding DDL

## Status

Accepted 2026-07-14. This decision implements
`docs/superpowers/specs/2026-07-14-phase-38k-governed-object-inspector-metadata.md`.

## Context

The query schema service already owns authorization, inspection, caching,
auditing, and controlled error mapping for object metadata. Its object-detail
contract contains the information an operator needs for a table or view:
columns, index definitions, foreign-key definitions, and truncation flags.

The frontend loads this contract lazily but reduces it to counts. Making it
readable is a presentation problem, not a reason to add a database endpoint.
Combining it with raw DDL would widen the contract and introduce engine-specific
disclosure issues into a frontend-only delivery.

## Decision

### Reuse Existing Object Detail

Phase 38K Delivery A consumes the `ObjectDetailResponse` already loaded by
`QueryObjectExplorer`. Opening Inspector is a local state transition: no second
`getObjectDetails` call, query execution, preview request, navigation request,
or persistence.

Selection state contains only object identity, ready detail, and a local trigger
reference. It clears on target changes, object collapse/reload, or non-ready
detail. Existing generation and abort guards prevent late target-A responses
from appearing for target B.

### Inspector Is Read Only And Transient

Inspector is an explicit button on ready detail. It uses existing accessible UI
primitives as a transient panel or Sheet, never a workbench tab, output mode,
or third fixed desktop column.

It renders, in API order:

1. Columns: position, name, type, nullability, primary key, auto increment.
2. Indexes: name, constituent columns, unique, primary.
3. Foreign keys: name, local/referenced columns, referenced database/object,
   update/delete rules.

Empty arrays are valid metadata. The UI handles each matching truncation flag
as incomplete data and does not re-sort composite keys.

### Explicit Inspect, Not Right Click

A native Inspect button works with mouse, keyboard, touch, and mobile Sheets.
A right-click-only entry point would make the primary action inaccessible on
touch and ambiguous for keyboard users. A later context menu may call this same
action, but cannot create a separate data or authorization path.

### Definition SQL Is A Separate Backend Decision

Delivery A does not issue `SHOW CREATE`. MySQL `SHOW CREATE VIEW` can include a
`DEFINER`, while definition text introduces formatting, payload, caching, and
audit policy concerns. Delivery B must start with a separate table-only backend
contract that decides normalization/redaction, response cap, audit identity,
controlled errors, and fixture-backed integration coverage.

The browser must never query a database or construct definition SQL from object
names. It may call a future typed endpoint only after OpenAPI approval.

## Consequences

- Users gain readable schema metadata with no additional backend request when
  Inspector opens.
- Existing cache, audit, target governance, result-data, and credential
  boundaries remain unchanged.
- Delivery A works against current normalized MySQL/TiDB metadata and does not
  block future engines that implement the same contract.
- Raw table definitions and context menus remain deliberately absent rather
  than appearing as misleading placeholders.

## Rejected Alternatives

### Browser Information Schema Queries

Rejected because they require browser database access, bypass target governance,
and duplicate the backend's read-only and audit controls.

### Add Raw DDL To Existing Object Detail

Rejected because it mixes engine-specific definition text with normalized,
bounded metadata and can disclose a view `DEFINER`. It needs its own reviewable
contract.

### Render Only Count Tooltips

Rejected because counts cannot expose type detail, ordered composite keys, or
completeness state. A real Inspector is needed for the requested workflow.

## Verification Requirements

- Component tests prove Inspector uses loaded detail and adds no network call.
- Tests cover all sections, empty/truncated states, table/view behavior, stale
  object state, Escape, and focus return.
- E2E covers desktop, 375px mobile, EN/ZH, dark theme, and zero execute/preview/
  related-record request when opening Inspector.
- Existing result preview, Copy, and related-record flows remain covered.
- Final frontend candidate runs typecheck, lint, unit suite, build, E2E
  preflight/governance, and focused real E2E with zero failed or skipped tests.
