# ControlHub Domain Context

## Terms

### Governed Parameterized Template

A target-scoped, reusable saved query whose SQL text contains an explicitly
declared set of typed parameter bindings. The server, not the browser,
validates every binding and applies it only through the existing governed
execution chain. It is distinct from a static saved statement, browser string
interpolation, a secret placeholder, or an executable notebook.

### Template Parameter

A named, required scalar value declared by a governed parameterized template.
Version one supports only string, integer, decimal, and boolean values in SQL
expression positions. It cannot represent null, an array, an identifier, an
SQL fragment, a default value, or a secret placeholder. A parameter value is
used only for one execution and is never persisted, audited, logged, or added
to query history.

### Template Placeholder

The `:name` marker in a governed parameterized template's SQL text. The
backend recognizes it only after parsing and permits it only where a scalar
SQL value is valid. The browser never substitutes a placeholder into SQL; the
backend binds the matching Template Parameter through the database driver.

### Parameterized Saved Statement

A Governed Parameterized Template stored in the existing target-scoped saved
statement library. It uses the existing personal and shared_template scopes and
their authorization rules. A static saved statement has no Template Parameters;
a parameterized saved statement has one or more. Neither creates a global
template library or a new visibility scope.

### Template Load

The non-executing action that places a parameterized saved statement's SQL text
in the current worksheet and presents its Template Parameters for input. It
does not run SQL or request schema, history, disclosure, or results. Values
entered for a Template Load are ephemeral and are discarded on refresh or
worksheet change; only an explicit Run enters the existing governed execution
chain.

### Template Parameter Definition

The author-owned name and scalar type assigned to a Template Placeholder when
creating or updating a Parameterized Saved Statement. The server requires a
one-to-one match between definitions and placeholders: none may be missing,
duplicated, or unused. A runner supplies values only and cannot alter these
definitions.

### Template Execution

An explicit run request containing only a parameterized saved statement ID and
its proposed Template Parameter values. The server re-reads the latest
authorized template and definitions before validating and binding values, then
uses the existing governed execution chain. Editing loaded SQL exits Template
Execution mode and returns the worksheet to ordinary ad hoc SQL execution.

### Parameter Value Evidence Policy

A target-scoped, administrator-managed policy controlling whether actual
Template Parameter values are captured for executions on that target. It is
disabled by default and cannot be overridden by a runner or selected per
execution. When enabled, values belong in a separate restricted forensic store,
not ordinary audit events; policy changes are themselves auditable.

Parameter Value Evidence Policy is explicitly out of scope for Phase 38W. It
cannot be exposed until key management and restricted evidence-reader controls
are designed and implemented.

### Parameter Value Evidence Retention

The retention policy for captured Template Parameter values. MySQL may hold
only a seven-day hot copy. Longer retention requires a separately configured
external forensic archive, whose retention is administrator-configured within
that archive's supported limits. Without an external archive, MySQL retention
cannot be extended beyond seven days.

### Evidence-Required Execution

A Template Execution for a target with external Parameter Value Evidence
Retention enabled. The external archive write is part of the execution's
governance boundary: if it fails, the server rejects the execution and does not
run SQL. A target without external archival may execute with only its seven-day
MySQL hot evidence.

### External Evidence Archive

The future authoritative forensic store for parameter values retained longer
than seven days. It is out of scope for the first parameterized-template
delivery. A separate delivery must cover provider credentials, key management,
encryption, reader access, retention, and failure exercises before it can be
configured or activated.

### Template Value Session

The in-memory values entered for one Template Load. They survive local
validation and controlled execution errors so the runner can correct them, but
are discarded when the worksheet changes, the template is closed, the page is
refreshed, or the user signs out. A Template Value Session is never written to
browser persistence.

### Template Value Encoding

The JSON representation of a Template Parameter value at the Template
Execution API boundary: strings are JSON strings, integers are JSON integers,
decimals are JSON strings to preserve precision, and booleans are JSON
booleans. The server strictly rejects missing, unknown, or mismatched values.

### Template Parameter Validation

The server-side limits and user-safe validation for a Parameterized Saved
Statement: at most twenty parameters; lowercase names beginning with a letter
and containing only letters, digits, or underscores; each string or decimal at
most 4 KiB; and a complete values request at most 16 KiB. Failures are mapped
to localized, field-level controlled errors and never reveal SQL, supplied
values, driver details, or archive internals.

### Operator Access Boundary

The server-owned authorization matrix for ControlHub operational surfaces.
Authenticated editors may read Inventory and use only the existing governed
query capabilities granted to them. Only authenticated administrators may
mutate Inventory or read audit events. Anonymous access is limited to health,
login, and published API documentation.

### Operator Session

The authenticated session for a ControlHub console user. It is opaque to
browser JavaScript and carries the user's identity and role to server-owned
authorization boundaries. An Operator Session is distinct from a Backend
Bearer Credential used by programmatic clients and server-to-server calls. It
has a fixed eight-hour maximum age matching query freshness; expiry requires
an interactive login before any console operation resumes.

### Console Origin

The single configured web origin permitted to use an Operator Session. State
changing console requests must originate from this origin; cross-origin
embedding, credentialed CORS, and cross-site console use are unsupported.

### Console BFF

The server-owned frontend boundary through which a console browser accesses
ControlHub APIs. It translates an Operator Session into a Backend Bearer
Credential without exposing that credential to browser JavaScript. Console
browsers use the Console BFF exclusively; direct Backend Bearer APIs are for
approved non-browser clients.

### Sealed Operator Session

An Operator Session represented by an encrypted and authenticated, fixed-age
cookie managed by the Console BFF. It requires no server-side session store;
logout removes the cookie and key rotation invalidates sessions according to
the configured key policy. A user disablement, role change, or password reset
invalidates it immediately rather than waiting for normal expiry.

### Authorization Version

The current server-owned authorization state of a user, including whether the
user is active and the role currently assigned. Every protected request checks
Authorization Version so a stale Backend Bearer Credential cannot retain
permissions after a user disablement, role change, or password reset.

### Controlled Authorization Error

The safe response model for authentication and authorization failures. Missing,
invalid, expired, revoked, and disabled sessions return the same unauthenticated
outcome and require login again. A valid session without the required role
returns a distinct forbidden outcome. Neither outcome exposes credentials or
the reason a session is no longer accepted.

### Backend Bearer Credential

A backend-issued credential that authenticates an actor to ControlHub APIs. It
may be used by approved non-browser clients and by server-owned integration
boundaries, but is never exposed to console browser JavaScript.

### Evidence-Bearing Query Attempt

A governed query attempt that has reached a terminal outcome after its query
target has been resolved — whether success, rejection, timeout, failure, or
client cancellation. Every such attempt must persist one complete evidence
record, and the record write is cancellation-durable: it outlives the client
disconnect that may have caused the attempt to end. Unknown targets are never
evidence-bearing because there is no valid target to attribute.

### Evidence Persistence Window

The fixed two-second bounded context in which an Evidence-Bearing Query
Attempt's Execution Evidence Pair write runs. It is detached from the request
context — a client cancellation or deadline expiry can never drop the terminal
evidence — and the write is a single synchronous bounded attempt with no
retry, queue, worker, or disk buffer. A window expiry or persistence failure
surfaces the existing controlled backend error and the existing
persistence-failure counter.

### Execution Evidence Pair

The repository-owned atomic persistence unit for an Evidence-Bearing Query
Attempt: one query-execution history row and its corresponding fixed audit
event, committed in a single database transaction. The audit event type is a
fixed per-path constant — `query.executed` for ordinary, paged, and template
executions, `related_record_navigation` for related-record navigation — never
request-controlled. The repository owns the transaction, so services never
compose separate history and audit writes; the standalone execution-history
write seam is removed. If
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

### Client-Cancellation Evidence

The terminal Evidence-Bearing Query Attempt outcome recorded when the client
disconnects during query or disclosure work: status `failed` with the fixed
`query_canceled` error code and the fixed safe message "query canceled". A
query that completed successfully before the cancellation remains recorded as
success; deadline expiry keeps its separate `timeout` outcome, and a genuine
disclosure-policy rejection stays `rejected`. No statement values, template
values, credentials, DSNs, or raw errors are ever stored with cancellation
evidence.

### Workbench Request Terminal State

The visible settled state of a current Query Workbench request: authorized
content, an empty result, or a controlled error. Superseded requests never
change the visible state, and a current request never remains indefinitely in
loading after it succeeds or fails.

### Schema Metadata Identity

The Query Workbench ownership boundary for database, object, and completion
metadata: one query target and one database. Worksheets may share metadata only
while this identity is unchanged; changing either part immediately makes the
prior metadata unavailable.
