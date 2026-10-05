# pgsql Module

PostgreSQL governed read-only query front half: pure parse-tree transforms — no database access and no execution wiring. It implements spec `2026-09-23-postgresql-query-workbench-v1.md` stages G4/G5/G10-5a (issue #112/T4), the G6 execution-window rewrite (issue #113/T5), and the T7-S1 verification-record export used by the future executor. PostgreSQL execution remains disabled.

## Pipeline

```text
user SQL ──▶ GuardPG            (parse once; single SelectStmt; denylist; immutable RefID/RefPath collection)
         ──▶ QualifyEntities    (byte-offset "schema". insertion on entity refs only)
         ──▶ reparse            (qualified text must compile as written)
         ──▶ RefPath check      (original refs bind to canonical-tree RangeVar nodes)
         ──▶ PaginatePG         (paginated entry points only)
         ──▶ InjectWitnesses    (layout freeze → witness columns → DISTINCT Q+D → proof records)
         ──▶ deparse + reparse  (internal transport text, verified compilable)
```

`Rewrite` and `RewritePaginated` retain their existing signatures and run `GuardPG` internally. `RewriteFromGuardPaginated` is the thin cross-package seam for the future executor: it requires the exact same SQL and an unmodified `GuardResult` from `GuardPG`, validates the private statement/tree fingerprint and recollected refs, then reuses the same private pipeline. It never matches original refs to the canonical tree by traversal order or names — only by `RefPath`, with `RefEntity` → pinned-schema `RefQualified` as the allowed shape change.

Witness columns (`__chub_w<N>`, always NULL, typed as the source relation's `reltype`) attest which relation identity execution actually bound. They exist only inside the transport layout — never in user output, join keys, grouping keys, dedup keys, or stored SQL.

## T7-S1 verification records

Every original `RangeVar`, including each CTE use site, receives an immutable `RefID`. `RangeVarRef.Path` records the copied structural path used only for the canonical reparse check. `RefID` is stable input for a later binding gate; it is not replaced when witnesses merge.

`RewriteResult` exports the rewrite-time evidence set:

- `Refs`: original `RangeVarRef` values, including `RefID`, path, kind, name, alias, byte offset, and witnessability.
- `SourceCatalog`: attempt-local `SourceOccurrence` records. Original RangeVars use nonnegative IDs equal to `RefID`; derived tables, Q+D carriers, set-op carriers, and terminal non-entity carriers use generated negative IDs.
- `Public` / `PublicProofs`: `PublicProofs[i]` describes `Public[i]`; `TransportIndex` is the final transport-column index, so injected witnesses share the same coordinate system.
- `Witnesses` / `WitnessPositions`: each `WitnessRecord` carries `CoveredSources` (all original entity occurrences covered by the final position) and `CarrierOccurrence` (the propagation carrier that produced that position).

A public proof stores terminal `Dependencies` as `SourceRef` occurrence+slot pairs plus `FDOrigin` evidence. `FDOriginKnownColumn` records the expected `(RelationOID, AttributeNumber)` for a surviving direct Var. `FDOriginNoColumnOrigin` distinguishes a complete expression/non-column result; empty dependencies are valid only for constants/no-column inputs. `ProofUnresolved` means dependency or FD evidence is incomplete; it is never represented as an empty “complete” dependency set.

CTE and derived propagation preserve covered source IDs while updating the carrier. A repeated CTE use receives its own original CTE `RefID` but does not duplicate the CTE body's underlying entity occurrence. Set-operation output positions may merge compatible left/right `CoveredSources`, but the original entity `RefID`s remain distinct and the merged carrier is generated.

These records are rewrite-time expectations only. They do not prove that PostgreSQL bound, locked, typed, or returned any column; S2–S4 must compare them inside the governed transaction and independent observation path.

## Pagination (G6)

`RewritePaginated` and `RewriteFromGuardPaginated` run the pipeline above with `PaginatePG` applied to the reparsed qualified tree before witness injection, returning the `PageWindow` beside the `RewriteResult`. `PaginatePG` alone accepts only a single-root `SelectStmt` tree whose guard/qualification preconditions the caller already established; it validates all options and window arithmetic before touching the tree, then rewrites only the root `LimitCount`/`LimitOffset`/`LimitOption` — a `pageSize+1` sentinel fetch at `O + (page-1)*pageSize` bounded by the budget-capped browsable window.

`PageOptions.MaxRows` is the caller-resolved effective row budget — the service's default/hard-cap/production policy stays upstream and is not reimplemented here. `PageSize` must come from `model.AllowedPageSizes`. Accepted root limit forms: absent, `LIMIT ALL`/`NULL` (unbounded), and nonnegative integer literals (including decimal leading zeros, PG radix prefixes and legal digit separators beyond int32; values must fit int64); everything else — params, expressions, casts, floats/exponents, negatives, `OFFSET NULL`, `WITH TIES` — is rejected. Controlled codes: `query_not_allowed` (nil/malformed/multi-statement/non-SELECT tree), `validation_failed` (bad page size or budget), `page_out_of_range` (page beyond the browsable window or int64 overflow), `unsupported_limit_offset_form` (non-integer or unsupported root window). `PageWindow.Result(fetchedRows)` splits the delivered page from the sentinel and flags `BudgetReached` when the fetch hits the budget boundary — a row-budget signal, not proof additional rows exist; result-envelope semantics are T7/T8.

## Files

| File | Responsibility |
|------|---------------|
| walk.go | protoreflect-driven AST walker; path frames record parent message/field/index so visitors know structural position |
| guard.go | G5 read-only guard: single SELECT, no INTO/locking/data-modifying CTE, recursive forbidden-function denylist; classified original refs plus private same-analysis fingerprint; controlled `RejectError` codes |
| name_resolution.go | scope-aware `RangeVar` classification (CTE vs entity, WITH visibility, shadowing, ambiguity rejection), immutable `RefID`/`RefPath` collection, witnessable-position verdicts, and canonical byte-offset qualification on untrimmed original text |
| proof.go | T7-S1 verification record types (`SourceOccurrenceID`, `SourceRef`, `FDOrigin`, `PublicColumnProof`), `ErrEvidenceUnavailable`, deterministic set helpers, and internal record-consistency validation |
| inject.go | layout recording and freezing (`*`/`x.*`, ordinals, VALUES, per-reference colnames), entity metadata materialization, native USING/NATURAL merge proof, witness emission and propagation, SubLink suppression, set-op covered-source merging, DISTINCT Q+D, and public proof/source catalog export — provenance resolves through parent-linked correlated scopes (CTE bodies inherit the owning query's enclosing scope), per-row VALUES expression analysis under the VALUES layer's own WITH namespace, named-window clause expansion with SQL99 input-expression sort keys, and SQL92 ORDER BY/GROUP BY name binding at top level |
| rewrite.go | pipeline entry: guard or validated `GuardResult` → qualify → reparse → canonical `RefPath` mapping → optional pagination → inject → deparse → reparse-verify; exposes `Rewrite`, `RewritePaginated`, and `RewriteFromGuardPaginated` |
| pagination.go | G6 execution window: root-only LIMIT/OFFSET literal policy, int64-safe `A_Const` construction, budget arithmetic, `PageWindow.Result` bookkeeping |
| name_resolution_test.go | classification/visibility/qualification/guard intent tests |
| inject_test.go | freeze/injection/propagation/DISTINCT/suppression/set-op intent tests over stub resolvers |
| proof_test.go | RefID/RefPath mapping, public proof categories, unresolved evidence, source catalogs, and witness coverage/carrier intent tests |
| rewrite_api_test.go | external-package `RewriteFromGuardPaginated` seam and mixed-analysis rejection tests |
| pagination_test.go | PaginatePG window matrix, literal-form acceptance/rejection, tree immutability, `PageWindow.Result`, `RewritePaginated` pipeline shape |

## Boundaries

- No live catalog access — `ColumnResolver` is supplied by a later binding gate from the post-touch attribute map; `ColumnMetadataResolver` is its optional structured-metadata extension.
- Resolver failures stay internal except `ErrRelationNotFound` → `query_object_not_found`. Incomplete internal proof/mapping evidence fails with `ErrEvidenceUnavailable`, not a governed user rejection.
- No `to_regclass`/`has_schema_privilege`/touch-lock/OID verification — those remain T7-S2/S4 work.
- No PostgreSQL connection, SQL execution, HTTP routing, claim/finalize, remote-state persistence, or production-visible success path.
- No upstream budget policy — `PageOptions.MaxRows` arrives already resolved; unpaginated `Rewrite` still preserves all limit forms verbatim.
- No MySQL path changes — this package is isolated until explicitly wired.
