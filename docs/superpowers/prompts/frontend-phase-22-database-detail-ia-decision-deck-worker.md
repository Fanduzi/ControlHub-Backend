# Frontend Phase 22 Worker Prompt — Database Detail IA Decision Deck

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 22 — Database Detail IA Decision Deck**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-30-phase-22-database-detail-ia-decision-deck.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-30-phase-22-database-detail-ia-decision-deck.md
```

The implementation plan is authoritative. Follow it task by task unless current
frontend code has a factual mismatch. If there is a mismatch, stop and report it
before inventing a different approach.

## Visual Direction

Use the approved brainstorm direction:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase22-db-detail-ia/content/database-detail-ia-options.html
```

Implement **A. 首屏决策台 / First-Screen Decision Deck**.

Do not implement B or C unless explicitly asked later.

## Branch And Worktree

Create a dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-22-database-detail-ia-decision-deck
```

Branch:

```text
feat/phase-22-database-detail-ia-decision-deck
```

Base it on current frontend `main`, after Phase 21 has been merged.

## Goal

Reduce database detail page scrolling by turning the top of the page into a
decision deck.

The first screen should show:

- database identity and operator verdict
- top diagnostic evidence
- top next checks
- expanded topology entry
- abnormal member shortcut for clusters

Long sections should remain available, but they should no longer all compete as
equally expanded cards above topology.

## Required Deliverables

Implement:

1. New decision deck component:

```text
components/resources/database-decision-deck.tsx
```

2. Resource detail page wiring:

```text
app/(console)/resources/[id]/page.tsx
```

3. Workbench dedupe / lower-page context:

```text
components/resources/database-operator-workbench.tsx
```

4. i18n:

```text
messages/en.json
messages/zh-CN.json
```

5. Tests:

```text
tests/components/database-decision-deck.test.tsx
tests/resource-detail-page.test.tsx
tests/components/database-operator-workbench.test.tsx
```

6. E2E:

```text
e2e/operator-database-workflow.spec.ts
```

## Product Requirements

### First-Screen Decision Deck

The deck must appear near the top of database detail pages.

It must contain:

- resource display name
- resource subtype/environment
- health and lifecycle badges
- operator verdict badge
- top evidence section
- next checks section
- topology analysis entry
- abnormal members section for clusters

### Evidence And Checks

Use existing Phase 21 helpers:

- `buildDiagnosticEvidence`
- `buildRunbookChecks`

Default first-screen summary must show:

- at most 3 evidence items
- at most 3 runbook checks

Do not remove extra evidence/checks from lower-page context.

### Topology Entry

Topology must be reachable near the top without long scrolling.

Use link:

```text
/resources/{id}?topologyDepth=2&topologyExpanded=1
```

Do not embed another full ReactFlow instance inside the decision deck. Use a
compact card/action. Keep existing full topology behavior available.

### Abnormal Member Shortcut

For database clusters:

- show abnormal members near the top
- abnormal means:
  - `healthStatus=critical`
  - `healthStatus=warning`
  - `healthStatus=unknown`
  - `lifecycleStatus=stopped`
  - `lifecycleStatus=degraded`
- include member display name
- include health/lifecycle badges
- include existing topology link:

```text
/resources/{memberId}?topologyDepth=2&topologyExpanded=1
```

Healthy members should not dominate this shortcut. The full member table remains
available lower on the page.

### Cluster vs Instance

Cluster detail:

- decision deck
- topology entry near top
- abnormal member shortcut
- full member table remains available
- audit context remains available
- relations/profile remain available

Instance detail:

- decision deck
- topology entry near top
- parent cluster and connection context remain available
- no cluster-only abnormal member shortcut

### Do Not Build Full Tabs

Do not split the whole page into tabs in Phase 22.

Allowed:

- compact cards
- summary sections
- lower-priority section reordering
- existing panels

Not allowed:

- full-page tabbed navigation
- URL-synced tabs
- hiding topology inside a tab

## Must Preserve From Previous Phases

Do not regress:

- no `健康=严重` or `生命周期=已停止` style copy
- no `Health=` or `Lifecycle=` patterns
- no English enum leaks in Chinese affected surfaces
- member sorting remains operational-priority based
- abnormal members keep topology links
- audit context still avoids causal claims
- topology full view still works
- interaction stability from Phase 18A/18B remains intact
- same-origin API proxy `/__api` remains intact

## Constraints

Do not:

- modify backend code
- change backend API contracts
- add SQL execution
- add work orders
- add write actions
- edit topology layout
- add topology node highlighting
- implement full-page tabs
- restore `/cmdb` navigation
- restore demo `resourceSummaries`
- add broad output suppression
- tag, push, release
- add AI co-author attribution

If backend data is missing, display an explicit unavailable state. Do not fake
metrics or causes.

## Required Commands

Run all:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
git status --short --branch
```

Manual browser verification on `3000` is required:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Verify:

```text
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Expected:

- `/resources/14`
  - decision deck visible near top
  - operator verdict visible near top
  - top evidence and next checks visible near top
  - topology entry visible near top without long scrolling
  - abnormal member shortcut visible near top
  - full member table still available
  - audit context still available
  - relations/profile still available
  - no duplicate wall of verdict/evidence/runbook immediately below deck
- `/resources/22`
  - instance decision deck visible near top
  - topology entry visible near top
  - parent cluster and connection context still available
  - no cluster-only abnormal member shortcut
- Global
  - no CORS errors
  - no unexpected browser console errors/warnings
  - API requests use `/__api`
  - no machine-style copy returns

## Commit Requirements

Commit all intended changes. Suggested commit messages:

```text
feat: add database detail decision deck
feat: place decision deck above database detail sections
refactor: reduce duplicated database workbench content
refactor: prioritize database detail sections
test: cover database decision deck workflow
```

Do not include:

```text
Co-Authored-By
Claude
Anthropic
AI
```

## Final Report Format

Return:

```markdown
## Phase 22 Final Report

### Worktree / Branch / Commits
| Item | Value |
|---|---|

### Files Changed
| File | Purpose |
|---|---|

### Layout Before / After
| Area | Before | After |
|---|---|---|

### Decision Deck
| Element | Behavior |
|---|---|
| Verdict | |
| Top evidence | |
| Next checks | |
| Topology entry | |
| Abnormal members | |

### Live Verification
| Page | Result |
|---|---|
| /resources/14 | |
| /resources/22 | |

### Verification
| Command | Result |
|---|---|

### Scope Confirmation
- No backend changes:
- No product write operations:
- No SQL execution:
- No work orders:
- No topology editing:
- No full-page tabs:
- No broad output suppression:
- No tag/push/release:
- No AI co-author:
- git status:
```

Do not claim completion until all required commands and live browser checks pass.

