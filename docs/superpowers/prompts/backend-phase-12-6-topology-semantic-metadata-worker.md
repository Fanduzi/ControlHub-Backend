# Backend Phase 12.6: Topology Semantic Metadata For Database Graphs

You are implementing a focused backend topology-contract phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

This phase exists because the frontend cannot reliably render database-oriented topology from raw nodes/edges alone. The decision is already made: the backend must expose semantic metadata for topology rendering. Do not debate whether the frontend should infer everything itself.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-11-resource-topology-read-model-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-3-demo-data-and-topology-semantics-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-4-topology-semantics-followup-worker.md`
- `/Users/fan/GolangProjects/ControlHub/internal/model/topology.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/topology_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/topology_handler.go`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`
- `/Users/fan/GolangProjects/ControlHub/migrations/0004_seed_demo_data.sql`
- `/Users/fan/GolangProjects/ControlHub/migrations/0008_apply_demo_seed_cleanup_patch.sql`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
docker info
```

Expected:

- worktree path is under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- branch is dedicated to this phase
- base includes backend topology read model and follow-up seed truth on `main`
- worktree is clean
- Docker is available

If Docker is unavailable, stop and report the blocker.

## Parallel Coordination Rules

Frontend and backend workers cannot talk to each other during execution. This prompt is self-contained.

- Your output is the topology semantic-contract freeze for the frontend database-topology UX phase.
- Frontend may work on generic graph cleanup in parallel, but database semantic layout is not final until your metadata lands on `main`.
- If you also need seed-truth follow-up to make the metadata credible, make that change in this phase and report it explicitly.
- Final completion requires exact contract examples so the frontend worker can sync latest `main` and render without guessing roles.
- Recommended execution order is:
  1. backend Phase 12.6 lands first
  2. frontend Phase 14B syncs latest `main`
  3. frontend completes semantic layout, E2E, and live verification
- Treat true parallel work as limited to frontend scaffolding only, not final graph semantics.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives.

- Keep `GET /resources/{id}/topology` as the endpoint.
- It is allowed to extend the response shape with semantic metadata.
- Do not add explicit pixel coordinates.
- Do not add a full graph-layout engine on the backend.
- Do not add topology editing.
- Do not add query execution, discovery, work orders, or new product areas.
- Database-oriented topology must support this semantic stack:
  - application / consumer layer: `service`
  - entry / proxy layer: `domain_name`, `virtual_ip`, `database_proxy`
  - database cluster layer: `database_cluster`
  - replication chain layer: writer / primary / replica `database_instance`
  - control-plane layer: `control_plane_component`
  - host layer: `host`
- Non-database topology must still work without pretending everything is a database graph.

## Exact Scope

Extend topology response so each node and edge carries enough semantic information for the frontend to render layered database graphs deterministically.

### Node-level semantics

Add stable fields for at least:

- `topologyRole`
- `topologyLayer`

Optional if useful:

- `groupKey`
- `visualImportance`
- `isDatabaseTopology`
- `replicationDepth`
- `replicationParentId`

Accepted role vocabulary must be explicit and documented. At minimum support:

- `application`
- `entry`
- `proxy_active`
- `proxy_standby`
- `cluster`
- `primary`
- `replica`
- `replica_intermediate`
- `host`
- `control_plane`
- `service`
- `generic`

Layer must be semantic, not raw BFS distance.

### Edge-level semantics

Add a stable field for at least:

- `semanticType`

Accepted values must be explicit and documented. At minimum support:

- `traffic`
- `failover`
- `replication`
- `membership`
- `placement`
- `management`
- `dependency`
- `monitoring`

### Classification rules

You must make classification deterministic.

Use current resource truth when possible:

- `resourceType`
- `resourceSubtype`
- known relation types
- obvious role-bearing profile fields already available in backend truth

If the current truth is insufficient for a stable classification on demo MySQL paths, patch seed truth in a new migration instead of inventing fragile heuristics.

## Required Database Operator View

The frontend is targeting an operator-facing topology view closer to orchestrator than to a generic force graph.

That means the backend semantics must allow the frontend to separate:

1. application/client band

- services or clients that consume the database path

2. proxy/VIP band

- `domain_name`
- `virtual_ip`
- `database_proxy`

3. MySQL replication cluster band

- `database_cluster`
- primary/master instance
- replicas expanded by replication depth

4. control-plane band

- `control_plane_component`
- orchestrator / HA / monitoring style resources when present

5. host placement band

- `host`

The replication chain is the main structural backbone. Control-plane and host context must not be confused with the data path.

## Required Semantics For Demo Database Graphs

At minimum, live topology for the Payment/Config/Order MySQL family must let the frontend distinguish:

- application/service consumer nodes when present
- domain/VIP entry nodes
- active vs standby proxy
- cluster node
- primary instance
- replica instances
- control-plane component
- host placement edges
- replication depth / parent relationship for instance trees
- monitoring/failover-style control-plane relationships when present in the truth

If any of those are impossible with current truth, fix the truth in this phase and report exactly how.

## TDD Requirements

Use TDD. Add failing tests first.

At minimum add or update tests for:

- topology node semantic-role classification
- topology layer assignment
- topology edge semantic-type assignment
- non-database graph fallback semantics
- deterministic output ordering
- live demo MySQL topology contract

Integration tests must prove at least one real database topology response contains the semantic metadata expected by the frontend.

If seed truth changes, add regression tests for the changed truth as well.

## Verification

You must run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
make test-integration
make test-openapi-fuzz
```

If you add a patch migration, also run:

```bash
make migrate-up
make migrate-status
```

Then verify the local `controlhub` DB truth directly.

## Live Verification

At minimum verify topology payloads for:

- one Order MySQL resource
- one Config MySQL resource
- one Payment MySQL resource

For each, report:

- root resource id
- node count
- edge count
- example node roles/layers
- example edge semantic types

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

If GitNexus is available, run the repo-configured change-impact check before commit.

Stage explicit files only.

## Final Report

Your final report must include:

- worktree path
- branch
- commit hash
- exact topology fields added
- exact role vocabulary added
- exact edge semantic vocabulary added
- whether any migration was added, and filename
- whether demo seed truth changed, and exactly how
- all verification command results
- live verification results with example payload facts
- confirmation whether local `controlhub` DB received the patch
- `git status --short --branch`

Do not say “frontend can infer the rest”. The whole purpose of this phase is to stop frontend guesswork.
