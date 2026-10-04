# pgsql Module

PostgreSQL governed read-only query front half: pure parse-tree transforms — no database access, no execution wiring. Implements spec `2026-09-23-postgresql-query-workbench-v1.md` stages G4/G5/G10-5a (issue #112/T4) plus the G6 execution-window rewrite (issue #113/T5). PostgreSQL execution stays disabled; this package only produces the verified internal transport SQL plus the records the later binding gate and executor consume.

## Pipeline

```
user SQL ──▶ GuardPG            (parse once; single SelectStmt; denylist; RangeVar classification)
         ──▶ QualifyEntities    (byte-offset "schema". insertion on entity refs only)
         ──▶ reparse            (qualified text must compile as written)
         ──▶ PaginatePG         (RewritePaginated only: window arithmetic, root LIMIT/OFFSET rewrite)
         ──▶ InjectWitnesses    (freeze visible layout → per-layer witness columns → DISTINCT Q+D)
         ──▶ deparse + reparse  (internal transport text, verified compilable)
```

Witness columns (`__chub_w<N>`, always NULL, typed as the source relation's `reltype`) attest which relation identity execution actually bound. They exist only inside the transport layout — never in user output, join keys, grouping keys, dedup keys, or stored SQL.

## Pagination (G6)

`RewritePaginated` runs the pipeline above with `PaginatePG` applied to the reparsed qualified tree before witness injection, returning the `PageWindow` beside the `RewriteResult`. `PaginatePG` alone accepts only a single-root `SelectStmt` tree whose guard/qualification preconditions the caller already established; it validates all options and window arithmetic before touching the tree, then rewrites only the root `LimitCount`/`LimitOffset`/`LimitOption` — a `pageSize+1` sentinel fetch at `O + (page-1)*pageSize` bounded by the budget-capped browsable window.

`PageOptions.MaxRows` is the caller-resolved effective row budget — the service's default/hard-cap/production policy stays upstream and is not reimplemented here. `PageSize` must come from `model.AllowedPageSizes`. Accepted root limit forms: absent, `LIMIT ALL`/`NULL` (unbounded), and nonnegative integer literals (including decimal leading zeros, PG radix prefixes and legal digit separators beyond int32; values must fit int64); everything else — params, expressions, casts, floats/exponents, negatives, `OFFSET NULL`, `WITH TIES` — is rejected. Controlled codes: `query_not_allowed` (nil/malformed/multi-statement/non-SELECT tree), `validation_failed` (bad page size or budget), `page_out_of_range` (page beyond the browsable window or int64 overflow), `unsupported_limit_offset_form` (non-integer or unsupported root window). `PageWindow.Result(fetchedRows)` splits the delivered page from the sentinel and flags `BudgetReached` when the fetch hits the budget boundary — a row-budget signal, not proof additional rows exist; result-envelope semantics are T7/T8.

## Files

| File | Responsibility |
|------|---------------|
| walk.go | protoreflect-driven AST walker; path frames record parent message/field/index so visitors know structural position |
| guard.go | G5 read-only guard: single SELECT, no INTO/locking/data-modifying CTE, recursive forbidden-function denylist; controlled `RejectError` codes |
| name_resolution.go | scope-aware RangeVar classification (CTE vs entity, WITH visibility, shadowing, ambiguity rejection); qualified names always entity; witnessable-position verdicts incl. SubLink testexpr + qualified refs; canonical byte-offset qualification on the untrimmed original |
| inject.go | layout recording (`*`/`x.*`/ordinals/VALUES/per-reference colnames — stars stay verbatim unless an injected column forces expansion, so merged USING/NATURAL columns keep PostgreSQL's native values, types and source FDs; forced merges are referenced through the join's join_using_alias (user-supplied or generated under `__chub_`; NATURAL always freezes to the ORIGINAL public-column intersection — explicit USING when nonempty, `ON TRUE` with the original join type when empty — so injected witnesses can never become join keys) so PostgreSQL computes the merged var natively — common type, typmod, GROUP BY legality and FDs all intact; every public column additionally carries provenance — structured `ColumnType`+relation/attribute identity plus the set of scope-local references (FROM item + public ordinal + qualifier/name) the original output item was legally nameable through, restamped at each alias boundary), join aliases wrap a derived layer, witness emission (ordinary vs aggregate template — detection spans targets, ORDER BY, named windows), cross-layer propagation, SubLink suppression, final per-entity coverage rejection over a union-find merge of set-op leaves, DISTINCT Q+D with location-insensitive sort rebinding (ordinal keys positionally, ColumnRef keys by resolving the ORIGINAL FROM scope against recorded reference identities — leaf qualifiers, bare merged names, and join_using_alias spellings all bind when they denote the same surviving Var; bare keys bind output names first (SQL92), duplicates only when they denote the same Var/expression, and input lookup scans top-level FROM outputs only; the optional `ColumnMetadataResolver` capability supplies per-column OIDs/typmods/attribute identity plus the provider's native `CommonType` verdict, which proves whether a merged USING column kept its picked leaf Var verbatim — INNER prefers left then right, LEFT keeps left, RIGHT keeps right, FULL keeps none; a decided merged column whose only source reference is itself is a terminal computed JOIN identity and binds to itself; when that proof lacks evidence the failure is `ErrTypeResolutionUnavailable`, an internal missing-dependency error, never a `RejectError`), reserved `__chub_` name enforcement |
| rewrite.go | pipeline entry: guard → qualify → reparse → (PaginatePG when paginated) → inject → deparse → reparse-verify; `Rewrite` preserves the original window verbatim, `RewritePaginated` returns the `PageWindow` beside transport SQL, witness records, public output names. Resolver errors stay internal except `ErrRelationNotFound` → `query_object_not_found` with a fixed message. Semantic equivalence is proved by `internal/integration/pg_rewrite_test.go` + `pg_pagination_test.go` against disposable PostgreSQL |
| pagination.go | G6 execution window: root-only LIMIT/OFFSET literal policy (PG radix prefixes, legal separators and decimal leading zeros accepted within int64), int64-safe `A_Const` construction (ival ≤ MaxInt32, fval decimal above), budget arithmetic, `PageWindow.Result` bookkeeping |
| name_resolution_test.go | classification/visibility/qualification/guard intent tests |
| inject_test.go | freeze/injection/propagation/DISTINCT/suppression/set-op intent tests over a stub resolver |
| pagination_test.go | PaginatePG window matrix, literal-form acceptance/rejection incl. radix/separator spellings and int64 boundary, tree immutability, `PageWindow.Result`, `RewritePaginated` pipeline shape |

## Boundaries

- No live catalog access — `ColumnResolver` is a stub interface the binding gate will feed from the post-touch attribute map; `ColumnMetadataResolver` is its optional structured-metadata extension (per-column type OID/typmod + relation/attribute identity, plus the database's own common-type verdict for an OID pair — never a built-in type table). Resolvers without it still serve name-only queries; proofs that need the missing evidence fail with `ErrTypeResolutionUnavailable`, an internal error distinct from any governed rejection.
- No `to_regclass`/`has_schema_privilege`/touch-lock/OID verification — T5/T7.
- No upstream budget policy — `PageOptions.MaxRows` arrives already resolved; unpaginated `Rewrite` still preserves all limit forms verbatim.
- No MySQL path changes — this package is isolated until explicitly wired.
