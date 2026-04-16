# Frontend Phase 14B: Database Topology Semantic UX

You are implementing a focused frontend topology phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

This phase exists because the current topology still behaves like a generic relationship graph, while the product needs a database-oriented semantic topology view. The decisions are already made. Do not fall back to another generic graph cleanup.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-resource-topology-view-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-10-console-closeout-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-6-topology-semantic-metadata-worker.md`
- `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx`
- `/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts`
- `/Users/fan/JsProjects/ControlHub/services/topology.ts`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources/resource-detail-sheet.tsx`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
```

Expected:

- worktree path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is dedicated to this phase
- base includes frontend Phase 13.10 on `main`
- worktree is clean

Stop and report if the path, branch, base, or cleanliness is wrong.

## Parallel Coordination Rules

Frontend and backend workers cannot talk to each other during execution. This prompt is self-contained.

- You may improve generic topology UX and database-specific rendering scaffolding immediately.
- Final semantic database topology behavior is **not final** until backend Phase 12.6 lands on `main`.
- Before claiming completion, sync latest `main`, consume the final backend topology semantic fields, and rerun full validation plus live verification.
- Do not lock the UI to a frontend-guessed role model if backend semantic metadata becomes available.
- Recommended execution order is:
  1. backend Phase 12.6 lands first
  2. frontend Phase 14B syncs latest `main`
  3. frontend finishes semantic rendering, E2E, and live verification
- True parallel work is limited to non-final layout scaffolding and generic graph cleanup.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives.

- Database topology must use a semantic layered layout, not a generic BFS-column graph.
- The target reading order is:
  1. application / consumer band: `service`
  2. entry / proxy band: `domain_name`, `virtual_ip`, `database_proxy`
  3. database cluster band: `database_cluster`
  4. replication band: primary then replicas by replication depth
  5. control-plane band: `control_plane_component`
  6. host band: `host`
- Active and standby proxies must be visually distinguishable and stably ordered.
- Primary must be visually above or before replicas.
- Host placement must not cut through the main data path.
- Generic non-database graphs still need a sane fallback layout.
- URL-synced topology controls are required.
- Topology must support a true expanded analysis mode; simply making the inline canvas a little taller is not enough.
- If you introduce a dedicated topology tab or view mode, it must be encoded in the URL so shared links reproduce the same state.
- Keep the detail sheet compact; the full detail page is where topology gets the stronger surface.

## Exact Problems To Fix

### 1. Semantic layering

Required outcome:

- database pages read as an operator-facing topology, not a random graph
- the topology should feel closer to an orchestrator-style operational view than to a generic DAG
- service/application consumers sit above the proxy/database path when present
- VIP/domain appear above proxies
- active/standby proxies appear in the proxy layer
- cluster sits between proxy and instance layers
- primary and replicas are clearly separated
- replicas expand to the right by replication depth
- control-plane and host context are visually subordinate and do not break the main flow

### 2. Edge routing clarity

Current problem:

- edges still visually cross nodes
- important flows are hard to parse

Required outcome:

- use routing and spacing that materially reduce line-through-node failures
- improve node spacing, ports/handles, and layer separation as needed
- do not “solve” this only by changing edge color
- replication edges are the main left-to-right backbone and should almost never cut through instance nodes
- non-replication edges should be routed outside the replication corridor where practical

### 3. Semantic edge treatment

Required outcome:

- traffic, replication, management, placement, and failover-style relationships are visually distinguishable if backend semantics are available
- if backend semantic metadata is absent during early implementation, keep a clean fallback and then switch to backend truth before completion

### 4. Stronger full-page topology surface

Required outcome:

- full detail page gives topology first-class inspection space
- acceptable directions:
  - a dedicated URL-synced topology tab
  - a larger named topology view mode
- the topology surface must support an expanded analysis mode:
  - preferred: app-level fullscreen / immersive overlay
  - acceptable: dedicated full-page view mode
- do not treat “increase inline canvas height again” as sufficient
- keep sheet topology compact, not bloated

### 5. Expanded / fullscreen topology mode

Required outcome:

- the user can open topology into a larger analysis surface from the detail page
- expanded mode preserves current topology state
- expanded mode supports:
  - zoom in / zoom out
  - fit view
  - pan
  - close / back affordance
  - `Esc` exit when practical
- shared links must be able to reproduce the expanded topology view when URL state is present

Implementation guidance:

- prefer an app-level fullscreen overlay or immersive panel first
- browser Fullscreen API may be used as an enhancement, not as the only way to access the larger view
- do not require the detail sheet to support fullscreen

### 6. Localization and role labeling

Required outcome:

- Chinese mode must localize relevant status/role/type labels in topology
- do not leave mixed English role text on nodes if Chinese mode is active

## Required Layout Model

Implement two layout strategies:

### A. Database semantic layout

Use this when the root resource is database-oriented or the topology payload explicitly indicates database semantics.

Requirements:

- layer by backend semantic metadata when available
- if backend semantic metadata is temporarily absent, use a narrow fallback based on current stable truth without over-generalizing to all graphs
- render as a combined banded operator view plus replication tree:
  - top band: application / service consumers
  - upper-middle band: domain / VIP / proxy path
  - middle band: database cluster + instance replication tree
  - lower-middle band: orchestrator / HA / control-plane
  - bottom band: host placement
- within the replication tree:
  - primary/master is fixed at the far left of the instance area
  - direct replicas are one column to the right
  - deeper replicas continue expanding right by `replicationDepth`
  - replication edges should go `source:right -> target:left` where the graph library allows it
- stable ordering within band/layer:
  - active proxy before standby
  - cluster before instance fan-out
  - replicas with children before leaf replicas when practical, then deterministic name/id sort
- preserve deterministic output for the same input
- expanded/fullscreen mode may use a wider horizontal layout than the inline view, but it must preserve the same semantic ordering model

### B. Generic fallback layout

Use this for non-database graphs.

Requirements:

- preserve sane traversal-distance ordering
- do not let database-specific ordering distort generic graphs

## URL State Requirements

Topology controls must be URL-synced. At minimum keep these in URL if they exist:

- depth
- direction
- topology view / tab
- expanded/fullscreen state

Shared links must open to the same topology state.

## TDD Requirements

Use TDD. Add failing tests first.

At minimum add or update tests for:

- database semantic layer ordering
- active vs standby proxy ordering
- primary vs replica ordering
- host placement staying out of the main chain
- generic graph fallback not regressing
- stale request handling if touched
- URL-synced topology control state
- expanded/fullscreen topology mode state
- Chinese localization on topology labels

E2E must cover at least:

- one MySQL cluster page with clearly layered topology
- depth and direction changes reflected in URL and graph refresh
- shared link reproduction if tabs/view mode are introduced
- no obvious edge-through-node failure on the key demo path after layout change
- one path where a replica has its own downstream replica and still expands cleanly to the right
- opening expanded/fullscreen topology mode and preserving state

## Required Live Verification Pages

You must manually verify against a live backend:

- `/resources/41000000-0000-0000-0000-000000000010`
- `/resources/41000000-0000-0000-0000-000000000013`
- one additional database-oriented resource page from current seed data

Verify at least:

- semantic reading order
- proxy/cluster/instance layering
- Chinese labels in Chinese mode
- edge readability
- full-page topology surface
- expanded/fullscreen topology behavior

## Required Verification Commands

Inside the worktree:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

If practical after branch readiness, also verify from the main checkout:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

## Final Report

Your final report must include:

- worktree path
- branch
- commit hash
- whether backend Phase 12.6 semantic metadata was consumed
- exact topology controls now URL-synced
- exact database layering model implemented
- whether the final topology uses a banded operator view plus replication tree, and how
- how expanded/fullscreen topology mode works
- exact generic fallback behavior preserved
- test files added/updated
- all verification command results
- live verification results per required page
- `git status --short --branch`

Do not say “topology looks better”. State how the graph now encodes the operator mental model.
