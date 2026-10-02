# pgsql Module

PostgreSQL governed read-only query front half: pure parse-tree transforms — no database access, no execution wiring. Implements spec `2026-09-23-postgresql-query-workbench-v1.md` stages G4/G5/G10-5a for issue #112 (T4). PostgreSQL execution stays disabled; this package only produces the verified internal transport SQL plus the records the later binding gate and executor consume.

## Pipeline

```
user SQL ──▶ GuardPG            (parse once; single SelectStmt; denylist; RangeVar classification)
         ──▶ QualifyEntities    (byte-offset "schema". insertion on entity refs only)
         ──▶ reparse            (qualified text must compile as written)
         ──▶ InjectWitnesses    (freeze visible layout → per-layer witness columns → DISTINCT Q+D)
         ──▶ deparse + reparse  (internal transport text, verified compilable)
```

Witness columns (`__chub_w<N>`, always NULL, typed as the source relation's `reltype`) attest which relation identity execution actually bound. They exist only inside the transport layout — never in user output, join keys, grouping keys, dedup keys, or stored SQL.

## Files

| File | Responsibility |
|------|---------------|
| walk.go | protoreflect-driven AST walker; path frames record parent message/field/index so visitors know structural position |
| guard.go | G5 read-only guard: single SELECT, no INTO/locking/data-modifying CTE, recursive forbidden-function denylist; controlled `RejectError` codes |
| name_resolution.go | scope-aware RangeVar classification (CTE vs entity, WITH visibility, shadowing, ambiguity rejection) + witnessable-position verdicts + canonical byte-offset qualification |
| inject.go | layout freezing (`*`/`x.*`/NATURAL→USING/ordinals/VALUES/CTE colnames), witness emission (ordinary vs aggregate template), cross-layer propagation, SubLink suppression, set-op merge checks, DISTINCT Q+D wrapper, reserved `__chub_` name enforcement |
| rewrite.go | pipeline entry: guard → qualify → reparse → inject → deparse → reparse-verify; returns transport SQL, witness records, public output names |
| name_resolution_test.go | classification/visibility/qualification/guard intent tests |
| inject_test.go | freeze/injection/propagation/DISTINCT/suppression/set-op intent tests over a stub resolver |

## Boundaries (T4 does NOT do)

- No live catalog access — `ColumnResolver` is a stub interface the binding gate will feed from the post-touch attribute map.
- No `to_regclass`/`has_schema_privilege`/touch-lock/OID verification — T5/T7.
- No pagination form validation (`unsupported_limit_offset_form` belongs to G6) — the rewriter preserves all limit forms verbatim.
- No MySQL path changes — this package is isolated until explicitly wired.
