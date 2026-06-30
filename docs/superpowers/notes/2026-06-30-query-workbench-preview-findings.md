# Query Workbench Preview Findings — 2026-06-30

## Context

Manual preview was run after Phase 38B frontend and backend evidence were merged.
The preview used the local development stack:

- frontend: `ControlHub-Frontend` main at `d6a40e9`
- backend: `ControlHub-Backend` main at `4db3944`
- backend server: `http://localhost:8080`
- frontend server: `http://localhost:3100`
- dedicated query database: `controlhub-query-e2e-mysql` on `127.0.0.1:13306`
- ready query target: `616 / Local MySQL Query Dev`, `mysql`, `127.0.0.1:13306`, `run=true`, `readonly_sandbox_enabled`

The backend and dedicated query database proved that the query execution path can
work locally. The preview still exposed product and UI issues that should feed
the next Query Workbench hardening phase.

## Findings

### 1. Query target selector is too weak

The `/query` target selector is hard to use:

- target names are truncated or hard to scan;
- the selector does not support search;
- the ready local target is not easy to discover;
- target selection and target filtering are split across separate visual blocks.

This makes the page look locked even when a ready target exists. The selector
should become the primary target-picking control: searchable, readable, and able
to show target name, engine, environment, host:port, readiness, and run status.

### 2. Credential settings detail panel has ICU formatting errors

On `/settings/query-credentials`, selecting a target as an administrator raises
Next.js console errors:

```text
FORMATTING_ERROR: The intl string context variable "ref" was not provided
```

Affected message keys:

- `queryCredentialSettings.detail.credentialRefHint`
- `queryCredentialSettings.detail.boundaryNote`

The zh-CN strings contain `{ref}`, but `CredentialDetailPanel` calls
`t("detail.credentialRefHint")` and `t("detail.boundaryNote")` without values.

Preferred fix: avoid ICU placeholders for this copy and use literal example text
such as `CONTROLHUB_QUERY_CREDENTIAL_your-ref`. This prevents accidental exposure
or implication of a real credential reference.

### 3. `SHOW TABLES` is blocked by the SELECT-only guard

Running `show tables;` in the query worksheet returns:

```text
query validation failed: only a single SELECT statement is allowed
```

This is consistent with the Phase 37 backend boundary: only a single `SELECT` is
allowed. However, users expect database IDEs to help discover schema objects.

Do not solve this by broadly allowing `SHOW`. Safer options:

- add a dedicated schema browser backed by read-only metadata APIs;
- or explicitly whitelist a small set of metadata statements such as
  `SHOW TABLES` / `SHOW COLUMNS`, with guard, audit, and tests.

Short-term UI copy should clearly state: "Only SELECT statements are executable;
schema browsing is a separate capability."

### 4. Governance and access panel is too large for its value

The `/query` governance panel currently gives large permanent blocks to:

- execution disabled;
- missing read-only credential;
- audit required;
- JIT access planned.

These are useful facts, but not primary worksheet content. They should be
compressed into status labels with tooltip or popover details. Ready targets
should emphasize `Executable`, `Read-only sandbox`, and `Audited`; locked targets
should emphasize the one blocking reason.

### 5. Target facts are duplicated and occupy too much space

The query page repeats target metadata in multiple areas:

- engine;
- environment;
- host;
- owner;
- language;
- readiness;
- cluster.

The target selector, schema browser, and right-side facts panel overlap. The
workspace should prioritize editor, results, and history. Target facts should be
compact, collapsible, or shown only when diagnosing locked targets.

### 6. Query governance panel has a hydration mismatch

The `/query` page reports a React hydration mismatch around
`CredentialStatusSection`:

```text
server rendered: <p className="text-xs text-muted-foreground">
client rendered: <a href="/settings/query-credentials">
```

Root cause: `components/query/query-governance-panel.tsx` decides whether to show
the admin settings link from client-only role state during render. This is the
same class of bug previously fixed in the settings page.

Fix direction:

- do not read `window` / `sessionStorage` during render;
- initialize admin state as `null`;
- render the same stable output for SSR and the first client render;
- use `useEffect` to read `controlhub.role`;
- show the admin link only after the client confirms `role === "admin"`.

### 7. Worksheet editor is not IDE-like enough

The worksheet currently lacks common database IDE features:

- SQL syntax highlighting;
- SQL formatting;
- multiple worksheet tabs;
- worksheet naming or switching.

The next worksheet iteration should consider Monaco or CodeMirror, plus local
worksheet tabs. Persistence can remain deferred; local in-memory worksheets are
enough for the first tightening pass. Each worksheet must keep target context
clear to avoid executing an old statement against the wrong target.

## Proposed Priority

### Immediate bugs

1. Fix the query credential settings ICU `{ref}` formatting error.
2. Fix the query governance panel hydration mismatch.

### Query page usability

3. Replace the query target selector with a searchable target picker.
4. Compress governance and access facts into labels with tooltips.
5. Deduplicate target metadata and make secondary facts collapsible.

### Product follow-up

6. Add explicit SELECT-only guidance and plan a schema browser.
7. Upgrade the worksheet editor with highlighting, formatting, and multiple
   worksheets.

## Scope Notes

These findings do not require changing the Phase 37 SQL guard immediately.
The backend SELECT-only boundary remains valid. The primary issue is that the
frontend currently does not explain or structure that boundary in a way that
matches user expectations for a query workbench.
