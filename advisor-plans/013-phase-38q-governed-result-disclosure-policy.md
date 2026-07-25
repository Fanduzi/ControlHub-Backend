# Plan 013: Define the governed result-disclosure policy before expanding result-grid operations

> **Executor instructions**: This is a cross-repository design-and-delivery plan
> for a governance-sensitive capability. Follow the decision gates in order. Do
> not add UI-only masking, infer sensitivity from column names, or enable bulk
> clipboard/export behavior. If the decision record is not approved, stop after
> the documentation phase and report the unresolved policy choice.
>
> **Drift check (run first)**:
> ```bash
> git -C /Users/fan/GolangProjects/ControlHub diff --stat f0c6d81..HEAD -- internal/model internal/service internal/api internal/openapi
> git -C /Users/fan/JsProjects/ControlHub diff --stat 7a7f6fb..HEAD -- components/query services types tests e2e
> ```
> If result response, query guard, query-target access, or result-grid copy code
> has changed, compare the current code with the excerpts below. Stop if the
> proposed policy needs a different contract than this plan describes.

## Status

- **Priority**: P1
- **Effort**: L
- **Risk**: HIGH
- **Depends on**: Phase 38P (`f0c6d81` backend, `7a7f6fb` frontend)
- **Category**: direction, security, product design
- **Planned at**: backend `f0c6d81`, frontend `7a7f6fb`, 2026-07-23

## Why this matters

ControlHub already allows a user to copy a selected result cell to the browser
clipboard. The current backend result contract supplies only a column name,
database type, nullability, and raw cell values. It supplies neither source
provenance nor a server-owned classification or disclosure decision. Therefore
the browser cannot safely decide whether a value is sensitive, and styling a
cell as masked after the raw value reaches the browser would not be a security
boundary.

The accepted Phase 38I follow-up list calls for masking-aware result-grid
copy/navigation. This plan establishes the prerequisite: a backend-owned
result-disclosure contract. It deliberately does not turn ControlHub into a
general export tool or attempt to infer data sensitivity from names such as
`email`, `phone`, or `token`.

## Current state

### Existing result and copy path

- `internal/service/query_executor.go:110-164` scans a bounded MySQL result
  once for normal Run and governed related-record navigation. At line 131 it
  constructs `QueryDatabaseResult{Columns: columns, Rows: make([][]any, 0)}`.
- `internal/model/query_execution.go` defines the public query-result column
  and row shapes. There is no field for source provenance, classification,
  masking, or copy eligibility.
- `components/query/query-editor-shell.tsx:2213-2226` builds text from the
  selected cell and calls `copyToClipboard(text)` directly.
- `components/query/query-editor-shell.tsx:2249-2256` renders the selected-cell
  copy control. It has no policy input.
- `lib/clipboard.ts:9-18` only calls the browser Clipboard API and never sends
  a request. It is transport plumbing, not a governance decision point.
- `tests/components/query-workbench.test.tsx:2660-2815` intentionally verifies
  single-cell and header copy and states that bulk copy/export do not exist.
- `docs/superpowers/notes/2026-07-11-phase-38i-query-platform-product-design-review.md:38-40`
  records result-grid copy/navigation under a masking policy as deferred work;
  it explicitly rejects unrestricted Tabularis-style export.

### Conventions and constraints

- The backend resolves target access and credentials only in service code;
  `internal/service/query_target_access.go:20-27` keeps the DSN unexported.
- `internal/service/query_guard.go:65-101` is the only normal-query guard
  entrypoint. Do not relax it, add a browser SQL parser, or make copy depend on
  a client-supplied actor/role.
- `internal/service/query_execution_service.go:666-668` records fixed query
  audit outcomes. Audit records must never contain a copied value, raw SQL,
  credential material, DSN, or actor ID in public responses.
- Existing UI wording and test shape live in
  `components/query/query-editor-shell.tsx` and
  `tests/components/query-editor-shell.test.tsx`; use the same controlled-error
  and localized-message conventions.

## Locked product decision

The following decisions are deliberately fail-closed and are not open for an
executor to reinterpret:

1. **Default**: every result projection without an exact approved rule is
   `blocked`. The service rejects it before executing SQL and returns a fixed
   governance error. It does not return a raw value and ask the browser to hide
   it.
2. **Policy owner and scope**: only an administrator may manage persistent
   rules at exact `target + database + object + column` scope. No browser
   request may supply a policy, role, actor, or override. A target-wide allow
   rule is prohibited: a single table can contain both operational and
   sensitive columns.
3. **Display modes**: the public result-column contract uses exactly
   `raw_copy_allowed`, `masked_no_copy`, and `blocked`.
   `masked_no_copy` means the server transforms the value before JSON
   serialization, the browser renders that returned replacement, and clipboard
   copy is disabled. `blocked` rejects the query before execution rather than
   altering row shape by silently omitting a projected column.
4. **Projection boundary**: v1 permits only a single-table direct column,
   qualified direct column, or `*` that the server can expand against the
   governed schema exactly. Aliases may preserve a direct source column only
   when the parser proves that identity. Expressions, aggregates, joins,
   subqueries, derived tables, ambiguous columns, JSON paths, and UDF output
   are `blocked`; no heuristic name matching is permitted.

   **Literal-only no-FROM exemption**: Pure literal-only no-FROM SELECT
   projections (e.g., `SELECT 1`, `SELECT 'text'`, `SELECT NULL`) are
   intrinsically safe and returned as `raw_copy_allowed` without a
   table-column policy. This narrowly includes AST literal nodes with
   optional aliases. Non-literal expressions (functions, operators,
   variables, subqueries, etc.) remain blocked. Target access, read-only
   SQL guard, row cap, timeout, execution audit, and history remain
   enforced for these queries.
5. **Audit**: preserve the existing governed execution audit. Do not add a
   browser copy-audit endpoint: clipboard is a local browser action and cannot
   establish a trusted server-side exfiltration record. A future approval or
   export design must introduce its own trustworthy audit boundary.

The migration must make absence of a matching rule fail closed. The fixture
setup must seed explicit non-sensitive rules for its test tables; it must never
silently add a broad allow policy to production-like targets.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Backend unit gate | `go test -count=1 ./...` | exit 0, no failures or skips |
| Backend contract gate | `make openapi-validate` | exit 0 |
| Backend integration gate | `make test-integration` | exit 0, no failures or skips |
| Backend fuzz gate | `make test-openapi-fuzz` | exit 0 |
| Frontend typecheck | `npx tsc --noEmit -p tsconfig.json` | exit 0 |
| Frontend lint | `npm run lint` | 0 errors |
| Frontend unit gate | `npm run test` | exit 0, no failures or skips |
| Frontend build | `npm run build` | exit 0 |
| Frontend E2E governance | `npm run check:e2e-governance` | exit 0 |
| Frontend E2E | `npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts` | 0 failures, 0 skips |

## Scope

### In scope after policy approval

- A server-owned result-disclosure policy model, migration, admin-only policy
  management path, and OpenAPI contract.
- Query-result provenance for direct table columns, including a typed
  `displayMode` and `copyAllowed` decision emitted with every result column.
- Server-side masking before normal Run and related-record rows are serialized.
- Frontend rendering/copy behavior driven solely by the returned column
  decision, plus clear EN and zh-CN policy messaging.
- Fixed, value-free audit behavior if the approved decision requires it.
- Unit, integration, OpenAPI, component, and real E2E coverage for the
  approved policy.

### Explicitly out of scope

- CSV/JSON/export/download, copy-all, row-range copy, clipboard history, and
  browser-only redaction.
- Any new query engine, SQL guard widening, client SQL parsing, write/DDL
  execution, actor/role request fields, credential/DSN exposure, or changes to
  unrelated Object Explorer, Explain, relationship-map, or history behavior.
- Name-based rules such as masking every column called `email`; these are
  unreliable and must never become a hidden policy engine.
- Classifying arbitrary SQL expressions, UDF output, JSON paths,
  multi-table joins, or `SELECT *` as raw-copy-safe unless the approved
  provenance resolver supports them exactly. Unsupported projection forms must
  fail closed under the approved policy. Pure literal-only no-FROM SELECTs
  are exempted as intrinsically safe (see Projection boundary above).

## Git workflow

- Use separate backend and frontend worktrees from the exact merged bases.
- Branch names: `phase-38q-governed-result-disclosure-backend` and
  `phase-38q-governed-result-disclosure-ui`.
- Match conventional commit style, for example
  `feat(query): add governed result disclosure policy`.
- Do not amend, rebase, force-push, or touch the backend root's user-owned
  `.gitignore` and `advisor-plans/README.md` modifications.
- Do not push, merge, or release until both candidate reviews and all gates are
  green. Preserve the frontend WIP branch
  `wip/query-runtime-fixes-2026-07-20`.

## Steps

### Step 1: Write and approve the Phase 38Q specification and design

Create one tracked frontend spec and one tracked design document under
`docs/superpowers/specs/` and `docs/superpowers/plans/`. Copy the locked
product decision above without weakening it. Include an exact response schema,
policy precedence, the behavior of unsupported projections, migration
ownership, audit decision, and all non-goals.

The design must distinguish **display** from **copy**: a raw value may never be
sent solely to be hidden by the UI. Use the locked enums
`raw_copy_allowed`, `masked_no_copy`, and `blocked`; do not replace them with
booleans or alternate names.

**Verify**: both documents have no unresolved decision placeholders, preserve
the locked fail-closed defaults, and are reviewed against the current execute
and related-record contracts.

### Step 2: Build the backend policy and provenance boundary

Add the smallest explicit persistence model and admin-only management path
needed by the approved policy. Apply the policy after target access and query
guard validation but before rows leave `MySQLQueryExecutor`/the execution
service response boundary. Preserve the Phase 38P invariant that successful
responses always return `rows: []`, never `null`.

For direct table columns, resolve source identity from the parsed SQL AST and
schema metadata. Do not derive a policy from user-provided aliases. If the
projection cannot be resolved exactly, follow the approved fail-closed mode.
Related-record queries have service-owned source metadata and must receive the
same policy through the shared scanner path.

**Verify**: add focused model/service tests proving no raw sensitive value is
serialized for a masked column, no unsupported projection becomes copyable,
and normal valid raw-copy-allowed data retains its value and row shape.

### Step 3: Add safe API, audit, and integration proofs

Document exact result-column disclosure fields in OpenAPI. Ensure request
payloads never accept a policy decision, actor id, role, copied value, raw SQL,
or credential data. If copy auditing is approved, record a fixed typed event
without value/SQL/DSN/credential/error text.

Use disposable integration MySQL data that contains a known policy-protected
column. Prove normal Run and related-record navigation both return masked data
before JSON serialization and preserve result caps. Prove errors are controlled
and no raw value appears in JSON, audit event fields, or handler errors.

**Verify**: `make openapi-validate`, `make test-integration`, and
`make test-openapi-fuzz` all exit 0 with no skip/failure.

### Step 4: Make the frontend obey only the server decision

Extend frontend result types/services with the backend disclosure enum. Update
`ResultTable` and the related-record panel so that:

- raw-copy-allowed cells use the existing single-cell copy control;
- masked cells display the server-supplied masked representation and have no
  value-bearing accessible label or clipboard operation;
- blocked/error policy states render localized controlled feedback;
- header copy remains metadata-only and never implies the column's values are
  copyable.

Do not generate a mask in React and do not place an unmasked string in title,
aria-label, test id, toast, console output, or clipboard text. Keep the
existing keyboard cell navigation and FK control behavior; if a masked FK value
cannot safely form navigation input, disable that navigation with an explicit
localized reason rather than sending the value.

**Verify**: component tests assert clipboard calls only for
`raw_copy_allowed`, never receive raw protected values, and preserve keyboard
navigation/focus behavior for allowed cells.

### Step 5: Prove the real governed workflow

Extend the dedicated query E2E fixture with non-sensitive sentinel data and a
policy-protected field only after the policy schema is approved. Do not place
credentials or real sensitive values in fixtures or test reports. Run desktop
EN, 375px mobile EN, and desktop zh-CN cases against a ready MySQL target.

E2E must prove all of the following without route mocks, force clicks,
`page.evaluate`, broad timeout changes, or test skips:

- raw-copy-allowed cell copies the expected non-sensitive fixture value;
- masked cell never exposes the raw fixture value in visible UI, accessible
  names, clipboard calls, network request bodies, or query history;
- related-record navigation follows the approved policy for a protected FK;
- no export/download/copy-all capability appears;
- exactly the expected governed requests occur, with no browser N+1 policy
  lookup and no credential/result-value disclosure.

**Verify**: run the full listed E2E command three consecutive times from the
exact final candidate SHAs. Each run must report 0 failed and 0 skipped.

### Step 6: Independent reviews and release

Before merge, run Momus with the absolute on-disk plan path as its sole prompt.
Resolve every Momus P1/P2 and obtain `OKAY`. Then obtain an Oracle adversarial
review of the exact final backend and frontend candidate SHAs. Resolve every
P1/P2 and obtain `none P1/P2` before fast-forwarding either repository.

After merged-root gates and three clean E2E passes, fast-forward only, push
normally, wait for required CI success, preserve unrelated WIP, and write a
delivery-evidence note with only verified SHAs/results.

**Verify**: `HEAD == origin/main` in both repositories; CI required jobs are
success; no unowned root paths changed; evidence note names the exact reviewed
SHAs and does not claim unsupported engine/runtime cases as verified.

## Test plan

- Backend model/service tests: policy precedence, unknown/unresolvable
  projections, raw/masked/blocked result shapes, `rows: []` zero-row invariant,
  and no raw protected value in public JSON or fixed audit fields.
- Backend handler/OpenAPI tests: admin authorization, invalid policy input,
  no actor/policy decision accepted from normal execute or related-record
  requests, and stable error mapping.
- Backend integration tests: actual MySQL direct projection and service-owned
  related-record path under the policy; prove masking occurs before response.
- Frontend service/component tests: enum handling, clipboard prohibition,
  accessible names, header behavior, keyboard/focus behavior, controlled error
  state, and no legacy `rows: null` crash regression.
- E2E: desktop EN, mobile EN, zh-CN, one-request policy boundary, no value leak,
  no export/copy-all, and the existing workbench/credential regression suite.

Use `tests/components/query-editor-shell.test.tsx` as the direct pattern for
ResultTable/RelatedRecordsPanel behavior and
`internal/service/query_execution_service_test.go` plus the existing
integration query-execution tests for service and MySQL fixtures.

## Done criteria

- [ ] The locked product decision is copied unchanged into tracked 38Q
  spec/design docs.
- [ ] Policy decisions are server-owned; no browser-only masking or name-based
  classification exists.
- [ ] Raw protected values never cross the API boundary for masked/blocked
  columns, including related-record results.
- [ ] Clipboard receives only policy-allowed values; no copy-all/export/download
  capability is added.
- [ ] All backend and frontend commands in this plan pass at exact candidate
  HEADs with no failures/skips.
- [ ] Real E2E passes three consecutive times with 0 failed and 0 skipped.
- [ ] Momus is `OKAY` and Oracle reports `none P1/P2` for final candidates.
- [ ] Both repositories are fast-forward merged/pushed with required CI green;
  the evidence note is accurate to those final SHAs.

## STOP conditions

- A design proposes client-side masking after raw data has reached the browser.
- Accurate source provenance for a projection requires heuristic alias/name
  matching, an unsupported SQL form, or a browser SQL parser.
- The policy would require changing credentials, the query guard's read-only
  boundary, or sending actor/role/policy decisions from the browser.
- A test requires real sensitive data, a printed secret/DSN/token, test skips,
  broad timeout relaxation, route mocks, force clicks, or DOM bypasses.
- Existing copy behavior turns out to be an explicitly approved compliance
  exception; stop and obtain that evidence rather than silently overriding it.

## Maintenance notes

- Every future query engine must explicitly implement the disclosure boundary
  before `availableActions.run` can claim policy-aware result handling. Do not
  silently inherit MySQL assumptions.
- Any future result export, saved query sharing, AI assistance, or result
  history expansion must consume the same server-owned disclosure enum. It must
  not create a parallel masking policy.
- Reviewers should inspect aliases, joins, `SELECT *`, expressions, and FK
  navigation separately. The most dangerous regression is a raw value reaching
  any browser-visible field before the UI decides to hide it.
