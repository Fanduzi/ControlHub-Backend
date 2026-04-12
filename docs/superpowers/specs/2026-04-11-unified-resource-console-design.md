# Unified Resource Console Design

## 1. Goal

Build the first phase of a unified resource console for platform engineering, DBA, DevOps, and operations teams.

This phase is not a SQL work order platform and not a generic admin dashboard.
It is the asset foundation that later supports higher-level capabilities such as SQL review, query workbench, and change control.

## 2. Product Positioning

- Product type: unified resource console
- Primary value: asset visibility, ownership, environment context, relations, and baseline auditability
- Interaction style: professional control console, not OA/CRM/admin-template style
- Initial users: platform engineers, DBAs, DevOps, operations engineers

## 3. Phase-1 Scope

### Included capabilities

- Login and basic role distinction
- Manual asset registration
- Manual asset editing
- Resource list filtering and search
- Resource detail viewing
- Right-side detail panel from list pages
- Dedicated detail page for deep inspection
- Resource relation maintenance
- Owner, environment, tag, and status maintenance
- Baseline audit event recording

### Excluded from phase 1

- SQL work orders
- SQL review workflow
- SQL query workbench
- Automatic discovery
- Batch import
- Fine-grained permission model
- Heavy topology visualization

## 4. Phase-1 Asset Families

- Host
- Database Instance
- Database Cluster
- Service

Database assets should include both cluster and instance as first-class concepts from the start.
The first version does not need full topology modeling, but cluster must exist as a logical resource container.

## 5. Product Information Architecture

### Primary navigation

- Overview
- Resources
- CMDB
- Databases
- Audits
- Settings

### Page roles

- `Overview`
  - Resource health summary
  - Pending attention items
  - Risk resources
  - Recent audit events
  - Should not become a four-card generic dashboard
- `Resources`
  - Unified resource list
  - Search and filters
  - Row click opens right-side detail panel
- `CMDB`
  - Configuration-oriented list and maintenance view over the same resource foundation
- `Databases`
  - Database-focused list and detail view for instances and clusters
- `Audits`
  - Audit event list and timeline
- `Settings`
  - Environments
  - Owners
  - Users
  - Roles
  - Supporting dictionaries

## 6. UI Direction

### Design principles

- One product shell reused everywhere
- One primary accent color only
- Consistent radius and spacing scale
- Minimal shadows; prefer border and background separation
- Dense, professional layout
- Tables first, cards second
- Detail information shown in sections and side panels
- Avoid large decorative dashboard cards
- Avoid template-admin visual language

### Detail interaction model

- Default list interaction: open right-side detail panel
- Deep inspection: open dedicated resource detail page
- Do not rely on modal-only detail views

### Frontend stack

- Next.js App Router
- TypeScript
- Tailwind CSS
- shadcn/ui
- TanStack Table
- React Hook Form
- Zod

### Frontend maintainability rule

Although the project uses React, implementation should avoid unnecessary complexity.
Prefer clear server/client boundaries, small client islands, readable component composition, and explicit data flow.

## 7. Frontend Structure

The frontend repository lives at `/Users/fan/JsProjects/ControlHub`.

Recommended top-level structure:

- `app/`
- `components/ui/`
- `components/app-shell/`
- `components/blocks/`
- `services/`
- `lib/`
- `hooks/`
- `types/`

### Responsibilities

- `app/`: routing, layouts, route-level pages
- `components/ui/`: shadcn/ui primitives and wrappers
- `components/app-shell/`: `AppShell`, `Sidebar`, `Topbar`
- `components/blocks/`: reusable product blocks such as `PageHeader`, `DataTableShell`, `DetailPanel`, `ActivityTimeline`, `ResourceRelationPanel`, `StatusBadge`, `EmptyState`
- `services/`: API client and resource-oriented request functions
- `lib/`: helpers, formatting, route constants, configuration
- `hooks/`: local reusable hooks only
- `types/`: frontend contract-aligned types

## 8. Backend Architecture

The backend repository lives at `/Users/fan/GolangProjects/ControlHub`.

### Architectural style

Do not start with heavy DDD-style architecture.
Use pragmatic layered architecture with clear domain boundaries.

Recommended backend layers:

- HTTP/API layer
- application/service layer
- repository/data access layer
- model/schema layer

### Initial backend modules

- `auth`
- `users`
- `roles`
- `environments`
- `owners`
- `resources`
- `relations`
- `audit`

### Suggested backend layout

- `cmd/server/`
- `internal/api/`
- `internal/service/`
- `internal/repository/`
- `internal/model/`
- `internal/openapi/`
- `migrations/`

## 9. API Contract Strategy

Frontend and backend should collaborate through `REST + OpenAPI`.

### Why

- Stable contract between two separate repositories
- Clear request and response shapes
- Easier frontend typing and mock generation later
- Reduced drift during iterative backend evolution

### Initial API groups

- `/auth/*`
- `/users`
- `/roles`
- `/environments`
- `/owners`
- `/resources`
- `/resources/{id}`
- `/resources/{id}/relations`
- `/resources/{id}/audit-events`
- `/audit-events`

Database-focused pages in phase 1 still consume the shared resource foundation rather than a separate database-only platform contract.

## 10. Data Modeling Strategy

The foundation should use:

- unified resource core
- unified relation model
- typed extension models by asset family
- read-friendly projections when needed

### Do not use as the foundation

- one giant universal asset table for all fields
- purely isolated per-product models with no shared backbone
- EAV as the primary model

### Core records

#### Resource core

Shared stable fields include:

- `id`
- `resource_type`
- `resource_subtype`
- `name`
- `display_name`
- `environment_id`
- `owner_id`
- `lifecycle_status`
- `health_status`
- `source`
- `external_id`
- `created_at`
- `updated_at`

#### Typed extensions

Separate typed profiles:

- `resource_profiles_host`
- `resource_profiles_database_instance`
- `resource_profiles_database_cluster`
- `resource_profiles_service`

Rules:

- High-frequency query fields should be explicit columns
- Low-frequency vendor-specific attributes may live in typed `spec` JSON fields
- JSON is supplemental, not the primary schema model

#### Relations

`resource_relations` should be first-class records.

Examples:

- host runs database instance
- database instance belongs to database cluster
- service depends on database instance
- service depends on database cluster

#### Database domain guardrails

For database-centric assets, the data model should preserve a clear separation
between logical database boundaries, running nodes, infrastructure carriers, and
supporting middleware/control components.

Rules:

- `database_cluster` is a logical service boundary, not a catch-all container
  for every related component.
- `database_instance` is the running database node or instance.
- `host` is the infrastructure carrier when a database instance actually runs on
  a VM or physical machine.
- Future carrier types such as containers, Pods, or cloud compute units should
  be modeled as independent resources rather than forced into `host`.
- Proxies such as ProxySQL should be modeled as independent resources, not as
  database instances.
- HA/control components such as Orchestrator should be modeled as independent
  resources, not as fields on `database_cluster`.
- Resource topology belongs in `resource_relations`, not in typed profile
  fields.
- Typed profiles describe intrinsic properties of the resource itself, not the
  full graph around it.

This separation is required so the model remains valid for:

- one host running multiple database instances
- container or Pod-backed deployments
- cloud RDS-style assets with no real host resource
- role or endpoint changes caused by failover

Practical interpretation for phase 1 and beyond:

- `database_instance -> member_of -> database_cluster`
- `database_instance -> runs_on -> host` when a real carrier exists
- `service -> depends_on -> database_instance` or `service -> depends_on -> database_cluster`
- future resource types may add relations such as:
  - `database_proxy -> fronts -> database_cluster`
  - `control_plane_component -> manages -> database_cluster`
  - `database_instance -> replicates_to -> database_instance`

Current source-of-truth rule:

- The authoritative relationship between a database instance and its carrier is
  the relation record such as `runs_on`.
- A typed profile field like `resource_profiles_database_instance.host` is
  supplemental display or connection metadata only; it must not become the sole
  topology source.

#### Status layers

Keep separate status concepts:

- lifecycle status
- health status
- sync/freshness status when introduced later

#### Audit baseline

Phase 1 should support at least:

- `audit_events`
- room for future `resource_snapshots`

The `audit_events` table in MySQL is a **phase-1 bootstrap/demo placeholder** only.
It has no foreign-key constraints on resource or user tables, so resource write
paths never depend on it transactionally.  The long-term backing store for audit
events is **ClickHouse**, which is better suited for append-only, high-write,
time-range queries.  The HTTP contract (`GET /audit-events`,
`GET /resources/{id}/audit-events`) will remain unchanged when the migration to
ClickHouse happens — only the repository implementation will be swapped.

## 11. Initial Persistence Outline

The first version should likely include these core tables:

- `users`
- `roles`
- `environments`
- `owners`
- `resources`
- `resource_relations`
- `resource_profiles_host`
- `resource_profiles_database_instance`
- `resource_profiles_database_cluster`
- `resource_profiles_service`
- `audit_events`

The final relational schema can evolve during implementation, but this is the intended shape.

## 12. Initial Screens

### A. Overview

- Compact summary sections
- Pending attention area
- Risk resources
- Recent audit events

### B. Unified Resource List

- Search
- Type filter
- Environment filter
- Status filter
- Table-first layout
- Right-side detail panel

### C. CMDB Resource View

- Same resource foundation, more configuration-oriented

### D. Resource Detail Page

- Basic info
- Tags
- Environment
- Owner
- Relations
- Related services and databases
- Recent changes
- Recent alerts placeholder
- Recent audit records

### E. Database View

- Database instances and clusters from shared resource base
- Search and filters
- Instance/cluster oriented columns

### F. Audit Log

- Event list or timeline
- Actor
- Target resource
- Event type
- Result status
- Filters

## 13. Recommended Delivery Order

1. Backend project skeleton and OpenAPI baseline
2. Frontend project skeleton and shared app shell
3. Core dictionaries: users, roles, environments, owners
4. Resource list and resource detail primitives
5. Resource relations and audit event baseline
6. Overview and database-focused views

## 14. Success Criteria for Phase 1

- A user can log in with a basic role
- A user can create and edit hosts, database instances, database clusters, and services
- A user can browse resources in a unified list
- A user can inspect a resource in a side panel and in a full detail page
- A user can maintain basic relations between resources
- A user can see baseline audit events
- The platform shell feels like a professional resource console rather than a generic admin dashboard
