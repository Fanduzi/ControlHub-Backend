# Phase 23 Database Read-Model Consistency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a read-only consistency layer to database detail pages so operators can see whether members, relations, topology, and profile data agree.

**Architecture:** Keep backend contracts unchanged. Add pure frontend consistency helpers, render compact consistency/context panels below topology, and preserve the Phase 22B compact-vs-diagnostic decision deck behavior.

**Tech Stack:** Next.js App Router, React, TypeScript, next-intl, existing resource/detail view models, existing topology data flow, Vitest, Playwright, Phase 18C E2E governance.

---

## File Structure

- Create `lib/database-read-model-consistency.ts`
  - Pure helper functions for cluster and instance consistency checks.
  - No React, no network calls.
- Create `tests/lib/database-read-model-consistency.test.ts`
  - Unit coverage for all consistency rules.
- Create `components/resources/database-consistency-panel.tsx`
  - Renders compact OK state or warning issue list.
- Create `components/resources/database-instance-context-panel.tsx`
  - Renders instance parent cluster, role, and connection facts.
- Modify `app/(console)/resources/[id]/page.tsx`
  - Render consistency panel below topology for database resources.
  - Render instance context panel for database instances.
- Modify `messages/en.json`
  - Add `databaseConsistency` keys.
- Modify `messages/zh-CN.json`
  - Add matching Chinese keys.
- Modify `tests/resource-detail-page.test.tsx`
  - Page-level rendering coverage.
- Create or update `tests/components/database-consistency-panel.test.tsx`
  - Component rendering coverage.
- Create or update `tests/components/database-instance-context-panel.test.tsx`
  - Instance context rendering coverage.
- Update `e2e/operator-database-workflow.spec.ts`
  - E2E assertions for consistency/context sections.

## Phase Constraints

- Frontend only.
- No backend code.
- No backend API contract changes.
- No SQL execution.
- No work orders.
- No write operations.
- No topology editing.
- No full-page tabs.
- No broad output suppression.
- No AI co-author in commits.

---

## Task 1: Add Pure Consistency Helper Tests

**Files:**
- Create: `tests/lib/database-read-model-consistency.test.ts`
- Create later: `lib/database-read-model-consistency.ts`

- [ ] **Step 1: Write failing helper tests**

Create `tests/lib/database-read-model-consistency.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import type { ClusterMember, TopologyResponse } from "@/types/resource";
import type { ResourceDetailViewModel } from "@/types/view-models";
import {
  buildClusterConsistency,
  buildInstanceConsistency,
} from "@/lib/database-read-model-consistency";

function clusterResource(): ResourceDetailViewModel {
  return {
    id: 14,
    resourceType: "database_cluster",
    resourceSubtype: "mysql",
    name: "payment-mysql-cluster-prod",
    displayName: "Payment MySQL Cluster Production",
    environmentId: 1,
    ownerId: 1,
    lifecycleStatus: "running",
    healthStatus: "healthy",
    source: "seed",
    externalId: "dbaas-payment-mysql-cluster-prod",
    labels: {},
    createdAt: "",
    updatedAt: "",
    archivedAt: null,
    archivedBy: null,
    archiveReason: null,
    environmentName: "Production",
    ownerName: "DBA Team",
    summary: "",
    isArchived: false,
    profile: {},
    profileSummary: { engine: "mysql", nodeCount: 2 },
    relations: [
      {
        id: 1,
        fromResourceId: 22,
        toResourceId: 14,
        relationType: "member_of",
        createdAt: "",
        direction: "incoming",
        relatedResourceId: 22,
        relatedResourceName: "payment-mysql-primary-prod",
        relatedResourceDisplayName: "Payment MySQL Primary Production",
        relatedResourceType: "database_instance",
        relatedResourceSubtype: "mysql",
        relatedResourceHealthStatus: "healthy",
        relatedResourceLifecycleStatus: "running",
      },
    ],
    auditEvents: [],
    recentAudits: [],
    members: [],
  };
}

function instanceResource(): ResourceDetailViewModel {
  return {
    ...clusterResource(),
    id: 22,
    resourceType: "database_instance",
    name: "payment-mysql-primary-prod",
    displayName: "Payment MySQL Primary Production",
    profileSummary: {
      engine: "mysql",
      version: "8.0.36",
      hostname: "prod-db-host-02.internal",
      port: 3307,
      role: "primary",
    },
    clusterInfo: {
      id: 14,
      displayName: "Payment MySQL Cluster Production",
      healthStatus: "healthy",
      lifecycleStatus: "running",
    },
    relations: [
      {
        id: 1,
        fromResourceId: 22,
        toResourceId: 14,
        relationType: "member_of",
        createdAt: "",
        direction: "outgoing",
        relatedResourceId: 14,
        relatedResourceName: "payment-mysql-cluster-prod",
        relatedResourceDisplayName: "Payment MySQL Cluster Production",
        relatedResourceType: "database_cluster",
        relatedResourceSubtype: "mysql",
        relatedResourceHealthStatus: "healthy",
        relatedResourceLifecycleStatus: "running",
      },
    ],
    members: undefined,
  };
}

function member(overrides: Partial<ClusterMember> = {}): ClusterMember {
  return {
    id: 22,
    name: "payment-mysql-primary-prod",
    displayName: "Payment MySQL Primary Production",
    resourceType: "database_instance",
    resourceSubtype: "mysql",
    lifecycleStatus: "running",
    healthStatus: "healthy",
    profileSummary: {
      role: "primary",
      hostname: "prod-db-host-02.internal",
      port: 3307,
      engine: "mysql",
    },
    ...overrides,
  };
}

function topology(resourceIds: number[]): TopologyResponse {
  return {
    rootId: 14,
    isDatabaseTopology: true,
    nodes: resourceIds.map((id) => ({
      id,
      name: `node-${id}`,
      displayName: `Node ${id}`,
      resourceType: id === 14 ? "database_cluster" : "database_instance",
      resourceSubtype: "mysql",
      healthStatus: "healthy",
      lifecycleStatus: "running",
      topologyRole: id === 14 ? "cluster" : "replica",
      topologyLayer: id === 14 ? "cluster" : "replication",
      isDatabaseTopology: true,
      visualImportance: 5,
    })),
    edges: [],
  };
}

describe("database read-model consistency", () => {
  it("returns ok for a cluster whose members, relations, and topology agree", () => {
    const result = buildClusterConsistency({
      resource: clusterResource(),
      members: [member()],
      topology: topology([14, 22]),
    });

    expect(result.status).toBe("ok");
    expect(result.issues).toHaveLength(0);
    expect(result.counts.members).toBe(1);
    expect(result.counts.topologyDatabaseNodes).toBe(2);
  });

  it("reports missing member role", () => {
    const result = buildClusterConsistency({
      resource: clusterResource(),
      members: [member({ profileSummary: { hostname: "db", port: 3306 } })],
      topology: topology([14, 22]),
    });

    expect(result.status).toBe("warning");
    expect(result.issues).toContainEqual(
      expect.objectContaining({
        id: "member-role-missing-22",
        kind: "missing_profile",
        messageKey: "databaseConsistency.issues.memberRoleMissing",
      }),
    );
  });

  it("reports member missing from topology", () => {
    const result = buildClusterConsistency({
      resource: clusterResource(),
      members: [member()],
      topology: topology([14]),
    });

    expect(result.status).toBe("warning");
    expect(result.issues).toContainEqual(
      expect.objectContaining({
        id: "member-missing-from-topology-22",
        kind: "topology_mismatch",
      }),
    );
  });

  it("reports topology-only database instance node", () => {
    const result = buildClusterConsistency({
      resource: clusterResource(),
      members: [member()],
      topology: topology([14, 22, 23]),
    });

    expect(result.status).toBe("warning");
    expect(result.issues).toContainEqual(
      expect.objectContaining({
        id: "topology-only-node-23",
        kind: "topology_mismatch",
      }),
    );
  });

  it("returns ok for an instance with parent cluster, role, connection, and topology", () => {
    const result = buildInstanceConsistency({
      resource: instanceResource(),
      topology: topology([14, 22]),
    });

    expect(result.status).toBe("ok");
    expect(result.issues).toHaveLength(0);
    expect(result.facts.role).toBe("primary");
    expect(result.facts.connection).toBe("prod-db-host-02.internal:3307");
  });

  it("reports missing instance parent cluster", () => {
    const resource = instanceResource();
    resource.clusterInfo = undefined;

    const result = buildInstanceConsistency({
      resource,
      topology: topology([14, 22]),
    });

    expect(result.status).toBe("warning");
    expect(result.issues).toContainEqual(
      expect.objectContaining({
        id: "instance-parent-cluster-missing",
        kind: "missing_relation",
      }),
    );
  });

  it("reports missing instance connection", () => {
    const resource = instanceResource();
    resource.profileSummary = { role: "replica", engine: "mysql" };

    const result = buildInstanceConsistency({
      resource,
      topology: topology([14, 22]),
    });

    expect(result.status).toBe("warning");
    expect(result.issues).toContainEqual(
      expect.objectContaining({
        id: "instance-connection-missing",
        kind: "missing_profile",
      }),
    );
  });
});
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
npm run test -- tests/lib/database-read-model-consistency.test.ts
```

Expected: fail because `@/lib/database-read-model-consistency` does not exist.

---

## Task 2: Implement Pure Consistency Helpers

**Files:**
- Create: `lib/database-read-model-consistency.ts`
- Test: `tests/lib/database-read-model-consistency.test.ts`

- [ ] **Step 1: Add helper implementation**

Create `lib/database-read-model-consistency.ts`:

```ts
import type { ClusterMember, TopologyResponse } from "@/types/resource";
import type { ResourceDetailViewModel } from "@/types/view-models";

export type ConsistencyStatus = "ok" | "warning" | "unknown";

export type ConsistencyIssueKind =
  | "missing_profile"
  | "missing_relation"
  | "topology_mismatch";

export type ConsistencyIssue = {
  id: string;
  kind: ConsistencyIssueKind;
  severity: "warning" | "unknown";
  messageKey: string;
  resourceId?: number;
  resourceName?: string;
};

export type ClusterConsistencyResult = {
  status: ConsistencyStatus;
  counts: {
    members: number;
    topologyDatabaseNodes: number;
  };
  issues: ConsistencyIssue[];
};

export type InstanceConsistencyResult = {
  status: ConsistencyStatus;
  facts: {
    parentClusterName?: string;
    role?: string;
    connection?: string;
  };
  issues: ConsistencyIssue[];
};

function hasConnection(profile: ClusterMember["profileSummary"]): boolean {
  return Boolean(profile?.hostname && profile.port != null);
}

function databaseInstanceTopologyIds(topology?: TopologyResponse): Set<number> {
  const ids = new Set<number>();
  for (const node of topology?.nodes ?? []) {
    if (node.resourceType === "database_instance") {
      ids.add(Number(node.id));
    }
  }
  return ids;
}

function toStatus(issues: ConsistencyIssue[]): ConsistencyStatus {
  if (issues.length === 0) {
    return "ok";
  }
  return issues.some((issue) => issue.severity === "warning")
    ? "warning"
    : "unknown";
}

export function buildClusterConsistency({
  resource,
  members,
  topology,
}: {
  resource: ResourceDetailViewModel;
  members: ClusterMember[];
  topology?: TopologyResponse;
}): ClusterConsistencyResult {
  const issues: ConsistencyIssue[] = [];
  const topologyInstanceIds = databaseInstanceTopologyIds(topology);
  const memberIds = new Set(members.map((member) => member.id));

  for (const member of members) {
    if (!member.profileSummary?.role) {
      issues.push({
        id: `member-role-missing-${member.id}`,
        kind: "missing_profile",
        severity: "warning",
        messageKey: "databaseConsistency.issues.memberRoleMissing",
        resourceId: member.id,
        resourceName: member.displayName,
      });
    }

    if (!hasConnection(member.profileSummary)) {
      issues.push({
        id: `member-connection-missing-${member.id}`,
        kind: "missing_profile",
        severity: "warning",
        messageKey: "databaseConsistency.issues.memberConnectionMissing",
        resourceId: member.id,
        resourceName: member.displayName,
      });
    }

    if (topology && !topologyInstanceIds.has(member.id)) {
      issues.push({
        id: `member-missing-from-topology-${member.id}`,
        kind: "topology_mismatch",
        severity: "warning",
        messageKey: "databaseConsistency.issues.memberMissingFromTopology",
        resourceId: member.id,
        resourceName: member.displayName,
      });
    }
  }

  if (topology) {
    for (const topologyId of topologyInstanceIds) {
      if (!memberIds.has(topologyId)) {
        issues.push({
          id: `topology-only-node-${topologyId}`,
          kind: "topology_mismatch",
          severity: "warning",
          messageKey: "databaseConsistency.issues.topologyOnlyNode",
          resourceId: topologyId,
        });
      }
    }
  }

  return {
    status: toStatus(issues),
    counts: {
      members: members.length,
      topologyDatabaseNodes: topologyInstanceIds.size + (topology ? 1 : 0),
    },
    issues,
  };
}

export function buildInstanceConsistency({
  resource,
  topology,
}: {
  resource: ResourceDetailViewModel;
  topology?: TopologyResponse;
}): InstanceConsistencyResult {
  const issues: ConsistencyIssue[] = [];
  const role = resource.profileSummary?.role;
  const hostname = resource.profileSummary?.hostname;
  const port = resource.profileSummary?.port;

  if (!resource.clusterInfo) {
    issues.push({
      id: "instance-parent-cluster-missing",
      kind: "missing_relation",
      severity: "warning",
      messageKey: "databaseConsistency.issues.instanceParentClusterMissing",
      resourceId: resource.id,
      resourceName: resource.displayName,
    });
  }

  if (!role) {
    issues.push({
      id: "instance-role-missing",
      kind: "missing_profile",
      severity: "warning",
      messageKey: "databaseConsistency.issues.instanceRoleMissing",
      resourceId: resource.id,
      resourceName: resource.displayName,
    });
  }

  if (!hostname || port == null) {
    issues.push({
      id: "instance-connection-missing",
      kind: "missing_profile",
      severity: "warning",
      messageKey: "databaseConsistency.issues.instanceConnectionMissing",
      resourceId: resource.id,
      resourceName: resource.displayName,
    });
  }

  if (topology) {
    const appearsInTopology = topology.nodes.some((node) => Number(node.id) === resource.id);
    if (!appearsInTopology) {
      issues.push({
        id: "instance-missing-from-topology",
        kind: "topology_mismatch",
        severity: "warning",
        messageKey: "databaseConsistency.issues.instanceMissingFromTopology",
        resourceId: resource.id,
        resourceName: resource.displayName,
      });
    }
  }

  return {
    status: toStatus(issues),
    facts: {
      parentClusterName: resource.clusterInfo?.displayName,
      role,
      connection: hostname && port != null ? `${hostname}:${port}` : undefined,
    },
    issues,
  };
}
```

- [ ] **Step 2: Run helper tests**

Run:

```bash
npm run test -- tests/lib/database-read-model-consistency.test.ts
```

Expected: pass.

- [ ] **Step 3: Commit helper**

```bash
git add lib/database-read-model-consistency.ts tests/lib/database-read-model-consistency.test.ts
git commit -m "feat: add database read-model consistency helpers"
```

---

## Task 3: Add Consistency Panel Component

**Files:**
- Create: `components/resources/database-consistency-panel.tsx`
- Create: `tests/components/database-consistency-panel.test.tsx`
- Modify later: `messages/en.json`
- Modify later: `messages/zh-CN.json`

- [ ] **Step 1: Write component tests**

Create `tests/components/database-consistency-panel.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { ClusterConsistencyResult } from "@/lib/database-read-model-consistency";

function t(key: string, params?: Record<string, number | string>) {
  const keys: Record<string, string> = {
    "title": "Data consistency",
    "description": "Read-only check across members, relations, topology, and profile data.",
    "status.ok": "Data consistent",
    "status.warning": "Needs data review",
    "status.unknown": "Not enough data",
    "counts": "{members} members · {topologyDatabaseNodes} topology database nodes",
    "issues.memberRoleMissing": "Backend did not provide role information.",
    "issues.memberMissingFromTopology": "Topology does not include this member.",
  };
  let result = keys[key] ?? key;
  if (params) {
    for (const [name, value] of Object.entries(params)) {
      result = result.replace(`{${name}}`, String(value));
    }
  }
  return result;
}

vi.mock("next-intl", () => ({
  useTranslations: () => t,
}));

describe("DatabaseConsistencyPanel", () => {
  it("renders compact ok state", async () => {
    const { DatabaseConsistencyPanel } = await import(
      "@/components/resources/database-consistency-panel"
    );

    const result: ClusterConsistencyResult = {
      status: "ok",
      counts: { members: 2, topologyDatabaseNodes: 3 },
      issues: [],
    };

    render(<DatabaseConsistencyPanel result={result} />);

    expect(screen.getByText("Data consistency")).toBeInTheDocument();
    expect(screen.getByText("Data consistent")).toBeInTheDocument();
    expect(screen.getByText("2 members · 3 topology database nodes")).toBeInTheDocument();
  });

  it("renders warning issues", async () => {
    const { DatabaseConsistencyPanel } = await import(
      "@/components/resources/database-consistency-panel"
    );

    const result: ClusterConsistencyResult = {
      status: "warning",
      counts: { members: 1, topologyDatabaseNodes: 1 },
      issues: [
        {
          id: "member-role-missing-22",
          kind: "missing_profile",
          severity: "warning",
          messageKey: "databaseConsistency.issues.memberRoleMissing",
          resourceName: "Payment MySQL Replica",
        },
      ],
    };

    render(<DatabaseConsistencyPanel result={result} />);

    expect(screen.getByText("Needs data review")).toBeInTheDocument();
    expect(screen.getByText(/Payment MySQL Replica/)).toBeInTheDocument();
    expect(screen.getByText(/Backend did not provide role information/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
npm run test -- tests/components/database-consistency-panel.test.tsx
```

Expected: fail because component does not exist.

- [ ] **Step 3: Implement component**

Create `components/resources/database-consistency-panel.tsx`:

```tsx
"use client";

import { useTranslations } from "next-intl";

import { DetailPanel } from "@/components/blocks/detail-panel";
import { cn } from "@/lib/utils";
import type {
  ClusterConsistencyResult,
  ConsistencyStatus,
} from "@/lib/database-read-model-consistency";

const statusTone: Record<ConsistencyStatus, string> = {
  ok: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  warning: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
  unknown: "bg-muted text-muted-foreground",
};

function localKey(key: string): string {
  return key.replace("databaseConsistency.", "");
}

export function DatabaseConsistencyPanel({
  result,
}: {
  result: ClusterConsistencyResult;
}) {
  const t = useTranslations("databaseConsistency");

  return (
    <DetailPanel title={t("title")} description={t("description")}>
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <span
            data-consistency-status={result.status}
            className={cn(
              "rounded-md px-2 py-1 text-xs font-semibold",
              statusTone[result.status],
            )}
          >
            {t(`status.${result.status}`)}
          </span>
          <span className="text-sm text-muted-foreground">
            {t("counts", result.counts)}
          </span>
        </div>

        {result.issues.length > 0 ? (
          <ul className="space-y-2">
            {result.issues.map((issue) => (
              <li
                key={issue.id}
                data-consistency-issue-kind={issue.kind}
                className="rounded-lg border border-border bg-background px-3 py-2 text-sm"
              >
                <span className="font-medium text-foreground">
                  {issue.resourceName ?? issue.resourceId ?? ""}
                </span>
                <span className="ml-2 text-muted-foreground">
                  {t(localKey(issue.messageKey))}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">{t("allSignalsAgree")}</p>
        )}
      </div>
    </DetailPanel>
  );
}
```

- [ ] **Step 4: Add i18n keys**

In `messages/en.json`, add under root:

```json
"databaseConsistency": {
  "title": "Data consistency",
  "description": "Read-only check across members, relations, topology, and profile data.",
  "allSignalsAgree": "All visible database signals agree.",
  "counts": "{members} members · {topologyDatabaseNodes} topology database nodes",
  "status": {
    "ok": "Data consistent",
    "warning": "Needs data review",
    "unknown": "Not enough data"
  },
  "issues": {
    "memberRoleMissing": "Backend did not provide role information.",
    "memberConnectionMissing": "Backend did not provide host or port information.",
    "memberMissingFromTopology": "Topology does not include this member.",
    "topologyOnlyNode": "Topology includes a database node that is not in the member table.",
    "instanceParentClusterMissing": "Parent cluster information is missing.",
    "instanceRoleMissing": "Backend did not provide instance role information.",
    "instanceConnectionMissing": "Backend did not provide instance host or port information.",
    "instanceMissingFromTopology": "Topology does not include this instance."
  }
}
```

In `messages/zh-CN.json`, add matching keys:

```json
"databaseConsistency": {
  "title": "数据一致性",
  "description": "只读检查成员、关系、拓扑和画像数据是否一致。",
  "allSignalsAgree": "当前可见的数据库信号一致。",
  "counts": "{members} 个成员 · {topologyDatabaseNodes} 个拓扑数据库节点",
  "status": {
    "ok": "数据一致",
    "warning": "需要数据复核",
    "unknown": "数据不足"
  },
  "issues": {
    "memberRoleMissing": "后端未提供角色信息。",
    "memberConnectionMissing": "后端未提供主机或端口信息。",
    "memberMissingFromTopology": "拓扑未包含该成员。",
    "topologyOnlyNode": "拓扑包含成员表中不存在的数据库节点。",
    "instanceParentClusterMissing": "缺少所属集群信息。",
    "instanceRoleMissing": "后端未提供实例角色信息。",
    "instanceConnectionMissing": "后端未提供实例主机或端口信息。",
    "instanceMissingFromTopology": "拓扑未包含该实例。"
  }
}
```

- [ ] **Step 5: Run tests**

Run:

```bash
npm run test -- tests/components/database-consistency-panel.test.tsx
```

Expected: pass.

- [ ] **Step 6: Commit component**

```bash
git add components/resources/database-consistency-panel.tsx tests/components/database-consistency-panel.test.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: render database consistency panel"
```

---

## Task 4: Add Instance Context Panel

**Files:**
- Create: `components/resources/database-instance-context-panel.tsx`
- Create: `tests/components/database-instance-context-panel.test.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Write component tests**

Create `tests/components/database-instance-context-panel.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { InstanceConsistencyResult } from "@/lib/database-read-model-consistency";

function t(key: string) {
  const keys: Record<string, string> = {
    "instanceContext.title": "Instance context",
    "instanceContext.description": "Parent cluster, role, and connection facts.",
    "instanceContext.parentCluster": "Parent cluster",
    "instanceContext.role": "Role",
    "instanceContext.connection": "Connection",
    "instanceContext.missing": "Not provided by backend",
  };
  return keys[key] ?? key;
}

vi.mock("next-intl", () => ({
  useTranslations: () => t,
}));

describe("DatabaseInstanceContextPanel", () => {
  it("renders parent cluster, role, and connection", async () => {
    const { DatabaseInstanceContextPanel } = await import(
      "@/components/resources/database-instance-context-panel"
    );

    const result: InstanceConsistencyResult = {
      status: "ok",
      facts: {
        parentClusterName: "Payment MySQL Cluster Production",
        role: "primary",
        connection: "prod-db-host-02.internal:3307",
      },
      issues: [],
    };

    render(<DatabaseInstanceContextPanel result={result} />);

    expect(screen.getByText("Instance context")).toBeInTheDocument();
    expect(screen.getByText("Payment MySQL Cluster Production")).toBeInTheDocument();
    expect(screen.getByText("primary")).toBeInTheDocument();
    expect(screen.getByText("prod-db-host-02.internal:3307")).toBeInTheDocument();
  });

  it("renders explicit missing value text", async () => {
    const { DatabaseInstanceContextPanel } = await import(
      "@/components/resources/database-instance-context-panel"
    );

    const result: InstanceConsistencyResult = {
      status: "warning",
      facts: {},
      issues: [],
    };

    render(<DatabaseInstanceContextPanel result={result} />);

    expect(screen.getAllByText("Not provided by backend").length).toBeGreaterThan(0);
  });
});
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```bash
npm run test -- tests/components/database-instance-context-panel.test.tsx
```

Expected: fail because component does not exist.

- [ ] **Step 3: Implement component**

Create `components/resources/database-instance-context-panel.tsx`:

```tsx
"use client";

import { useTranslations } from "next-intl";

import { DetailPanel } from "@/components/blocks/detail-panel";
import type { InstanceConsistencyResult } from "@/lib/database-read-model-consistency";

function Fact({ label, value }: { label: string; value?: string }) {
  const t = useTranslations("databaseConsistency");
  return (
    <div className="rounded-lg border border-border bg-background px-3 py-2">
      <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground">
        {label}
      </p>
      <p className="mt-1 text-sm font-medium text-foreground">
        {value || t("instanceContext.missing")}
      </p>
    </div>
  );
}

export function DatabaseInstanceContextPanel({
  result,
}: {
  result: InstanceConsistencyResult;
}) {
  const t = useTranslations("databaseConsistency");

  return (
    <DetailPanel
      title={t("instanceContext.title")}
      description={t("instanceContext.description")}
    >
      <div className="grid gap-3 md:grid-cols-3">
        <Fact
          label={t("instanceContext.parentCluster")}
          value={result.facts.parentClusterName}
        />
        <Fact label={t("instanceContext.role")} value={result.facts.role} />
        <Fact
          label={t("instanceContext.connection")}
          value={result.facts.connection}
        />
      </div>
    </DetailPanel>
  );
}
```

- [ ] **Step 4: Add i18n keys**

Add to `databaseConsistency` in `messages/en.json`:

```json
"instanceContext": {
  "title": "Instance context",
  "description": "Parent cluster, role, and connection facts.",
  "parentCluster": "Parent cluster",
  "role": "Role",
  "connection": "Connection",
  "missing": "Not provided by backend"
}
```

Add to `databaseConsistency` in `messages/zh-CN.json`:

```json
"instanceContext": {
  "title": "实例上下文",
  "description": "所属集群、角色和连接信息。",
  "parentCluster": "所属集群",
  "role": "角色",
  "connection": "连接",
  "missing": "后端未提供"
}
```

- [ ] **Step 5: Run component tests**

Run:

```bash
npm run test -- tests/components/database-instance-context-panel.test.tsx
```

Expected: pass.

- [ ] **Step 6: Commit instance panel**

```bash
git add components/resources/database-instance-context-panel.tsx tests/components/database-instance-context-panel.test.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: add database instance context panel"
```

---

## Task 5: Wire Panels Into Resource Detail Page

**Files:**
- Modify: `app/(console)/resources/[id]/page.tsx`
- Test: `tests/resource-detail-page.test.tsx`

- [ ] **Step 1: Inspect existing topology data flow**

Run:

```bash
rg -n "TopologyPanel|topology|DatabaseDecisionDeck|DatabaseOperatorWorkbench" app components lib tests/resource-detail-page.test.tsx
```

Expected: identify where resource topology is fetched and where Phase 22B deck
is rendered.

- [ ] **Step 2: Add page tests**

Update `tests/resource-detail-page.test.tsx` with tests that assert:

```tsx
expect(screen.getByText("Data consistency")).toBeInTheDocument();
expect(screen.getByText("Instance context")).toBeInTheDocument();
```

Use existing resource detail page mocks. For cluster fixtures, assert data
consistency appears. For instance fixtures, assert both instance context and
data consistency appear.

- [ ] **Step 3: Run page tests and verify failure**

Run:

```bash
npm run test -- tests/resource-detail-page.test.tsx
```

Expected: fail because panels are not rendered yet.

- [ ] **Step 4: Wire implementation**

In `app/(console)/resources/[id]/page.tsx`:

1. Import helpers and components:

```tsx
import { DatabaseConsistencyPanel } from "@/components/resources/database-consistency-panel";
import { DatabaseInstanceContextPanel } from "@/components/resources/database-instance-context-panel";
import {
  buildClusterConsistency,
  buildInstanceConsistency,
} from "@/lib/database-read-model-consistency";
```

2. Compute results after topology data is available:

```tsx
const isDatabaseCluster = resource.resourceType === "database_cluster";
const isDatabaseInstance = resource.resourceType === "database_instance";
const isDatabaseResource = isDatabaseCluster || isDatabaseInstance;

const clusterConsistency = isDatabaseCluster
  ? buildClusterConsistency({
      resource,
      members: resource.members ?? [],
      topology,
    })
  : null;

const instanceConsistency = isDatabaseInstance
  ? buildInstanceConsistency({
      resource,
      topology,
    })
  : null;
```

Use the actual variable names already present in the page. Do not introduce a
second topology fetch. If the page stores topology under a different variable,
pass that existing response.

3. Render below topology:

```tsx
{isDatabaseCluster && clusterConsistency ? (
  <DatabaseConsistencyPanel result={clusterConsistency} />
) : null}

{isDatabaseInstance && instanceConsistency ? (
  <>
    <DatabaseInstanceContextPanel result={instanceConsistency} />
    <DatabaseConsistencyPanel
      result={{
        status: instanceConsistency.status,
        counts: { members: 0, topologyDatabaseNodes: 0 },
        issues: instanceConsistency.issues,
      }}
    />
  </>
) : null}
```

If `DatabaseConsistencyPanel` needs a more generic result type after actual
implementation, update its prop type deliberately and keep tests aligned.

- [ ] **Step 5: Run page tests**

Run:

```bash
npm run test -- tests/resource-detail-page.test.tsx
```

Expected: pass.

- [ ] **Step 6: Commit wiring**

```bash
git add app/(console)/resources/[id]/page.tsx tests/resource-detail-page.test.tsx
git commit -m "feat: add database consistency to resource detail"
```

---

## Task 6: Update E2E Workflow

**Files:**
- Modify: `e2e/operator-database-workflow.spec.ts`

- [ ] **Step 1: Add E2E assertions**

Update the existing workflow tests:

For `/resources/14` cluster:

```ts
await expect(page.locator("h3", { hasText: /Data consistency|数据一致性/i })).toBeVisible();
await expect(page.locator("[data-consistency-status]")).toBeVisible();
```

For `/resources/22` instance:

```ts
await expect(page.locator("h3", { hasText: /Instance context|实例上下文/i })).toBeVisible();
await expect(page.locator("h3", { hasText: /Data consistency|数据一致性/i })).toBeVisible();
```

Keep existing deck assertions:

- `/resources/22` remains compact.
- `/resources/14` remains diagnostic.
- topology remains visible.

- [ ] **Step 2: Run targeted E2E**

Ensure backend is running on `:8080` and port `3000` is free, then run:

```bash
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
```

Expected: pass.

- [ ] **Step 3: Commit E2E**

```bash
git add e2e/operator-database-workflow.spec.ts
git commit -m "test: cover database read-model consistency workflow"
```

---

## Task 7: Full Verification And Closeout

**Files:**
- No code files unless verification reveals issues.

- [ ] **Step 1: Run static and unit checks**

Run:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Expected:

- governance passes
- TypeScript passes
- lint passes
- unit tests pass
- build succeeds

- [ ] **Step 2: Run E2E gates**

Run:

```bash
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

Expected: all pass. If backend is not running, start it from
`/Users/fan/GolangProjects/ControlHub` with:

```bash
go run ./cmd/server
```

Stop any server process you start after verification.

- [ ] **Step 3: Live browser verification**

Start frontend from `/Users/fan/JsProjects/ControlHub`:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Verify:

- `/resources/14`
  - diagnostic deck remains visible
  - topology follows the deck
  - data consistency panel appears below topology
  - no console errors
- `/resources/22`
  - compact healthy deck remains compact
  - no topology button in compact deck
  - instance context panel appears
  - data consistency panel appears
  - no console errors

- [ ] **Step 4: Run repository status**

Run:

```bash
git status --short --branch
git log --oneline -5
```

Expected: clean working tree after commits.

- [ ] **Step 5: Final report**

Report:

- commit hashes
- files changed
- exact consistency rules implemented
- `/resources/14` live result
- `/resources/22` live result
- full verification matrix
- scope confirmation:
  - no backend changes
  - no API contract changes
  - no SQL
  - no work orders
  - no write operations
  - no topology editing
  - no tag/push/release
  - no AI co-author

