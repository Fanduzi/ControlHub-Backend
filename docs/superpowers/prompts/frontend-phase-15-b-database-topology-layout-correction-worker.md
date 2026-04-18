# Frontend Phase 15B: Database Topology Layout Correction

You are implementing a strict frontend correction phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

This phase exists because Phase 15A fixed React Flow handle warnings but failed the actual product requirement for database topology layout. Passing tests and having zero console warnings is not enough. The topology must visually match the operator mental model requested by the user.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-14-b-database-topology-semantic-ux-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-15-a-console-trust-and-topology-correction-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-6-topology-semantic-metadata-worker.md`
- `/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts`
- `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx`
- `/Users/fan/JsProjects/ControlHub/types/resource.ts`
- `/Users/fan/JsProjects/ControlHub/services/topology.ts`
- `/Users/fan/JsProjects/ControlHub/tests/topology-mapper-semantic.test.ts`
- `/Users/fan/JsProjects/ControlHub/tests/topology-mapper.test.ts`
- `/Users/fan/JsProjects/ControlHub/tests/topology-panel.test.tsx`
- `/Users/fan/JsProjects/ControlHub/e2e/topology.spec.ts`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -10
git worktree list
```

Expected:

- worktree path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is dedicated to this phase, for example `feat/phase-15b-database-topology-layout`
- base includes frontend Phase 15A on `main`
- backend `main` includes Phase 12.6 semantic metadata and Phase 12.7 subtype filtering
- worktree is clean

Stop and report if the path, branch, base, or cleanliness is wrong.

## Why This Phase Exists

Live review of `Order MySQL Cluster Prod` showed the graph is still wrong:

- ProxySQL is on the far left instead of above the database layer.
- The database cluster is rendered as a normal middle node in a horizontal chain.
- Primary/replica layout does not follow “primary left, replicas right by replication depth”.
- Replication edges still visually route like a generic graph.
- Orchestrator/control-plane and host context visually compete with the main database path.

This means Phase 15A's topology layout portion is not accepted. Do not defend the old layout. Replace it with a database-specific layout.

## Fixed Decisions

These decisions are final. Do not ask the user to choose alternatives.

- This phase is topology-only.
- Do not modify overview, resources table, database table, CMDB, audits, settings, auth, archive UI, or backend APIs.
- Do not add database vendor logos/icons.
- Do not add topology editing.
- Do not add new backend semantics unless you prove the existing backend contract is insufficient.
- Do not call the work complete based only on unit tests, Playwright, or console warnings.
- Visual browser verification against real topology pages is mandatory.
- Screenshots are required as local evidence but must not be committed.

## Strict Scope

Allowed files:

- `lib/topology-mapper.ts`
- `components/blocks/topology-panel.tsx`
- `tests/topology-mapper-semantic.test.ts`
- `tests/topology-mapper.test.ts`
- `tests/topology-panel.test.tsx`
- `e2e/topology.spec.ts`
- `messages/en.json`
- `messages/zh-CN.json`

Only edit i18n files if adding or changing topology layer labels.

If you believe another file is required, stop and report why before editing it.

## Target Visual Model

Implement a database-specific layout that follows this mental model:

```text
+------------------------------------------------------+
|                    Application Layer                 |
|                 App / Client / Service               |
+------------------------------+-----------------------+
                               |
                               v
+------------------------------------------------------+
|                   ProxySQL / Entry Layer             |
|  VIP / Domain / ProxySQL active + standby             |
+------------------------------+-----------------------+
                               |
                               | read/write route
                               v

+------------------------------------------------------+
|              MySQL Replication Cluster               |
|                                                      |
|  +------------------+    +------------------+        |
|  |      PRIMARY     |--->|     REPLICA      |---> ... |
|  +------------------+    +------------------+        |
|                                                      |
|  Other direct replicas fan out to the right           |
+------------------------------------------------------+

+------------------------------------------------------+
|              Orchestrator / Control Plane            |
|          monitoring / failover / promotion            |
+------------------------------------------------------+

+------------------------------------------------------+
|                  Host / Placement Layer              |
+------------------------------------------------------+
```

Required geometry:

- Application/service nodes are above entry/proxy nodes.
- VIP/domain/proxy nodes are above the MySQL replication cluster.
- Database cluster is not a generic horizontal middle node between proxy and instances.
- Primary/master database instance is fixed at the left side of the replication area.
- Replicas expand to the right by `replicationDepth`.
- Direct replicas with the same depth are vertically stacked to the right of the primary.
- Deeper replicas are further right.
- Standby proxy is near active proxy, not mixed into instance rows.
- Orchestrator/control-plane is separated from the replication corridor.
- Hosts are below the database path and visually subordinate.

## Cluster Node Rule

The `database_cluster` node must not be rendered as the center of a left-to-right chain.

Choose one of these implementations:

1. Preferred: render database cluster as a group/band label or background container for replication nodes.
2. Acceptable: render it as a small header/anchor in the replication band, above or near the primary, but not between proxy and primary as a normal path node.

If you keep it as a visible React Flow node, it must not force `proxy -> cluster -> instance` horizontal routing that crosses the replication tree.

## Edge Routing Rules

Edges must match the visual model.

Required:

- `replication` / `replicates_to`: `source-right -> target-left`
- `traffic` / active `fronts` / `points_to`: upper layer to lower layer, `source-bottom -> target-top`
- `failover`: proxy/entry layer relationship, visually distinct but not in replication corridor
- `management` / `monitoring`: control plane to cluster/instances, weak/dashed, routed outside the replication corridor where practical
- `placement` / `runs_on`: database to host, downward and weak
- `membership` / `member_of`: must not become the dominant visual edge if explicit replication exists

Do not set edge handle IDs unless matching `Handle` IDs and `type` exist.

React Flow source/target type rule:

- `sourceHandle` must reference a `Handle type="source"`.
- `targetHandle` must reference a `Handle type="target"`.
- If a position can be both source and target, use prefixed IDs such as `source-right` and `target-right`.

## Layout Algorithm Requirements

Implement a separate database layout path. Do not keep stretching the generic graph.

Recommended shape:

```text
if topology.isDatabaseTopology:
  use mapDatabaseTopologyToFlow(...)
else:
  use existing generic distance layout
```

Database layout should:

- identify primary nodes using `topologyRole === "primary"` or replication edges with outgoing but no incoming replication
- place primary at replication column 0
- place replicas at `replicationDepth` columns to the right
- if backend `replicationDepth` is missing, compute it from `semanticType === "replication"` edges
- place proxies/entry nodes above the replication area
- place services above proxies/entry
- place control-plane nodes below or side-below the replication area
- place hosts below the replication area
- keep deterministic order:
  - active proxy before standby
  - primary before replicas
  - replicas with children before leaf replicas when possible
  - then by display/name/id

## Visual Grouping Requirements

The expanded topology view must visually communicate layers.

At minimum show layer labels/bands for:

- Application Layer
- ProxySQL / Entry Layer
- MySQL Replication Cluster
- Orchestrator / Control Plane
- Host / Placement

Inline topology may be simpler, but expanded mode must show the grouping clearly.

## Non-Database Fallback

Generic non-database graphs must still work.

Requirements:

- keep traversal-distance ordering
- do not apply database-specific vertical stack rules to generic graphs
- existing generic layout tests must still pass or be intentionally updated with equivalent assertions

## TDD Requirements

Use TDD. Add failing tests first.

At minimum add or update tests for:

- proxy/entry nodes have lower `y` than application and higher `y` than database replication nodes
- database cluster does not sit between proxy and primary as a normal left-to-right middle node
- primary x-position is left of all replicas
- replica x-position increases with `replicationDepth`
- direct replicas with the same depth share the same x band and stack vertically
- replication edges use `source-right -> target-left`
- traffic edges use vertical handles
- management/monitoring edges do not use replication handles
- layer bands include Application, ProxySQL/Entry, MySQL Replication Cluster, Orchestrator/Control Plane, Host/Placement
- generic non-database fallback does not regress
- expanded topology mode still works with URL sync
- no edge references an invalid handle id or wrong handle type

Tests must assert geometry facts, not just snapshots.

## Required Verification

Run all:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npx playwright test e2e/topology.spec.ts
npx playwright test
```

If full Playwright is blocked by backend availability, start the local backend and frontend correctly and rerun. Do not claim completion without full Playwright.

## Mandatory Live Browser Verification

Use the current local backend and frontend.

Verify these pages in expanded topology mode:

- Order MySQL Cluster Prod:
  - `/resources/40000000-0000-0000-0000-000000000001?topologyDepth=2&topologyExpanded=1`
- Payment MySQL Cluster Production:
  - `/resources/41000000-0000-0000-0000-000000000010?topologyDepth=2&topologyExpanded=1`
- Platform Config Service MySQL Cluster Production:
  - `/resources/41000000-0000-0000-0000-000000000013?topologyDepth=2&topologyExpanded=1`

For each page, report:

- whether ProxySQL / entry appears above MySQL replication
- whether primary/master is visually left of replicas
- whether replicas expand rightward by replication depth
- whether database cluster is not a generic middle chain node
- whether orchestrator/control-plane is separated from the replication corridor
- whether host placement is subordinate
- whether console has zero React Flow warnings
- whether console has zero NaN/pageerror errors

You must also capture local screenshots for each page and inspect them before reporting. Do not commit screenshots.

## Acceptance Bar

This phase is not accepted if:

- ProxySQL is still on the far left of the graph as the main horizontal chain start.
- The database cluster is still a normal middle node between proxy and instances.
- Primary and replicas are not visually left-to-right.
- Replication edges still connect to the wrong side of nodes.
- Control-plane or host nodes visually dominate the replication path.
- You only report tests/console and do not report visual layout facts from screenshots/browser.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage only allowed files. Do not commit screenshots, `.next`, `test-results`, logs, local env files, or `.worktrees`.

## Commit

Commit after verification passes.

Suggested message:

```bash
git commit -m "fix: correct database topology layout model (Phase 15B)"
```

Do not add AI co-author trailers.

## Final Report Requirements

Only write a final closeout report if all Closeout Gate requirements from the shared guardrails are satisfied.

The final report must include:

- commit hash
- worktree path and branch
- clean git status
- changed files
- exact database layout algorithm used
- cluster node treatment
- edge handle model
- layer/band model
- test results for every required command
- live browser verification table for all three required resources
- screenshot inspection summary for all three required resources
- console verification results
- negative scope confirmation:
  - did not change overview/resources/databases table behavior
  - did not change backend APIs
  - did not add logos/icons
  - did not add topology editing
  - did not tag, push, release, or add AI co-author
- next phase input, if any remains
