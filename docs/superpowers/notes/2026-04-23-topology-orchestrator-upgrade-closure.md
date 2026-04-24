# 2026-04-23 Topology Orchestrator-Style Upgrade Closure

This note records the audited closeout state for the topology orchestrator-style upgrade as of 2026-04-23.

## Outcome

- The topology upgrade is substantially implemented across backend and frontend.
- The initiative should not be treated as pending or still in design-only state.
- The initiative is not fully closed because several approved spec details were only partially implemented or implemented with different semantics.
- The correct final state for this round is **partial, implemented-with-spec-gaps**.

## What Shipped

- Backend topology nodes now carry richer profile metadata including hostname, IP, and port.
- Backend topology responses include per-node problems and top-level problem summaries.
- Frontend topology nodes render in a richer orchestrator-style card format.
- The topology panel includes a collapsible problems panel.
- Clicking nodes opens a detail popup with key node metadata.
- Topology mapping and rendering logic were upgraded together with tests.

## Evidence

Backend evidence:

- `internal/model/topology.go`
- `internal/service/topology_service.go`
- `internal/service/topology_service_test.go`
- `internal/integration/topology_test.go`

Frontend evidence:

- `/Users/fan/JsProjects/ControlHub/types/resource.ts`
- `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx`
- `/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts`
- `/Users/fan/JsProjects/ControlHub/tests/topology-panel.test.tsx`
- `/Users/fan/JsProjects/ControlHub/tests/topology-mapper-semantic.test.ts`

Representative backend commits from this workstream:

- `3a2e853` — add topology response semantic roles and group keys
- `a037bec` — use seeded topology semantics in integration coverage
- `51a410a` — enrich topology nodes with resource profile metadata
- `c9c1e92` — add topology problem summaries and richer frontend topology panel

## What Did Not Fully Close

- `degraded` health was specified as a warning-condition peer to `warning`, but the audited backend detection does not explicitly implement it.
- The spec included a `no_replica` topology warning; this was not found in the audited implementation.
- Zone/datacenter grouping is visually approximated, but not fully implemented according to the spec’s data semantics.
- Popup and problem-message behavior is close to the approved design, but not exact in component semantics and i18n treatment.

## Why Partial Closure Is Acceptable

- The topology panel upgrade is already materially shipped and usable.
- Remaining work is limited to spec-completion and semantic cleanup, not core feature construction.
- Marking the initiative partial preserves execution truth and avoids fake completion.

## Rollback / Fallback

- Keep the current topology implementation as the baseline.
- If the spec gaps matter, address them in a narrow closure pass rather than reverting the current upgrade.
- No database or schema rollback is relevant to the remaining topology work.

## Follow-Up

A short follow-up pass should decide and execute one of two paths:

- **Finish to spec:** add `degraded` + `no_replica`, align zone grouping semantics, and tighten popup/i18n parity.
- **Accept current implementation:** explicitly downgrade those spec items and close the initiative as delivered behavior.

## Final Status

- Final status: `partial`
- Recommended label: `implemented-with-spec-gaps`
