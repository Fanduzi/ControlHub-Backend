# Explain Discloses Relation and Index Names; MySQL Honors User LIMIT/OFFSET

Status: accepted

Two related workbench findings from dogfooding are resolved together.

Explain plan nodes previously carried no identifiers at all, so a plan could
render two indistinguishable "index access" nodes with nothing to tell them
apart. Operators with access to a query target can already browse its schema
and objects, so the relation/table name and the chosen index name carry no
new disclosure: the normalized node now exposes bounded `relation` and
`index` fields (≤128 bytes, no control characters; over-bound or malformed
names are dropped rather than failing the plan). Predicates, literals,
column lists, `cost_info`, `possible_keys`, `used_key_parts`, `ref`,
`message`, and all other free-form engine detail remain withheld. MySQL
maps `table_name`/`key`; PostgreSQL maps "Relation Name" (falling back to
"CTE Name") and "Index Name".

MySQL/TiDB execution previously rewrote the user's own `LIMIT`/`OFFSET`
away, so `LIMIT 2 OFFSET 5` returned the first governed page and `LIMIT 3`
was reported truncated. The guard now reads literal `LIMIT`/`OFFSET` values
from the AST and intersects them with the governed cap — browsable rows are
`min(effectiveMaxRows, userLimit)`, the user's `OFFSET` shifts the fetch
origin, and a page beyond the browsable window is rejected — matching the
established PostgreSQL pagination semantics. Non-literal forms (placeholders,
expressions) are rejected through the existing `validation_failed` path. A
user `LIMIT` tighter than the cap is fetched exactly and is never marked
truncated.

Consequences: Explain responses gain two optional identifier fields
(contract-extending, not contract-breaking); MySQL paginated and
non-paginated execution returns the user's actual window instead of the
governed page; `LIMIT 0` yields an empty first page; template and saved
statement execution share the same guard and inherit the same semantics.
