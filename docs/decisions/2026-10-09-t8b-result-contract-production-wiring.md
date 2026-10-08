# Decision: T8-B Result-Contract Production Wiring

## Status

Accepted on 2026-10-09. This decision records three choices made while wiring
the accepted T8-A result contract (spec
`docs/superpowers/specs/2026-09-23-postgresql-query-workbench-v1.md`, G2/G7,
issue #116) into the three governed MySQL/TiDB result paths: ordinary execute,
saved-statements/execute, and related-record navigation. It does not reopen
the T8-A component contract and does not claim #116 complete — frontend
capability declaration and the PostgreSQL path remain for their owning tickets.

## Decisions

### 1. HTTP 400 for `result_contract_upgrade_required`

The frozen specification names the controlled error code but does not pin an
HTTP status. The refusal is mapped to **400** on all three governed result
endpoints because the inadequacy lives in the request: the client did not
declare the `cellTruncated` capability required to receive the page it asked
for. It is deliberately not 403 (nothing about the actor's authorization
changed — a retry with the capability declared succeeds) and not 409 (no
resource-state conflict). The refusal body carries only the fixed code and
message — no rows, no matrix, no partial results — and the already-committed
success evidence pair is never rewritten.

### 2. A valid matrix flag describes the *delivered* cell

`cellTruncated[i][j]` means "the value the client receives at row i, column j
is a truncated prefix of the scanned value." Disclosure masking replaces the
cell with the fixed `[MASKED]` placeholder (or preserves SQL NULL), so the
delivered value is never a truncated raw — `Apply` therefore clears the flag
on every `masked_no_copy` cell while keeping flags on `raw_copy_allowed`
cells. Clearing the flag grants nothing: `CopyAllowed=false` still denies CSV
export and clipboard semantics. Without this, a page whose only oversized
cells were masked would wrongly refuse capability-less clients, violating the
spec's clean-page compatibility rule.

### 3. Internal processing failure vs. post-finalize capability refusal

Two failure families share neither code nor evidence semantics:

- **Corrupt or missing truncation evidence** (a non-empty page with a nil or
  misaligned matrix, a residual matrix on a zero-row page) is an internal
  invariant violation. Detected before finalize (scanner or disclosure Apply),
  it records one terminal `failed` pair and returns the fixed
  `query_backend_error` (502). Detected post-finalize by the gate's defensive
  validation, the committed success pair stands and the same fixed backend
  error is returned — never a capability refusal.
- **Capability refusal** is post-finalize only: `DecideResultDelivery` runs
  after `persistAttempt`/`persistNavigationAttempt` commits the success pair.
  A refusal returns the zero response, writes no second history row, and never
  updates the recorded status. An evidence-persistence failure returns first
  (502), so a capability refusal can never mask it.

## Verification

- Six-group regression matrix: scanner (8191/8192/8193 + rune boundaries,
  NULL/typed cells, sentinel/budget-dropped row exclusion), page/payload
  budgets, disclosure flag semantics and ownership, all three entry paths,
  evidence ordering (fake-based unit tests plus real-MySQL counts of exactly
  one success history row and one audit event per attempt, and a
  trigger-injected pair rollback where 502 wins over the refusal), and
  HTTP/OpenAPI conformance.
- `go build ./...`, `go vet ./...`, `GOFLAGS=-count=1 make test`,
  `go test -race -count=1 ./internal/service ./internal/api`,
  `make openapi-validate`, `make test-integration` — all run at the delivery
  SHA.
