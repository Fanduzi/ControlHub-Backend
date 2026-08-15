# Decision: Bounded Untrusted Bearer Audit Persistence

## Status

Accepted on 2026-08-15. This decision records the bounded-audit clarification
for Issue #29, "38X-2I: Bound unauthenticated Bearer audit persistence and
preserve anonymous audit cutover". It clarifies the scope of
`auth.bearer/rejected` from the published 2026-08-12 authentication-hardening
decision and does not rewrite that decision or any prior delivery evidence.

## Context

The published 2026-08-12 authentication-hardening decision records
`auth.bearer/rejected` for protected-request credential verification failures.
As written, that taxonomy made a request with no `Authorization` header and an
attacker-supplied arbitrary credential indistinguishable, and both could create
unbounded synchronous audit persistence work before the controlled 401
response. Ordinary unauthenticated background traffic must not become a
high-volume request log or a database write-amplification path.

## Decisions

### 1. Missing credentials are not rejected credentials

A request with no `Authorization` header is absence of a credential, not a
rejected supplied credential. It returns the existing generic 401, the
protected handler does not execute, and it emits no `auth.bearer/rejected`
event. A supplied but untrusted Bearer Credential — malformed, forged,
expired, revoked, disabled, or otherwise rejected before a verified actor
exists — remains eligible for the fixed `auth.bearer/rejected` event.

### 2. Fixed process-local persistence budget

A supplied but untrusted Backend Bearer Credential that is rejected before a
verified actor exists may persist at most 60 `auth.bearer/rejected` events per
minute per server process. The budget is fixed, requires no configuration, has
no IP, token, identity, or request-value dimension, resets on process restart,
and rolls forward on a fixed one-minute window anchored at the first event in
that window.

The budget governs only persistence of untrusted rejection events. Verified
actor events — a validly signed but stale credential rejected by the
eight-hour freshness gate, and verified role denials (`auth.authorization/
denied`) — are never budgeted. Rejected login behavior is unchanged.

### 3. Suppression is observable, not silent

Once the budget is exhausted, the response remains the same controlled 401,
handler execution is unchanged, no per-attempt audit row or detailed log is
written, and a fixed safe suppression counter increments. The suppression
counter is operational telemetry, not an audit event: it carries no identity,
credential, or request dimension and is exposed only through the existing
administrator-only auth-audit metrics surface, alongside the existing
persistence-failure counter.

## Deliberate limits of this first implementation

- The budget is process-local and per-router-instance; the deployed server runs
  one router per process, which is the stated boundary. There is no distributed
  or database-backed limiter, no Redis, and no cross-process coordination.
- The budget is a fixed-window bound anchored at the first event of each
  minute, not a sliding window. Under sustained attack traffic it deliberately
  favors bounded storage and availability over complete unauthenticated-attempt
  history; suppression telemetry is the explicit operational signal for that
  tradeoff.
- Missing-header requests emit no event at all, so a pure unauthenticated
  scanner without credentials is invisible to the audit trail by design. The
  bounded rejected-credential trail and its suppression metric are the
  security evidence for supplied-credential attempts.
- The two authentication middleware factories remain unchanged in structure;
  their duplication is an accepted follow-up, not part of this delivery.

## Consequences

- Deployments bound hostile unauthenticated audit writes to 60 rows per minute
  per process without configuration or new infrastructure.
- The controlled 401 outcome is invariant under audit persistence and
  suppression; observability controls never alter access decisions.
- Administrators can observe both audit-store failures and reduced audit
  fidelity through the existing metrics surface, with no identity exposure.

## Rejected Alternatives

- Auditing every missing-header request: rejected because ordinary
  unauthenticated traffic would become a high-volume request log.
- A rolling/sliding per-attempt window or per-IP/per-credential budget:
  rejected because it adds dimensions and state the accepted privacy-safe
  taxonomy deliberately avoids; the fixed per-process bound is the specified
  boundary.
- Distributed or database-backed limiting: rejected because the process-local
  budget already bounds write amplification and adds no new infrastructure or
  dependency.
- Failing requests closed or logging per-attempt details when the budget is
  exhausted: rejected because it would change access decisions or create
  identity-bearing logs.

## References

- Parent issue: `Fanduzi/ControlHub-Backend#29`
- Published authentication-hardening decision (not rewritten):
  `docs/decisions/2026-08-12-phase-38x-authentication-hardening-decisions.md`
- Null-audit-actor cutover decision and delivery:
  `Fanduzi/ControlHub-Backend#30`
