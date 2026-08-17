# ControlHub Domain Context

Domain terms and their boundaries for ControlHub Backend.

## Terms

### Evidence-Bearing Query Attempt

A governed query attempt that has reached a terminal outcome after its query
target has been resolved. Every such attempt must persist one complete evidence
record; unknown targets are never evidence-bearing because there is no valid
target to attribute.

### Execution Evidence Pair

The repository-owned atomic persistence unit for an Evidence-Bearing Query
Attempt: one query-execution history row and its corresponding fixed audit
event, committed in a single database transaction. The repository owns the
transaction, so services never compose separate history and audit writes. If
either write fails — including audit insertion — the whole pair rolls back and
no partial evidence commits. A failed pair surfaces the existing controlled
backend failure, increments a dimensionless persistence-failure counter once,
and emits one fixed safe log category containing no actor, target, statement,
value, credential, DSN, request data, or raw database error.

### Query Evidence Persistence Failure

A failed Execution Evidence Pair write. It is observable only through a
dimensionless process counter (`queryEvidencePersistenceFailures`, admin-only
metrics) and one fixed safe log category; it never carries identity, target,
statement, value, credential, DSN, request, or raw driver/database details.
There is no automatic retry, queue, worker, or disk buffer.