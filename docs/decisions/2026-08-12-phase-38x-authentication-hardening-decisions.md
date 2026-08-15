# Decision: Phase 38X Authentication Hardening Follow-Ups

## Status

Accepted on 2026-08-12. This decision records the remediation required before
the Phase 38X parent authentication-boundary ticket can close. It does not
rewrite the already published Issue #7 specification or prior delivery
evidence.

## Context

Independent parent-ticket verification found that the deployed authorization
boundary did not yet meet four accepted security properties:

1. Governed-query bearer freshness could be configured beyond the documented
   eight-hour maximum.
2. Authentication and authorization outcomes had no fixed, privacy-safe audit
   trail.
3. Console BFF production configuration accepted weak session-key material and
   an insecure Console Origin.
4. User passwords used a legacy unsalted SHA-256 representation.

These are separate implementation concerns, but share a safety boundary:
authentication, authorization, session configuration, and password handling
must fail predictably without exposing credentials or user-provided values.

## Decisions

### 1. Fixed eight-hour credential freshness

Remove `QUERY_EXECUTION_TOKEN_MAX_AGE`. Backend Bearer Credentials and
governed-query fresh-actor verification use one non-configurable eight-hour
maximum age. A server must not start with an alternate freshness value.

Operator Sessions retain their existing fixed eight-hour maximum age. The
frontend BFF, backend Bearer verification, and governed-query freshness are
therefore one aligned boundary rather than independently tunable lifetimes.

### 2. Minimal authentication and authorization audit taxonomy

Audit records capture only these fixed event types and fixed result values:

| Event type | Result values | Recording point |
| --- | --- | --- |
| `auth.login` | `succeeded`, `rejected` | Interactive/backend login outcome |
| `auth.bearer` | `rejected` | Protected-request credential verification failure |
| `auth.authorization` | `denied` | Verified actor rejected by a role boundary |

Successful ordinary protected reads are not audited. This avoids turning the
audit trail into a high-volume request log while retaining the security-relevant
login and denial evidence needed to investigate access failures.

An event with a verified actor records that actor's user ID. A rejected login or
unverified Bearer credential has no trusted actor and records no actor ID. A
target resource ID is recorded only when the rejected authorization boundary
already has a target resource identity; otherwise it is absent.

Audit events must never contain an email address, password, password hash,
Backend Bearer Credential, Operator Session or cookie, session key, request
body, query text, parameter value, DSN, IP address, User-Agent, or underlying
failure reason.

Audit persistence is fail-open. An audit write failure must not turn a valid
login into a rejected login, and must not change an authentication or
authorization decision. It must increment a fixed-category operational metric
and emit a safe error log that contains no identity, credential, request value,
or configuration secret. This intentionally creates an observable audit gap
rather than a silent one.

### 3. Production Console BFF configuration

The Console BFF accepts session-sealing keys only as base64-encoded values that
decode to exactly 32 random bytes. Production accepts exactly one configured
`https://` Console Origin. Empty values, malformed base64, wrong decoded
length, insecure `http://` Origins, multiple Origins, paths, and ambiguous
Origins fail startup before protected console traffic is served.

The existing local-development exception remains explicit and must not weaken
the production validation path.

### 4. Gradual password migration to Argon2id

New and reset passwords use Argon2id with 64 MiB memory, time cost 3, and
parallelism 1. Implementations must verify that this budget stays within about
250 ms on the lowest supported deployment specification.

Existing SHA-256 password hashes are accepted only long enough to migrate an
account: a successful legacy login immediately writes an Argon2id hash. New
hashes are never written in the legacy representation. Legacy verification is
removed only by a later explicit forced-reset or migration-completion delivery;
this change must not lock out inactive or infrequent users merely because they
have not logged in yet.

Expose only a non-identity-bearing operational count of accounts still using a
legacy hash. Do not expose account lists, hash values, passwords, or migration
status through public APIs, audit events, or logs.

## Delivery Boundaries

The work splits into blocker-first tickets:

1. Fixed eight-hour freshness and minimal auth/authz audit events. This blocks
   the parent Issue #7 closure because both are parent P1 findings.
2. BFF session-key and production-Origin validation. This is a security
   hardening ticket with frontend production-config and browser acceptance
   coverage.
3. Argon2id gradual password migration. This is a backend data-format and
   authentication-flow ticket with compatibility tests and a measured resource
   budget.

Each ticket must use real MySQL integration coverage. The BFF ticket also
requires Chromium coverage. Every audit assertion must prove forbidden values
are absent, and every configuration assertion must prove unsafe input fails
before serving protected traffic.

## Consequences

- Deployments lose the ability to extend governed-query credential freshness
  beyond eight hours by configuration.
- Security event audit coverage is explicit but deliberately not complete
  request logging; audit-store failures are observable without blocking access.
- Production BFF deployments need correctly encoded key material and an HTTPS
  Console Origin before startup.
- Password migration is transparent at the next successful login, with a
  bounded Argon2id resource cost and no surprise mass lockout.

## Rejected Alternatives

- Keeping a configurable freshness duration above eight hours: rejected because
  it silently weakens the accepted Operator Access Boundary.
- Auditing every successful protected request: rejected because it creates
  noisy, high-volume request logging rather than useful security audit events.
- Failing authentication closed when audit persistence is unavailable: rejected
  because the accepted availability policy is fail-open with explicit
  operational visibility.
- Accepting arbitrary high-entropy-looking sealing strings or HTTP production
  Origins: rejected because neither proves the required production boundary.
- Immediately invalidating every legacy SHA-256 account: rejected because no
  user self-service reset flow exists and it would lock out legitimate users.

## References

- Parent issue: `Fanduzi/ControlHub-Backend#7`
- Parent verification evidence:
  `docs/superpowers/evidence/2026-08-12-phase-38x-1-operator-access-boundary-parent-release-evidence.md`
- Operator Session decision:
  `docs/decisions/2026-08-09-operator-session-boundary.md`
- Seed credential remediation:
  `docs/decisions/2026-08-10-phase-38x-operator-boundary-seed-credential-remediation.md`
