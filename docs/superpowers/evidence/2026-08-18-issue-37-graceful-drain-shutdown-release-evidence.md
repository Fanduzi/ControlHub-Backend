# Issue #37 (38X-3D) Bounded Graceful Drain on SIGTERM/SIGINT — Release Evidence

Date: 2026-08-18

This is the backend delivery record for `Fanduzi/ControlHub-Backend#37`
("38X-3D: Drain in-flight query evidence during normal shutdown", parent #9).
It adds the server lifecycle seam the parent's shutdown-durability stories
required: SIGTERM/SIGINT stop new traffic and drain in-flight handlers for at
most a fixed ten seconds, so a governed query reaching a terminal outcome can
commit its Execution Evidence Pair (Issue #34) inside its two-second Evidence
Persistence Window (Issue #35) during a normal operator-initiated shutdown.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main` at start, after #36 closure at `0f8e439`) | `0f8e4395674121c3ad9c66038f38c1d5b777c499` |
| Branch / worktree | `issue-37-drain-shutdown-20260818` at `~/GolangProjects/ControlHub-wt-issue-37-20260818` |
| Product SHA (all candidate gates run here) | `11f3237197f67b426659790f498359f0e3d4f097` |
| Delivery commits | `11f3237` `feat(server): bounded graceful drain of in-flight requests on SIGTERM/SIGINT (issue #37)` |

Delivery range (`git diff --stat origin/main...HEAD`): 6 files, +561/-8 —
`cmd/server/shutdown.go` (new, 77 lines), `cmd/server/shutdown_test.go` (new,
334 lines), `cmd/server/main.go` (+28/-8, signal wiring), `cmd/server/README.md`
(+5, shutdown contract), `docs/decisions/2026-08-18-phase-38x-3d-graceful-drain-shutdown.md`
(new ADR, 122 lines), `README.md` (architecture row).

## What Was Built

1. **Graceful drain seam (`cmd/server/shutdown.go`).** `runServer` serves a
   pre-bound listener until a signal arrives on the injected channel, then
   `drainAndExit` runs `http.Server.Shutdown` under a drain bound: the
   listener closes immediately (new traffic stops) and in-flight handlers may
   finish. `http.Server.Shutdown` never cancels request contexts, so a
   governed query's existing five-second deadline and its two-second
   Evidence Persistence Window complete naturally during the drain.
2. **Fixed ten-second bound.** `shutdownDrainTimeout = 10 * time.Second` is a
   constant — the existing five-second query deadline plus the two-second
   evidence window plus scheduling margin. It is a product invariant, not
   environment configuration; `runServer` accepts an injectable bound only so
   tests can prove the bound terminates the wait, and `main` always passes 0
   (selecting the constant). No retry, queue, worker, or disk buffer was
   added — the drain only waits.
3. **Exit-code and log contract.** A clean drain logs exactly one fixed
   message (`ControlHub shutdown drain complete; exiting`) and exits 0.
   Deadline exhaustion (after force-releasing remaining connections via
   `srv.Close`), HTTP server failure, and a second signal each log exactly
   one fixed message and exit 1. No message interpolates an error value,
   request, statement, target, credential, DSN, or raw failure detail. The
   process can never wait indefinitely, and forced second signal / `kill -9` /
   crash / host loss / power loss remain outside the durability guarantee.
4. **Main wiring (`cmd/server/main.go`).** `signal.Notify` registers SIGTERM
   and SIGINT; the router is served from a pre-bound listener; exit code
   flows through the `runServer` seam via `os.Exit`.
5. **Lifecycle tests (`cmd/server/shutdown_test.go`, 8 tests).** Traffic
   stop (listener refuses new dials while a handler is still active),
   successful drain of an in-flight handler with a completed response, drain
   bound exhaustion (exit 1, elapsed ≥ injected 40 ms bound, bounded ceiling),
   second-signal immediate exit, HTTP server failure (exit 1), the
   single-fixed-log contract on every terminal outcome (captured via injected
   `logf`), and real SIGTERM/SIGINT wiring delivered to the test process.
   Coordination is deterministic via channels; only the 40 ms timeout test
   waits on its injected bound.

## Candidate Gates (exact product SHA `11f3237`)

All executed in the issue-37 worktree at `HEAD=11f3237197f67b426659790f498359f0e3d4f097`.

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, exit 0, 1816 tests, 14 packages |
| `go test -race -count=1 ./...` | PASS, exit 0, 1816 tests, no data races |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./internal/integration/` | PASS, clean |
| `go build ./...` | PASS, clean |
| `go test ./internal/openapi` | PASS, 12 tests (OpenAPI YAML validity; no OpenAPI change — no new endpoint/table/status enum) |
| `make test-integration` | PASS, exit 0, 389 passed (238 top-level + 151 subtests) / 0 failed / 0 skipped (Testcontainers mysql:8.0) |
| `make test-openapi-fuzz` | PASS, exit 0, exactly 1 test (`TestOpenAPIFuzz`), Schemathesis 4.15.2, checks clean |
| `check_three_level_doc.sh` | PASS on the full issue change set (L3 headers, `cmd/server/README.md` + root `README.md` synchronized; ADR included) |
| `gofmt -l` (changed files) | Clean |

The existing no-leak suites (statement previews, template values, credentials,
DSNs, raw driver errors) run inside the above unit and real-MySQL integration
gates and are unchanged by this delivery, which introduces no new persistence
surface or log sink.

## Two-Axis Code Review (committed range `0f8e439...11f3237`)

Independent Standards and Spec reviews ran as parallel read-only sub-agents on
the committed range.

- Round 1 — Spec: blocker (failure path emitted the signal-received message
  plus the failure log — two messages — contrary to the "only a fixed safe
  log" AC, and no test asserted log content); medium note (evidence-completion
  criterion proven with a generic in-flight handler, not a real MySQL query).
  Standards: hard/high (tests claimed the fixed-log contract but never
  captured or asserted logs — AGENTS.md Rule 9/12); hard/medium (client GET
  errors were discarded before an unbounded `<-inFlight`; a startup regression
  could hang).
- Fixes applied in the same delivery: single fixed constant log message per
  terminal outcome with an injected `logf` seam; `assertOneMessage` asserts
  exact text and single count on all four outcomes; `waitInFlight` fails fast
  on client error and gains a bounded timeout arm; L3 `input:` header made
  import-accurate.
- Round 2 — Spec: APPROVE. Standards: ITERATE on two findings (timeout arm
  for `waitInFlight`; stale `log` in the L3 header), both fixed.
- Round 3 — Standards: APPROVE, no new hard violations or blocking smells.
- Final verdicts: **Spec APPROVE, Standards APPROVE**; no unresolved P1/P2.

## Acceptance Criteria Status

All twelve #37 ACs are met:

- SIGTERM and SIGINT stop new traffic and begin graceful shutdown —
  `TestRunServer_TerminationSignalsBeginGracefulDrain` (real signals) and
  `TestRunServer_SignalStopsTrafficAndDrainsInFlightRequest` (traffic stop
  while the handler is still active).
- Existing handlers may finish for at most ten seconds — fixed
  `shutdownDrainTimeout`; `TestRunServer_DrainDeadlineExhaustionExitsNonZero`
  proves the bound terminates the wait (exit 1).
- The ten-second bound is fixed, not environment-configurable — code
  constant; only the test seam injects shorter bounds, production always
  passes 0.
- A query with in-flight evidence can complete during normal drain — see
  judgement call below; in-flight handler completes with a full response and
  exit 0.
- Clean drain exits successfully — exit 0 with the single clean-drain log.
- Shutdown deadline exhaustion or server failure emits only a fixed safe log
  and exits nonzero — exactly one constant message per outcome, asserted by
  `assertOneMessage`; exit codes 1.
- Shutdown never waits indefinitely and introduces no background queue or
  disk buffer — bounded `Shutdown` context; drain-only design; second signal
  forces immediate exit.
- Crash, host loss, power loss, forced second signal, and `kill -9` remain
  explicitly outside the durability guarantee — ADR Context/Decision 5.
- Lifecycle tests prove traffic stop, successful drain, timeout, and bounded
  completion without wall-clock sleeps where deterministic coordination is
  possible — channel-coordinated handler entry/release; the only timed wait
  is the 40 ms injected drain bound itself.
- The accepted shutdown and persistence trade-offs are recorded in the ADR
  and module documentation — `docs/decisions/2026-08-18-phase-38x-3d-graceful-drain-shutdown.md`
  and `cmd/server/README.md`.
- Unit, race, build, integration, no-leak, and documentation gates pass —
  candidate gate table above.

Deliberate judgement calls recorded for traceability:

- **Evidence-completion proof at the lifecycle seam, not full-stack.** The
  "query with in-flight evidence can complete during normal drain" criterion
  is proven by (i) an in-flight handler completing with a delivered response
  during the drain in the lifecycle tests — the production
  `http.Server.Shutdown` path — combined with (ii) `Shutdown` never cancelling
  request contexts and (iii) the unchanged two-second Evidence Persistence
  Window being strictly inside the ten-second bound. A full-stack real-MySQL
  re-verification of the end-to-end evidence chain is deliberately deferred to
  the successor verification ticket #38 (38X-3E, ready-for-agent).
- **Injectable drain bound and log function.** `runServer` takes
  `drain time.Duration` and `logf func(string, ...any)` so tests are
  deterministic; `main` always passes `0` (the fixed constant) and
  `log.Printf`, so the product contract stays fixed and non-configurable.
- **No integration test added.** The lifecycle seam lives in `cmd/server`
  (package main); the unit tests exercise the exact production
  `http.Server.Shutdown` code path including real SIGTERM/SIGINT delivery.

## CI

| Run | Head | Result |
| --- | --- | --- |
| [32088870770](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32088870770) | `92b20d8` (feat + candidate evidence) | Both jobs success: `release-local-gates` ✓, `release-docker-gates` ✓ |

The docs-only follow-up run for the final pushed head is appended below once it completes.

## Root Worktree Preservation

The ROOT worktree (`~/GolangProjects/ControlHub`, still at `3af5d29`) was not
touched by this delivery; its pre-existing dirty paths (modified `CLAUDE.md`,
`advisor-plans/README.md`; untracked user WIP docs incl. a local
`CONTEXT.md`, `AGENTS.md.bak*`, `CLAUDE.md.bak*`, `docs/agents/`, two decision
records, two superpowers plans/specs) remain untouched in place.