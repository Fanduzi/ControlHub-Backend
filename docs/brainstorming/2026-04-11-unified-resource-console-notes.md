# Unified Resource Console Brainstorming Notes

## Purpose

This file records the confirmed baseline decisions from early product and architecture discussion.
It is a working note, not the final design spec.

## Product Positioning

- The product starts as a unified asset control console, not a SQL work order platform.
- SQL review, query workbench, and change workflows are upper-layer applications built on top of the asset model.
- The platform should feel like a professional control console for platform engineering, DBA, DevOps, and operations teams.

## Phase-1 Scope Direction

- Frontend and backend start together.
- Phase 1 focuses on foundation capabilities, especially asset management.
- Phase 1 asset source is primarily manual registration and manual maintenance.
- Phase 1 asset families are narrowed to:
  - Host
  - Database Instance
  - Database Cluster
  - Service
- Phase 1 interaction model for resource details:
  - Resource lists open a right-side detail panel for fast inspection
  - Deep inspection uses a dedicated detail page
  - The side panel is the default first-step interaction in list pages

## Layering

The platform should evolve in three layers:

1. Asset foundation
   - Unified resource identity
   - Environment
   - Owner
   - Labels and tags
   - Health and lifecycle status
   - Resource relations
   - Baseline audit events
2. Resource capability layer
   - Database instances
   - Clusters
   - Hosts
   - Middleware
   - Services
3. Upper-layer workbenches
   - SQL work orders
   - SQL review
   - Query tools
   - Change control

## Resource Modeling Direction

The recommended foundation is:

- Unified resource identity model
- Unified relation model
- Typed extension models by asset family

This means the platform should not use:

- One giant universal table for every asset detail
- Purely isolated per-product domain tables with no shared backbone
- EAV-style modeling as the core foundation

## Recommended Core Model

### 1. Resource Core

Every asset should have a shared resource record for stable common fields such as:

- id
- resource_type
- resource_subtype
- name
- display_name
- environment_id
- owner_id
- lifecycle_status
- health_status
- source
- external_id
- created_at
- updated_at

### 2. Typed Extensions

Resource-specific fields should live in typed extension models, for example:

- database instance profile
- cluster profile
- host profile
- service profile

High-frequency filter fields should be explicit columns.
Low-frequency or vendor-specific fields may live in a typed `spec` JSON field.

### 3. Resource Relations

Relations should be first-class records, not embedded ad hoc in type tables.

Examples:

- host runs database instance
- database instance belongs to cluster
- service depends on redis
- service connects to mysql

### 4. Query Projections

The system should support read-optimized projections for list pages and search.
The UI should not rely on expensive multi-table joins over the write model for every listing.

### 5. Status Layers

The model should separate:

- lifecycle status
- health status
- sync or freshness status

### 6. Audit Baseline

Even in phase 1, the foundation should leave room for:

- resource snapshots
- audit events

These will later support upper-layer change workflows.

## Current Open Questions

- Whether database assets start from instance-only or include cluster semantics immediately
- What minimum manual registration form is required for each asset family
- How strict the first version of ownership, environment, and relation validation should be
- Frontend framework baseline selection between React/Next and Vue/Nuxt

## Backend Architecture Direction

- The backend should not start with heavy DDD-style structure.
- Phase 1 should prefer a straightforward layered architecture:
  - HTTP/API layer
  - application/service layer
  - repository/data access layer
  - model/schema layer
- Domain boundaries still matter, but the implementation should stay pragmatic and easy to evolve.

## Frontend Framework Research Notes

### Confirmed Frontend Constraints

- The frontend will be built primarily by the agent in early phase.
- The user may make small follow-up modifications later.
- The frontend should not be based on a traditional admin template.
- The product shell must feel like a professional resource control console.
- The user likes the shadcn style and prefers open-code components.

### Compared Directions

#### A. Next.js + React + shadcn/ui

Strengths:

- Strongest ecosystem reach
- Original shadcn implementation and fastest upstream updates
- Mature examples for App Router, server components, and dashboard-like app shells

Weaknesses:

- Next.js App Router assumes React knowledge
- The mental model includes server/client component boundaries and React-first patterns
- Harder for a Vue-familiar maintainer to read and modify later

#### B. Nuxt + Vue 3 + shadcn-vue

Strengths:

- Better readability and approachability for a Vue-familiar maintainer
- Nuxt is a mature full-stack Vue framework with strong module ecosystem
- shadcn-vue has near-parity for the component categories needed by this project
- Good fit for building a custom product shell without inheriting admin-template constraints

Weaknesses:

- shadcn-vue is a port, not the original upstream implementation
- Ecosystem breadth is smaller than React + Next + shadcn/ui
- Some examples and community references are fewer than the React ecosystem

#### C. Nuxt + Vue 3 + Nuxt UI

Strengths:

- Strong official Vue/Nuxt-native UI ecosystem
- Large component set and good production ergonomics
- Very strong fallback if a custom shell project needs high-level Vue components quickly

Weaknesses:

- Visually and structurally it is a different UI system from the shadcn approach
- Mixing Nuxt UI and shadcn-vue as equal peers would risk design inconsistency

### Final Frontend Direction

Chosen frontend stack:

- Next.js
- React
- TypeScript
- Tailwind CSS
- shadcn/ui
- TanStack Table

Selection note:

- Although Vue-based options would reduce the user's learning barrier, the project will proceed with Next.js + shadcn/ui for stronger upstream alignment and broader ecosystem support.
- To reduce future maintenance cost, the frontend architecture should intentionally avoid unnecessarily complex React patterns.
- The implementation should favor clear app-shell boundaries, explicit server/client component usage, and readable state flow over clever abstractions.
