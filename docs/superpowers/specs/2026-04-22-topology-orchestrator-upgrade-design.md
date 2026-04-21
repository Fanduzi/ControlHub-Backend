# Topology Panel — Orchestrator-Style Upgrade Design

**Goal:** Upgrade the topology panel from a simple node graph to an Orchestrator-style visualization with rich node details (IP/port/hostname), status-colored nodes, zone-aware grouping, problem summaries, and clickable detail popups.

**Reference:** MySQL Orchestrator topology UI — nodes show IP:port, version, and status color; borders indicate datacenter/zone; problem panel lists anomalies; clicking a node opens a detail modal.

---

## Current State

- **Rendering**: ReactFlow with custom `topologyNode` and `topologyGroup` components
- **Node info**: displayName, resourceType, resourceSubtype, healthStatus badge, lifecycleStatus badge, role label
- **Missing**: No IP, hostname, or port on nodes; no status-based node coloring; no datacenter grouping; no problem summary; no click-to-detail popup
- **Backend**: `TopologyNode` struct has no profile fields; `buildTopologyNodes()` ignores `res.ProfileSummary`

---

## Design

### 1. Enriched Node Data (Backend)

**TopologyNode** gets new optional fields:

```go
type TopologyNode struct {
    // ... existing fields unchanged ...

    // NEW: Profile metadata for display
    Hostname  string `json:"hostname,omitempty"`
    IP        string `json:"ip,omitempty"`
    Port      int    `json:"port,omitempty"`

    // NEW: Topology-specific problem flags
    Problems []TopologyProblem `json:"problems,omitempty"`
}

type TopologyProblem struct {
    Severity  string `json:"severity"`   // "warning" | "critical"
    Message   string `json:"message"`
    Code      string `json:"code"`
}
```

**In `buildTopologyNodes()`**, extract from the already-loaded `model.Resource`:

```go
if res.ProfileSummary != nil {
    node.Hostname = res.ProfileSummary.Hostname
    node.IP = res.ProfileSummary.IP
    node.Port = res.ProfileSummary.Port
}
```

No new repository queries needed — `ProfileSummary` is already populated on the `Resource` model.

**Problem detection** in the topology service:

| Condition | Severity | Code | Message |
|-----------|----------|------|---------|
| `healthStatus == "critical"` | critical | `health_critical` | Resource health is critical |
| `healthStatus == "warning"` or `healthStatus == "degraded"` | warning | `health_warning` | Resource health degraded |
| `lifecycleStatus == "stopped"` | critical | `lifecycle_stopped` | Resource is stopped |
| `lifecycleStatus == "provisioning"` | warning | `lifecycle_provisioning` | Resource is provisioning |
| Database instance: no running replica found for primary | warning | `no_replica` | No healthy replica available |

The topology response also gets a top-level `problems` summary:

```go
type TopologyResponse struct {
    // ... existing fields ...
    Problems []TopologyProblemSummary `json:"problems,omitempty"`
}

type TopologyProblemSummary struct {
    ResourceID   string            `json:"resourceId"`
    ResourceName string            `json:"resourceName"`
    ResourceType string            `json:"resourceType"`
    Severity     string            `json:"severity"`
    Problems     []TopologyProblem `json:"problems"`
}
```

### 2. Node Visual Design (Frontend)

**Orchestrator-style node layout** for database instances:

```
┌──────────────────────────────┐
│ ● mysql   Primary       [⚙] │  ← engine icon + role + click target
│ 10.0.10.20:3306              │  ← IP:port (bold, primary info)
│ prod-db-host-01              │  ← hostname (muted)
│ ■ Healthy  ▸ Running         │  ← status badges
└──────────────────────────────┘
```

For hosts and other types:

```
┌──────────────────────────────┐
│ □ Host                       │
│ 10.0.10.22                   │  ← IP
│ prod-db-host-02              │  ← hostname
│ ■ Healthy  ▸ Running         │
└──────────────────────────────┘
```

**Status-based node coloring** (like Orchestrator):

| Condition | Border Color | Background |
|-----------|-------------|------------|
| Root node | `primary` (blue ring) | `primary/5` |
| `healthStatus == "critical"` OR `lifecycleStatus == "stopped"` | `red-500` | `red-500/10` |
| `healthStatus == "warning"` OR `healthStatus == "degraded"` | `amber-500` | `amber-500/10` |
| Healthy, running | Default border | Default bg |
| Any problem present | Colored by worst severity | Tinted bg |

**Priority**: Critical > Warning > Healthy. If a node has both a critical and warning problem, it shows red.

### 3. Zone-Aware Grouping (Frontend)

**Datacenter/zone from labels or environment:**

- If resource has label `zone` or `datacenter`: use that value as zone key
- Fallback: use `environmentId` as the zone identifier (Production, Staging, etc.)

**Visual**: Group box border color encodes zone. Use a deterministic color from a palette:

```
const ZONE_COLORS = [
  { border: "border-blue-400", bg: "bg-blue-400/5" },
  { border: "border-emerald-400", bg: "bg-emerald-400/5" },
  { border: "border-amber-400", bg: "bg-amber-400/5" },
  { border: "border-violet-400", bg: "bg-violet-400/5" },
];
```

Zone label appears at top-left of the group box: `"Zone: us-east-1a"` or `"Production"`.

**Layout adjustment**: In `computeDatabaseLayout()`, nodes sharing the same zone key are placed adjacent within their layer band. Each zone gets its own group box wrapping its nodes.

### 4. Problem Summary Panel (Frontend)

A collapsible panel **above** the topology graph:

```
┌─ Problems (3) ──────────────────────────────────────── [▼ Collapse] ─┐
│                                                                        │
│  🔴 Order MySQL Replica 01 — health_critical, lifecycle_stopped       │
│  🟡 Config Service MySQL Primary — health_warning                     │
│  🟡 Analytics ClickHouse Node 02 — health_critical                    │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
```

- Only shown when `response.problems.length > 0`
- Count badge in the header: `"Problems (3)"`
- Each row: severity icon (colored dot) + resource name + problem codes
- Clicking a row highlights/selects the node in the graph

**i18n keys** added for problem messages and panel labels.

### 5. Clickable Node Detail Popup (Frontend)

On node click, instead of navigating away, show a **popover/sheet** with key details:

```
┌─────────────────────────────────────┐
│  Order MySQL 01 Prod          [✕]   │
│  ─────────────────────────────────  │
│  Type        Database Instance      │
│  Engine      MySQL 8.0              │
│  Host        prod-db-host-01        │
│  Address     10.0.10.20:3306        │
│  Health      ● Healthy              │
│  Lifecycle   ▸ Running              │
│  Role        Primary                │
│  ─────────────────────────────────  │
│  [View Full Details →]             │
└─────────────────────────────────────┘
```

- Uses a **Popover** component anchored to the clicked node
- Shows: displayName, resourceType, engine, hostname, IP:port, health, lifecycle, role
- "View Full Details" link navigates to `/resources/{id}`
- Close on click-outside or Escape
- The existing click-to-navigate behavior is preserved via the detail link

### 6. Frontend Type Updates

```typescript
// types/resource.ts additions
type TopologyNode = {
  // ... existing fields ...
  hostname?: string;
  ip?: string;
  port?: number;
  problems?: TopologyProblem[];
};

type TopologyProblem = {
  severity: "warning" | "critical";
  message: string;
  code: string;
};

type TopologyResponse = {
  // ... existing fields ...
  problems?: TopologyProblemSummary[];
};

type TopologyProblemSummary = {
  resourceId: string;
  resourceName: string;
  resourceType: string;
  severity: "warning" | "critical";
  problems: TopologyProblem[];
};
```

---

## Scope

### In Scope
1. Backend: Enrich `TopologyNode` with hostname/IP/port, add problem detection
2. Backend: Add `problems` summary to `TopologyResponse`
3. Frontend: Orchestrator-style node rendering with IP:port, hostname, status coloring
4. Frontend: Problem summary panel above topology graph
5. Frontend: Clickable node detail popup (popover)
6. Frontend: Zone-aware group box coloring (label-based)
7. i18n: New keys for problem messages and panel labels

### Out of Scope
- Orchestrator-style actions (failover, stop slave, etc.) — admin actions are separate
- Real-time replication lag — no streaming/lag data available yet
- Drag-to-rearrange nodes — ReactFlow already supports this
- Full-screen topology redesign — keep existing expanded mode

---

## Files Changed

### Backend (Go)
- `internal/model/topology.go` — Add `Hostname`, `IP`, `Port`, `Problems` to `TopologyNode`; add `TopologyProblem`, `TopologyProblemSummary` types; add `Problems` to `TopologyResponse`
- `internal/service/topology_service.go` — Extract profile data in `buildTopologyNodes()`; add `detectProblems()` function; add problem summary to response

### Frontend (TSX/TS)
- `types/resource.ts` — Add `hostname`, `ip`, `port`, `problems` to `TopologyNode`; add `TopologyProblem`, `TopologyProblemSummary` types
- `components/blocks/topology-panel.tsx` — Orchestrator-style node rendering, status coloring, problem panel, click popup, zone grouping
- `messages/en.json` — New keys for problems panel, problem messages
- `messages/zh-CN.json` — New keys (Chinese)

### Tests
- `internal/service/topology_service_test.go` — Test problem detection logic
- `tests/components/topology-panel.test.tsx` (new) — Test node rendering with enriched data
