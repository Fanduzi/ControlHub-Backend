# Decision: Evidence Persistence Window for Cancellation-Durable Query Evidence

## Status

Accepted on 2026-08-18. This decision records the design for Issue #35,
"38X-3B: Preserve terminal query evidence after client cancellation" (parent
#9). It extends the Issue #34 atomic Execution Evidence Pair with
cancellation durability and does not rewrite the Issue #34 decision, the
Phase 38W/38X evidence decisions, or any prior delivery evidence.

## Context

Issue #34 made ordinary, paged, and template executions record one repository
owned atomic Execution Evidence Pair (history row + fixed `query.executed`
audit event). Every attempt is guaranteed recorded — but the guarantee had a
hole: the pair write ran against the request context. When a client
disconnected mid-query, the executor returned `context.Canceled`, and the
following pair write received the already-canceled request context, so the
write failed and the request surfaced the controlled backend error with no
evidence committed. The terminal outcome of the attempt was lost exactly when
the attempt was terminal.

A second hole: only the executor and disclosure-apply failure paths classified
outcomes. A client cancellation during disclosure preflight work (inspector/
policy reads) failed with a raw error that bubbled to a 500 with no evidence,
and a canceled disclosure read that happened to be blended into the
`ErrQueryDisclosureBlocked` wrap was misclassified as a public policy
rejection.

## Decision

1. **Evidence Persistence Window.** The Execution Evidence Pair write for an
   Evidence-Bearing Query Attempt runs in its own fixed **two-second** bounded
   context, detached from request cancellation and deadline via
   `context.WithoutCancel` + `context.WithTimeout`. `context.WithoutCancel`
   preserves the request's trace values while severing the Done channel, so a
   client disconnect or deadline expiry can never drop the terminal evidence.
   The write is a single synchronous bounded attempt with **no retry, queue,
   worker, or disk buffer**. A window expiry or any persistence failure
   surfaces the existing controlled backend error and the existing
   `queryEvidencePersistenceFailures` counter (Issue #34 semantics unchanged).
   The window lives in the service choke point `persistAttempt`, so it covers
   success, rejection, timeout, failure, and cancellation evidence on the
   ordinary, paged, and template paths uniformly.

2. **Client-cancellation classification.** `context.Canceled` from the executor
   (driver abort after disconnect) or from disclosure work is classified as
   status `failed`, error code `query_canceled`, with the fixed safe message
   "query canceled". The raw driver error is never persisted, returned, or
   logged. `context.DeadlineExceeded` keeps its existing `timeout` outcome
   unchanged (408).

3. **Disclosure terminal outcomes reach the shared atomic evidence path.** A
   public disclosure-policy rejection (`ErrQueryDisclosureBlocked` without a
   cancellation or deadline cause) stays a `rejected` attempt. All other
   post-target disclosure terminal failures — including a canceled or
   deadline-expired disclosure read blended into the blocked wrap — are
   classified (failed/query_canceled or timeout) and recorded through the
   same atomic pair path with a controlled sentinel response. To make the
   blend detectable, the disclosure service `%w`-wraps the inner preflight
   read error inside the blocked wrap (text-identical to the previous `%v`,
   but `errors.Is` can now see the cancellation/deadline cause).

4. **Success before cancellation stays success.** A query that completed
   successfully before the client cancellation arrived is recorded as
   `success`; the cancellation never retroactively downgrades or drops it.

5. **Scope boundary.** Unknown targets and failures before target resolution
   remain outside execution evidence. #36 related-record navigation stays on
   its standalone history/audit seam (Issue #34 expand step) and is not
   covered by this decision.

## Consequences

- Every Evidence-Bearing Query Attempt on the atomic path — ordinary, paged,
  and template — is now cancellation-durable after target resolution.
- The canceled outcome vocabulary on the wire is unchanged: the handler still
  maps the sentinel to the existing controlled `query_backend_error` response
  (502); the recorded evidence carries the new fixed `query_canceled` code.
- Persistence telemetry semantics are unchanged: exactly one
  `queryEvidencePersistenceFailures` increment and one fixed safe log category
  per failed pair.
- No migration, no schema change, no new endpoint.

## Acceptance Mapping (Issue #35)

- Two-second bounded window detached from request cancellation/deadline; no
  retry/queue/worker/disk buffer — Decision 1.
- Client cancellation during query/disclosure records failed/query_canceled
  with a fixed safe message — Decisions 2–3.
- Deadline expiry remains the existing timeout outcome — Decisions 2–3.
- Disclosure policy rejection remains rejected; other post-target disclosure
  terminal failures record fixed safe failed or timeout evidence — Decision 3.
- Successful query before cancellation remains success — Decision 4.
- Unknown targets and pre-resolution failures remain outside evidence —
  Decision 5.
- Persistence failure rolls back the pair, increments fixed telemetry, and
  preserves the existing backend-error response — Decision 1 (unchanged
  Issue #34 semantics).
- Ordinary, paged, and template cancellation paths covered without storing
  statement values, template values, credentials, DSNs, or raw errors —
  Decisions 1–4 (evidence metadata unchanged from Issue #34).
- Glossary terms and decision documentation — this document.