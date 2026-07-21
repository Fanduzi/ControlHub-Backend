# Advisor Plans

Generated on 2026-07-07 after reviewing ControlHub query workbench/admin credential UX against Bytebase's local implementation.

## Execution Order & Status

| Plan | Title | Priority | Effort | Depends on | Status |
|------|-------|----------|--------|------------|--------|
| 001 | Query workbench and credential admin UX realignment | P1 | L | — | SUPERSEDED — major scope landed across Phases 38H–38I.1 |
| 002 | Bounded result-grid copy affordances | P1 | S | — | DONE — merged frontend `f486722`, 2026-07-14 |
| 003 | Governed foreign-key record navigation backend contract | P1 | L | 002; accepted versioned spec and design | DONE — merged backend `30ee906`, 2026-07-14 |
| 004 | Complete trusted table preview and related-record UI | P1 | L | 003 | DONE — merged frontend `57dfaca`, 2026-07-14 |
| 005 | Governed Object Inspector metadata UI | P1 | M | 004; existing schema-detail API | TODO — Phase 38K Delivery A |

## Notes

- This is an advisory record only. No product code was changed.
- Plan 001 predates the merged Phase 38H/38I/38I.1 workbench implementation;
  keep it as historical input rather than an open execution checklist.
- Plan 002 delivered the bounded result-grid copy slice. Its completion record
  is retained in the plan file; it is not an open implementation checklist.
- Plan 003 delivered the governed backend contract (`POST /query-targets/{id}/related-records`), including parameter binding, row caps, and no-value persistence boundaries. Its closure record is retained in the plan file.
- Plan 004 delivered the accepted narrow Object Explorer preview/provenance/
  related-result UI on frontend `57dfaca`. Its completion record remains for
  traceability; it is not an open implementation checklist.
- Plan 005 is the first Phase 38K delivery. It renders the already governed
  columns, indexes, and foreign keys as a real Object Inspector without
  expanding the backend contract. Governed table-definition SQL is a separate
  follow-up because MySQL view definitions can disclose `DEFINER` identity.
- Status values: TODO | IN PROGRESS | DONE | BLOCKED | REJECTED | SUPERSEDED.

## Dependency Notes

- Plan 003 follows Plan 002 because the result grid must already support an
  explicit, accessible single-cell action.
- Plan 004 depends on Plan 003 because it must use the frozen, governed backend
  endpoint. It must not generate SQL from selected result values or broaden the
  backend API.
- Plan 005 follows Plan 004 so inspector entry points and mobile Object Explorer
  behavior are added to the settled workbench interaction model. A future table
  definition endpoint must follow Plan 005; it must not be smuggled into the
  metadata-only UI delivery.

## Findings Considered And Rejected

- Frontend SQL string interpolation for FK navigation: rejected. It would place
  result values in worksheet SQL and execution-history previews, and cannot be
  made safe by escaping alone.
- Copy-all, CSV export, and arbitrary result export: rejected for Phase 38J.
  They expand exfiltration ergonomics beyond the approved visible-value scope.
