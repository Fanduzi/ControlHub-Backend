# Topology Orchestrator-Style Upgrade — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade topology panel to Orchestrator-style with IP/port on nodes, status coloring, problem summary, zone grouping, and clickable detail popup.

**Architecture:** Enrich backend TopologyNode with profile data + problem detection. Frontend upgrades ReactFlow node rendering to Orchestrator-style, adds problem panel, zone coloring, and detail popover.

**Tech Stack:** Go 1.26 (backend), ReactFlow + shadcn/ui (frontend), next-intl (i18n)

---

### Task 1: Backend — Enrich TopologyNode model

**Files:**
- Modify: `internal/model/topology.go`

- [ ] **Step 1: Add new fields to TopologyNode and new types**

In `internal/model/topology.go`, add to `TopologyNode` struct (after `ReplicationParentID`):

```go
Hostname  string             `json:"hostname,omitempty"`
IP        string             `json:"ip,omitempty"`
Port      int                `json:"port,omitempty"`
Problems  []TopologyProblem  `json:"problems,omitempty"`
```

Add new types after `TopologyResponse`:

```go
type TopologyProblem struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Code     string `json:"code"`
}

type TopologyProblemSummary struct {
	ResourceID   string            `json:"resourceId"`
	ResourceName string            `json:"resourceName"`
	ResourceType string            `json:"resourceType"`
	Severity     string            `json:"severity"`
	Problems     []TopologyProblem `json:"problems"`
}
```

Add `Problems` field to `TopologyResponse`:

```go
Problems []TopologyProblemSummary `json:"problems,omitempty"`
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/model/topology.go
git commit -m "feat: add profile fields and problem types to topology model"
```

---

### Task 2: Backend — Extract profile data and detect problems in topology service

**Files:**
- Modify: `internal/service/topology_service.go`

- [ ] **Step 1: Extract profile data in buildTopologyNodes**

In `buildTopologyNodes()`, after setting `IsDatabaseTopology`, add:

```go
if res.ProfileSummary != nil {
	node.Hostname = res.ProfileSummary.Hostname
	node.IP = res.ProfileSummary.IP
	node.Port = res.ProfileSummary.Port
}
```

- [ ] **Step 2: Add problem detection function**

Add after `buildTopologyNodes()`:

```go
func detectNodeProblems(res *model.Resource) []model.TopologyProblem {
	var problems []model.TopologyProblem
	switch res.HealthStatus {
	case string(model.HealthCritical):
		problems = append(problems, model.TopologyProblem{
			Severity: "critical", Code: "health_critical",
			Message: "Resource health is critical",
		})
	case string(model.HealthWarning), string(model.HealthDegraded):
		problems = append(problems, model.TopologyProblem{
			Severity: "warning", Code: "health_warning",
			Message: "Resource health is degraded",
		})
	}
	switch res.LifecycleStatus {
	case string(model.LifecycleStopped):
		problems = append(problems, model.TopologyProblem{
			Severity: "critical", Code: "lifecycle_stopped",
			Message: "Resource is stopped",
		})
	case string(model.LifecycleProvisioning):
		problems = append(problems, model.TopologyProblem{
			Severity: "warning", Code: "lifecycle_provisioning",
			Message: "Resource is provisioning",
		})
	}
	return problems
}
```

- [ ] **Step 3: Call problem detection in buildTopologyNodes**

After profile data extraction, add:

```go
node.Problems = detectNodeProblems(res)
```

- [ ] **Step 4: Build problem summary in BuildTopology**

In `BuildTopology()`, after building nodes and edges, add problem summary assembly:

```go
// Build problem summary from nodes with problems
var problemSummaries []model.TopologyProblemSummary
for _, n := range nodes {
	if len(n.Problems) == 0 {
		continue
	}
	worstSeverity := "warning"
	for _, p := range n.Problems {
		if p.Severity == "critical" {
			worstSeverity = "critical"
			break
		}
	}
	problemSummaries = append(problemSummaries, model.TopologyProblemSummary{
		ResourceID:   n.ID,
		ResourceName: n.DisplayName,
		ResourceType: string(n.ResourceType),
		Severity:     worstSeverity,
		Problems:     n.Problems,
	})
}
```

Set `response.Problems = problemSummaries` when building the response.

- [ ] **Step 5: Verify it compiles and passes tests**

Run: `go build ./... && go test ./internal/service/ -v -run TestTopology -count=1`
Expected: BUILD PASS, existing tests still pass

- [ ] **Step 6: Commit**

```bash
git add internal/service/topology_service.go
git commit -m "feat: extract profile data and detect problems in topology service"
```

---

### Task 3: Backend — Add topology service tests

**Files:**
- Modify: `internal/service/topology_service_test.go` (or create if needed)

- [ ] **Step 1: Write test for profile data enrichment**

Test that `buildTopologyNodes` correctly maps `ProfileSummary.Hostname/IP/Port` to `TopologyNode`.

- [ ] **Step 2: Write test for problem detection**

Test `detectNodeProblems` for each case:
- healthy + running → no problems
- critical health → critical problem
- warning health → warning problem
- stopped lifecycle → critical problem
- provisioning lifecycle → warning problem
- critical + stopped → two problems

- [ ] **Step 3: Run tests**

Run: `go test ./internal/service/ -v -count=1`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git add internal/service/topology_service_test.go
git commit -m "test: add topology problem detection and profile enrichment tests"
```

---

### Task 4: Frontend — Update TypeScript types

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/types/resource.ts`

- [ ] **Step 1: Add new fields to TopologyNode type**

Add to the `TopologyNode` type:

```typescript
hostname?: string;
ip?: string;
port?: number;
problems?: TopologyProblem[];
```

- [ ] **Step 2: Add new types**

After `TopologyNode`:

```typescript
type TopologyProblem = {
  severity: "warning" | "critical";
  message: string;
  code: string;
};

type TopologyProblemSummary = {
  resourceId: string;
  resourceName: string;
  resourceType: string;
  severity: "warning" | "critical";
  problems: TopologyProblem[];
};
```

- [ ] **Step 3: Update TopologyResponse**

Add to `TopologyResponse`:

```typescript
problems?: TopologyProblemSummary[];
```

- [ ] **Step 4: Commit**

```bash
git add types/resource.ts
git commit -m "feat: add profile, problem types to topology frontend types"
```

---

### Task 5: Frontend — Orchestrator-style node rendering

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/components/blocks/topology-panel.tsx`

- [ ] **Step 1: Add status-to-color mapping**

Add constants at top of file:

```typescript
const NODE_STATUS_STYLES: Record<string, { border: string; bg: string }> = {
  critical: { border: "border-red-500/70", bg: "bg-red-500/10" },
  warning: { border: "border-amber-500/70", bg: "bg-amber-500/10" },
  healthy: { border: "border-border", bg: "bg-card" },
};

function getNodeStatusStyle(data: TopologyNodeData) {
  const worstProblem = data.problems?.find(p => p.severity === "critical")
    ? "critical"
    : data.problems?.length
      ? "warning"
      : "healthy";
  if (data.isRoot) return null; // root keeps its own style
  return NODE_STATUS_STYLES[worstProblem] ?? NODE_STATUS_STYLES.healthy;
}
```

- [ ] **Step 2: Rewrite the topologyNode component body**

Replace the node content to show:
- Line 1: DbTypeIcon + engine name + role label
- Line 2: IP:port (bold) — for database instances; IP only for hosts
- Line 3: hostname (muted) — if available
- Line 4: Status badges (health + lifecycle)

Apply status-based border/bg coloring from `getNodeStatusStyle()`.

Key rendering logic:

```tsx
// IP:port display
const addressParts: string[] = [];
if (data.ip) addressParts.push(data.ip);
if (data.port) addressParts.push(String(data.port));
const address = addressParts.join(":");

// Inside the node component, replace the content divs:
<div className="flex items-center gap-2">
  {(data.resourceType === "database_instance" || data.resourceType === "database_cluster" || data.resourceType === "database_proxy") && data.resourceSubtype && (
    <DbTypeIcon subtype={data.resourceSubtype} className="size-3.5" />
  )}
  <span className="font-medium text-foreground">
    {data.ip ? address : data.displayName || data.name}
  </span>
  {data.isRoot && <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold text-primary">{t("topology.rootLabel")}</span>}
  {roleLabel && !data.isRoot && isDatabase && (
    <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">{roleLabel}</span>
  )}
</div>
{data.ip && (
  <div className="text-[11px] font-mono text-foreground/80 mt-0.5">
    {address}
  </div>
)}
{data.hostname && data.hostname !== data.ip && (
  <div className="text-[10px] text-muted-foreground mt-0.5 truncate max-w-[180px]">
    {data.hostname}
  </div>
)}
{!data.ip && (
  <div className="mt-1 flex items-center gap-2 text-muted-foreground">
    <span>{getTypeLabel(data.resourceType)}</span>
    {data.resourceSubtype && (
      <>
        <span>·</span>
        <span>{data.resourceSubtype}</span>
      </>
    )}
  </div>
)}
<div className="mt-1 flex gap-1">
  <StatusBadge status={data.healthStatus} tone="health" className="text-[10px]" />
  <StatusBadge status={data.lifecycleStatus} tone="lifecycle" className="text-[10px]" />
</div>
```

- [ ] **Step 3: Verify rendering in browser**

Run dev server, navigate to a resource detail page with topology, verify nodes show IP:port, hostname, and status coloring.

- [ ] **Step 4: Commit**

```bash
git add components/blocks/topology-panel.tsx
git commit -m "feat: Orchestrator-style node rendering with IP/port, hostname, status coloring"
```

---

### Task 6: Frontend — Problem summary panel

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/components/blocks/topology-panel.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/messages/en.json`
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/messages/zh-CN.json`

- [ ] **Step 1: Add i18n keys**

In `en.json`, add under `"topology"`:

```json
"problemsTitle": "Problems",
"noProblems": "All resources healthy",
"problemHealthCritical": "Health critical",
"problemHealthWarning": "Health degraded",
"problemLifecycleStopped": "Resource stopped",
"problemLifecycleProvisioning": "Resource provisioning",
"problemNoReplica": "No healthy replica"
```

In `zh-CN.json`, add under `"topology"`:

```json
"problemsTitle": "异常汇总",
"noProblems": "所有资源健康",
"problemHealthCritical": "健康状态异常",
"problemHealthWarning": "健康状态降级",
"problemLifecycleStopped": "资源已停止",
"problemLifecycleProvisioning": "资源正在部署",
"problemNoReplica": "无可用健康副本"
```

- [ ] **Step 2: Add problem panel component**

In `topology-panel.tsx`, add a collapsible problem panel above the ReactFlow container. It renders when `topologyData.problems?.length > 0`:

```tsx
{topologyData.problems && topologyData.problems.length > 0 && (
  <div className="rounded-lg border border-border bg-background mb-3">
    <button
      onClick={() => setProblemsExpanded(!problemsExpanded)}
      className="flex w-full items-center justify-between px-4 py-2 text-sm font-medium"
    >
      <span className="flex items-center gap-2">
        <AlertTriangle className="size-4 text-amber-500" />
        {t("topology.problemsTitle")} ({topologyData.problems.length})
      </span>
      <ChevronDown className={cn("size-4 transition-transform", problemsExpanded && "rotate-180")} />
    </button>
    {problemsExpanded && (
      <div className="border-t border-border px-4 py-2 space-y-1">
        {topologyData.problems.map((p) => (
          <div
            key={p.resourceId}
            className="flex items-center gap-2 text-xs py-1 cursor-pointer hover:bg-muted/50 rounded px-2"
            onClick={() => highlightNode(p.resourceId)}
          >
            <span className={cn("size-2 rounded-full shrink-0", p.severity === "critical" ? "bg-red-500" : "bg-amber-500")} />
            <span className="font-medium">{p.resourceName}</span>
            <span className="text-muted-foreground">
              {p.problems.map(pr => t(`topology.problem${pr.code.charAt(0).toUpperCase()}${pr.code.slice(1)}`)).join(", ")}
            </span>
          </div>
        ))}
      </div>
    )}
  </div>
)}
```

Add `problemsExpanded` state and `highlightNode` function (uses ReactFlow's `setNodes` to highlight a node by adding a temporary ring class).

- [ ] **Step 3: Commit**

```bash
git add components/blocks/topology-panel.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: add problem summary panel to topology view"
```

---

### Task 7: Frontend — Clickable node detail popup

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/components/blocks/topology-panel.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/messages/en.json`
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/messages/zh-CN.json`

- [ ] **Step 1: Add node detail popover**

Add state for `selectedNodeData` and render a Popover anchored near the topology graph when a node is clicked (instead of navigating away). The popover shows:

- displayName
- resourceType (localized)
- engine (with DbTypeIcon)
- hostname
- IP:port
- healthStatus badge
- lifecycleStatus badge
- role label
- "View Full Details →" link to `/resources/{id}`

Use shadcn Popover component. Position: `side="right"` with `sideOffset={8}`.

- [ ] **Step 2: Update i18n**

Add keys for popover labels:

```json
"viewDetails": "View Full Details →"
"address": "Address"
```

```json
"viewDetails": "查看完整详情 →"
"address": "地址"
```

- [ ] **Step 3: Commit**

```bash
git add components/blocks/topology-panel.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: clickable node detail popup in topology view"
```

---

### Task 8: Frontend — Zone-aware group coloring

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/components/blocks/topology-panel.tsx`

- [ ] **Step 1: Add zone color palette**

```typescript
const ZONE_PALETTE = [
  { border: "border-blue-400/60", bg: "bg-blue-400/5", label: "text-blue-500" },
  { border: "border-emerald-400/60", bg: "bg-emerald-400/5", label: "text-emerald-500" },
  { border: "border-amber-400/60", bg: "bg-amber-400/5", label: "text-amber-500" },
  { border: "border-violet-400/60", bg: "bg-violet-400/5", label: "text-violet-500" },
];

function getZoneColor(zoneKey: string) {
  let hash = 0;
  for (let i = 0; i < zoneKey.length; i++) hash = zoneKey.charCodeAt(i) + ((hash << 5) - hash);
  return ZONE_PALETTE[Math.abs(hash) % ZONE_PALETTE.length];
}
```

- [ ] **Step 2: Apply zone colors to group boxes**

In the `topologyGroup` component, use `getZoneColor(data.label)` to set the border and background classes on the group box. Show a zone label tag at top-left.

- [ ] **Step 3: Commit**

```bash
git add components/blocks/topology-panel.tsx
git commit -m "feat: zone-aware group box coloring in topology view"
```

---

### Task 9: Build verification and E2E testing

**Files:**
- None (verification only)

- [ ] **Step 1: Run Next.js build**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build`
Expected: BUILD PASS

- [ ] **Step 2: Run Go tests**

Run: `cd /Users/fan/GolangProjects/ControlHub && go test ./internal/service/ -v -run TestTopology -count=1`
Expected: ALL PASS

- [ ] **Step 3: Run frontend test suite**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx vitest run`
Expected: ALL PASS

- [ ] **Step 4: E2E browser verification**

Open a resource detail page in the browser. Verify:
1. Topology nodes show IP:port and hostname
2. Problem nodes have colored borders (red/amber)
3. Problem summary panel shows when problems exist
4. Clicking a node opens detail popover
5. Group boxes have zone-colored borders

- [ ] **Step 5: Final commit if any fixes needed**
