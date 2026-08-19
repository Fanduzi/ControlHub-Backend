# Decision: Bounded Graceful Drain on SIGTERM/SIGINT for In-Flight Query Evidence

## Status

Accepted on 2026-08-18. This decision records the design for Issue #37,
"38X-3D: Drain in-flight query evidence during normal shutdown" (parent #9).
It adds a server lifecycle seam on top of the Issue #34 atomic Execution
Evidence Pair and the Issue #35 two-second Evidence Persistence Window, and
does not rewrite those decisions, the Phase 38W/38X evidence decisions, or any
prior delivery evidence.

## Context

Issues #34–#36 made every Evidence-Bearing Query Attempt cancellable-durable:
once the query target is resolved, the history row and its fixed audit event
commit as one repository-owned atomic pair inside a fixed two-second Evidence
Persistence Window detached from request cancellation and deadline. That
guarantee still had a lifecycle hole: the server started with a direct
listen-and-serve call and installed no shutdown handling, so a normal operator
SIGTERM/SIGINT killed the process mid-query. Any handler that had not yet
persisted its terminal evidence lost it exactly when the operator had asked
the process to wind down — the most predictable, operator-initiated failure a
process can face.

A hard durability guarantee across process crash, host loss, power loss,
forced second signal, or `kill -9` would require an external durable queue, a
separate architecture deliberately out of scope for this delivery (parent #9,
out-of-scope list).

## Decision

1. **Signal wiring.** The server registers SIGTERM and SIGINT. The first
   signal begins graceful shutdown: `http.Server.Shutdown` immediately closes
   the listener (new traffic is stopped) and waits for existing handlers to
   return to idle.
2. **The ten-second drain bound is a fixed product invariant.** In-flight
   handlers may finish for at most ten seconds
   (`shutdownDrainTimeout = 10 * time.Second`), covering the existing
   maximum five-second query deadline, the two-second Evidence Persistence
   Window, and scheduling margin. It is a code constant, not environment
   configuration — no operator knob, because the bound is derived from
   product deadlines that are themselves fixed.
3. **Clean drain exits successfully.** If all in-flight handlers finish
   (including a governed query's deadline and its evidence window) within the
   bound, the process exits 0.
4. **Fail-loud boundary.** If the drain bound is exhausted or the HTTP server
   fails for any other reason, the process emits only one fixed safe log
   message — no error values, request data, statement, target, credential,
   DSN, or raw failure details — and exits non-zero. An incomplete drain must
   be visible to operators, never reported as clean.
5. **Second signal forces immediate exit.** A second SIGTERM/SIGINT during
   the drain exists explicitly outside the durability guarantee and forces
   immediate exit, so an operator can always regain control. `kill -9` and
   unresolvable host loss need the same disclaimer (out-of-scope above).
6. **No retry, queue, worker, or disk buffer.** The drain only waits for
   handlers; it does not add persistence machinery. Shutdown never cancels
   in-flight request contexts — unlike a client disconnect, a normal drain
   does not truncate the Evidence Persistence Window, so the two-second
   detached window completes naturally during the ten-second bound.
7. **Exit-code contract.** `runServer` returns the process exit code,
   centralizing the 0-vs-1 decision at one testable seam; `main` wires real
   signals and the fixed bound into it.

## Consequences

- On a normal operator shutdown, an in-flight governed query attempt that
  reached a terminal outcome can commit its Execution Evidence Pair; an
  interrupted drain is visible via a fixed safe log and a non-zero exit.
- The public API contract is unchanged: no endpoint, status code, envelope,
  or metrics surface is added or altered by the lifecycle change.
- No migration, no schema change, no new dependency. Only
  `cmd/server/main.go`, a new `cmd/server/shutdown.go` seam, and their tests
  change; the module README records the shutdown contract.
- The durability guarantee explicitly excludes process crash, host loss,
  power loss, forced second signal, and `kill -9` (Decision 5 and parent #9
  out-of-scope list).

## Alternatives Rejected

- **Unbounded or configurable drain.** An operator-tunable bound could outlive
  the query deadline it must cover and invite "just raise it" behavior that
  silently weakens the evidence guarantee; a fixed constant keeps the bound
  provably ≥ deadline + window + margin.
- **Background queue / disk buffer for unfinished evidence.** Would hold
  uncommitted evidence outside the database across process exit, requiring
  replay and idempotency machinery — the separate durable architecture
  parent #9 explicitly excludes.
- **Immediate exit on first signal.** Preserves nothing; equivalent to today's
  behavior and would leave the parent's shutdown-durability stories unmet.
- **`signal.NotifyContext` + `Server.Shutdown` only.** Loses the
  second-signal force-exit and the exit-code contract; the explicit channel
  seam keeps both at one testable function.

## Acceptance Mapping (Issue #37)

- SIGTERM and SIGINT stop new traffic and begin graceful shutdown —
  Decisions 1 and 2; proven in `TestRunServer_TerminationSignalsBeginGracefulDrain`
  (real signals to the test process) and `TestRunServer_SignalStopsTrafficAndDrainsInFlightRequest`
  (listener refuses new dials while a handler is still active).
- Existing handlers may finish for at most ten seconds — Decision 2
  (`TestRunServer_DrainDeadlineExhaustionExitsNonZero` with an injected short
  bound proves the bound terminates the wait).
- The ten-second bound is fixed, not environment-configurable — Decision 2
  (constant `shutdownDrainTimeout`; tests inject bounds only via the function
  parameter, production always passes 0).
- A query with in-flight evidence can complete during normal drain —
  Decisions 1 and 6; in-flight handler completes and the process exits 0 in
  the traffic-stop test, and the evidence window is unchanged and ≤ the bound.
- Clean drain exits successfully — Decision 3 (same test, exit 0).
- Shutdown deadline exhaustion or server failure emits only a fixed safe log
  and exits nonzero — Decision 4 (each failure path emits exactly one constant
  message with no interpolated values; `assertOneMessage` in
  `cmd/server/shutdown_test.go` asserts the exact text and single count).
- Shutdown never waits indefinitely and introduces no background queue or
  disk buffer — Decisions 2, 5, 6.
- Crash, host loss, power loss, forced second signal, and `kill -9` remain
  explicitly outside the durability guarantee — Decision 5, Context.
- Lifecycle tests prove traffic stop, successful drain, timeout, and bounded
  completion without wall-clock sleeps where deterministic coordination is
  possible — channel-coordinated in-flight handlers and injected short drain
  bounds; only the timeout test itself waits on a 40 ms bound.
- The accepted shutdown and persistence trade-offs are recorded in the ADR
  and module documentation — this document and `cmd/server/README.md`.