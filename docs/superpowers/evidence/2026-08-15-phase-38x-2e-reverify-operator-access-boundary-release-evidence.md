# Phase 38X-2E Re-verification Release Evidence (corrected)

Date: 2026-08-15
Issue: [#25, `38X-2E: Re-verify the hardened Operator Access Boundary`](https://github.com/Fanduzi/ControlHub-Backend/issues/25)

Status: **ready for human review** — this evidence corrects the earlier
blocked-with-evidence record. An independent mathematical re-verification
determined that the previously reported Security P2 (session-key
low-diversity guard "missing" non-power-of-two repeating periods) is a
**false positive**: for the only admissible 32-byte key length, the complete
block-repetition periods are exactly the proper divisors of 32, which are
exactly `{1, 2, 4, 8, 16}` — the precise set the guard checks. Fresh
fresh-context Standards, Spec, and Security reviews of the published refs
all conclude P1=0, P2=0. #25, #20, and #7 remain open; nothing was merged,
pushed, or closed.

## Exact Refs

| Item | Value |
| --- | --- |
| Backend repository | `Fanduzi/ControlHub-Backend` |
| Backend published ref (`origin/main`) | `f276be8a1561e96a0d14441748f3017d79747060` (re-fetched 2026-08-15T08:41Z; includes closed #28 evidence) |
| Backend CI run for published ref | `31870058199` — `release-local-gates` (job `94977192123`) and `release-docker-gates` (job `94977192138`) both `success` |
| Frontend repository | `Fanduzi/ControlHub-Frontend` |
| Frontend published ref (`origin/main`) | `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3` (re-fetched 2026-08-15T08:41Z) |
| Frontend CI run for published ref | `31716088251` — `release-local` (job `94501078628`) and `release-e2e` (job `94501078810`) both `success` |
| Backend task worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-25-math-20260815-164155` |
| Backend task branch | `issue-25-math-20260815-164155` (created from `origin/main` at `f276be8a…`; HEAD at creation `f276be8a1561e96a0d14441748f3017d79747060`) |
| Frontend task worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-math-20260815-164155` |
| Frontend task branch | `issue-25-math-fe-20260815-164155` (created from `origin/main` at `d6bc7520…`) |
| Superseded record | the earlier blocked evidence `docs/superpowers/evidence/2026-08-15-phase-38x-2e-reverify-operator-access-boundary-blocked-evidence.md` on branch `issue-25-reverify-20260815-152736` (commit `580446a`); the correction below replaces its P2 conclusion |

Tracker state at preflight and at evidence time: #21, #22, #23, #24, #26,
#27, #28 closed; #25, #20, #7 open. No issue was commented on, closed,
relabeled, or otherwise modified.

## Mathematical Re-verification of `hasRepeatingPattern` (the corrected point)

### Prior claim (now adjudicated false positive)

A prior independent Security review claimed that
`lib/operator-session/config.ts` `hasRepeatingPattern` "misses" repeating
periods `3, 5, 6, 7, 9–15, 31` for 32-byte keys because the loop only
iterates periods `1, 2, 4, 8, 16`, and that this violates the ADR §3
requirement that session-sealing keys be "exactly 32 random bytes".

### Verified facts (fresh read-only derivation against the published ref)

1. **Key length is fixed at 32 bytes.** `decodeKeyMaterial`
   (`lib/operator-session/config.ts:43–53`) requires the canonical base64
   shape `/^[A-Za-z0-9+/]{43}=$/` (44 chars) and rejects anything that does
   not decode to exactly 32 bytes (`bytes.length !== 32 → null`). Only
   32-byte keys reach `hasRepeatingPattern`.

2. **The guard's semantics is complete-block repetition.**
   `hasRepeatingPattern` (`config.ts:19–41`) iterates `period ∈ {1,2,4,8,16}`
   and, for each, compares every complete `period`-sized block
   `buf[i..i+period)` (`i = period, 2·period, …`) against the first block
   `buf[0..period)`. It returns true exactly when the whole 32-byte key is
   `k ≥ 2` complete repetitions of one shorter block.

3. **A 32-byte string is a complete repetition of a block of length `d`
   (key = block^k, k ≥ 2) iff `d` divides 32.** The proper divisors of 32
   are exactly `{1, 2, 4, 8, 16}` — the precise set the loop checks.
   (d = 32 is the whole key as a single block; every key trivially
   qualifies, so it is not a repetition pattern and is correctly excluded
   by `period <= 16`.)

4. **Periods not dividing 32 define an empty pattern class over 32-byte
   strings.** No 32-byte string can be a complete repetition of a 3-, 5-,
   6-, 7-, 9–15-, or 31-byte block, because 32/d is not an integer. A key
   such as the byte sequence `1,2,3,1,2,3,…,1,2` (32 bytes) is a
   truncated/prefix repetition — a different, per-byte modulo-equality
   property — not the complete-block class the guard defines. The prior
   claim conflated these two classes.

5. **Documented scope is consistent across code, README, and tests:**
   - Code comment (`config.ts:21–26`): "catches single-byte, two-byte,
     four-byte, eight-byte, and sixteen-byte repeating patterns (periods 1,
     2, 4, 8, 16) … a structural check — it rejects obvious repeating
     patterns — not an entropy measurement."
   - README (`lib/operator-session/README.md:29–34`): "keys with a short
     repeating pattern (periods 1, 2, 4, 8, or 16 bytes) … a structural
     check that catches obvious low-diversity material; it is not an
     entropy measurement and does not guarantee cryptographic quality."
   - Tests (`tests/lib/operator-session-config.test.ts:89–106`): period-1
     all-zeros key rejected; period-2 two-byte alternating cycle rejected;
     no test claims rejection of truncated or non-divisor periods.

6. **No false positives on random keys.** For each checked divisor `d`, a
   random 32-byte key matches all complete blocks with probability
   `2^(−8·32·(d−1)/d)` (e.g., `2^−128` for d=16) — negligible.

7. **The ADR §3 contract** ("accepts session-sealing keys only as
   base64-encoded values that decode to exactly 32 random bytes") is
   enforced by canonical base64 + exact 32-byte decode + the structural
   complete-block guard — the exact mechanism #23 added to close the #7
   2-byte-repeating P2. The ADR does not mandate an entropy estimator or a
   diversity threshold, and the code/README explicitly disclaim one.

### Adjudication

The checked set `{1,2,4,8,16}` equals the complete set of all possible
complete-repetition periods for the only admissible key length (32 bytes).
No pattern in the guard's defined class is missed; the "missed" periods
cannot be complete-repetition periods over a 32-byte key at all. Requiring
their rejection would demand an entropy/diversity estimator that the
accepted contract and the documented semantics do not require. The prior
P2 is therefore a **false positive**. Empirically confirmed on the
published module: complete-block keys of block sizes 1/2/4/8/16 × integer
counts are all rejected; a random high-entropy key is accepted.

A non-blocking documentation nit (P3) was noted by the Security review:
the comment phrase "obvious repeating patterns" (`config.ts:21`) is broader
than the implemented complete-block semantics; rewording to "complete
repetitions at periods dividing the key length" would prevent future
misreadings. No code or test change is required, and none was made.

## Re-verification Matrix (unchanged from the fresh run; executed on task-owned worktrees and disposable fixtures)

All runs used the task's own worktrees and per-run fixture operators
provisioned via the published safe seam `cmd/e2e-fixture-bootstrap`,
against a disposable `controlhub_e2e` metadata DB (migrated to 00017) and
task-owned `query_e2e`/`query_e2e_aux` databases seeded via the published
`cmd/querydev` seam. Neither earlier #25 candidate's worktrees, evidence,
services, fixture databases, CI outputs, or review verdicts were reused.

### Backend gates (at `f276be8a…`)

| Gate | Command | Result |
| --- | --- | --- |
| Release-local | `make release-local-gates` | PASS, exit 0 (14 packages `ok`; `go vet`, `go build`, OpenAPI YAML validation PASS) |
| Race | `go test -race -count=1 ./...` | PASS, exit 0 (13 packages `ok`, 0 FAIL, 0 SKIP) |
| Integration (real MySQL) | `make test-integration` | PASS, 220 PASS / 0 FAIL / 0 SKIP, incl. `TestAuthAudit_FailOpenOnDBError`, `TestAuthAudit_FailOpenPreservesRoleDenied403`, `TestAuthAudit_*`, `TestArgon2idMigration_*` |
| OpenAPI fuzz | `make test-openapi-fuzz` | PASS — Schemathesis 4.15.2, 50/51 operations (1 governed exclusion), 2040 generated / 2040 passed, all checks passed |
| Budget gate (local sanity) | `make argon2id-budget` | PASS, exit 0 — `result=PASS samples=20 median=99.186333ms p95=100.202125ms budget_median<=250ms budget_p95<=300ms` (developer machine only; sanity, not acceptance proof) |

Exclusions and non-applicable fuzz phases, fully accounted:
`executeSavedStatement` excluded via the governed single-operation contract
(`scripts/README.md`, mechanically enforced by
`TestOpenAPIFuzzExclusionContract` PASS); Schemathesis run-scoping
`--phases examples,fuzzing` disables Coverage/Stateful by design;
`[warnings] fail-on = []` keeps the three advisory warnings non-failing;
34 "skipped" Examples rows are example-less operations covered by fuzzing.
Integration suite: 0 skips.

### Budget proof on the documented lowest supported deployment (authoritative)

Baseline (`docs/decisions/2026-08-14-phase-38x-2g-argon2id-verification-budget.md`):
2 vCPU x86_64 / 8 GB / 14 GB SSD / Ubuntu 24.04 LTS (GitHub Actions
standard Linux runner class, owner-accepted). Verified from the published
CI for the exact backend ref (run `31870058199`, job `94977192123`):

- `result=PASS samples=20 median=106.434173ms p95=107.795926ms min=105.885409ms max=109.016224ms budget_median<=250ms budget_p95<=300ms`
- `environment goos=linux goarch=amd64 ncpu=2 image_os=ubuntu24`; runner
  Image `ubuntu-24.04`.
- Artifact `argon2id-budget-evidence/raw-output.txt` (sha256
  `15b5bb0154303e6b2c997acbaf1f8d1d2a48703df71a2524292e051a745d2118`)
  content byte-identical (after timestamp normalization) to the job log.
- Cross-check on the dedicated budget run `31852479841` (artifact sha256
  `33db6d299f393ffe3b3d4b1ae715c3b8b9698e55e2da321d5a6bb5f6911af6b9`):
  `median=110.84302ms p95=123.03068ms` — same 2-vCPU ubuntu-24.04 class.
- Breach path (no parameter lowering): with the budget constants
  temporarily set to an impossible 1 ms, `make argon2id-budget` produced
  `result=FAIL samples=20 … budget_median<=1ms budget_p95<=1ms`,
  `--- FAIL`, non-zero make exit; the test file was restored byte-identical
  (sha256 verified). Production Argon2id parameters were never changed.

### Frontend gates (at `d6bc7520…`, Node 22.22.0)

| Gate | Result |
| --- | --- |
| `check:runtime` / `check:e2e-preflight` / `check:e2e-governance` | PASS |
| `npx tsc --noEmit -p tsconfig.json` / `npm run lint` / `npm run build` | PASS |
| `npm run test` (vitest) | PASS — 98 files / 1494 tests, 0 failed, 0 skipped |
| `npm run release:e2e` (real Chromium) | PASS — 7 (smoke) + 3 (interaction) + 176 (full) = **186 passed, 0 failed, 0 skipped**; operator-session BFF spec 13/13 (EN desktop, 375px EN, zh-CN desktop; HttpOnly sealed session; no Backend Bearer in browser storage/DOM/readable cookies; client Authorization rejected; unsafe Origin rejected; forged/tampered/expired fail closed) |

Published frontend CI (run `31716088251`) reports identical totals.

### Live acceptance matrix (task-owned backend :18080, disposable DB)

| Area | Result |
| --- | --- |
| Anonymous 401 on every protected surface with the generic `unauthorized` body; public allowlist (`/health`, `/auth/login`, `/openapi.yaml`, `/docs`) reachable | PASS |
| Editor: read/governed-query 200; mutation/audit/admin-only 403 with controlled `forbidden` body | PASS |
| Admin: audit read, legacy-hash count, auth-audit metrics 200 | PASS |
| Role change, disablement, password reset invalidate prior credentials immediately (401) | PASS |
| Legacy successful-login migration to Argon2id (`$argon2id$…`), new/reset writes Argon2id | PASS |
| Freshness: 1 h old accepted (protected + governed); exactly 8 h rejected 401 (both, no handler execution); 8 h 1 m rejected; obsolete `QUERY_EXECUTION_TOKEN_MAX_AGE` (any value incl. empty) rejected at startup, exit 1 | PASS |
| **Matrix totals** | **37 PASS / 0 FAIL** |

### Audit taxonomy and fail-open (live + real MySQL)

Exact events/results (`auth.login` succeeded/rejected, `auth.bearer`
rejected, `auth.authorization` denied; 0 unexpected `auth.*` types);
trusted-actor attribution (`auth.login succeeded` 19/19 carry the verified
actor; rejected Bearer for unverified/forged credentials carries no actor;
verified-but-expired Bearer carries the verified actor per the ADR rule);
known-target attribution (editor `PATCH /resources/102` → denied with
actor + target 102, resource row unchanged — no handler execution);
successful protected reads excluded; real-MySQL audit-write failure (table
temporarily renamed): valid login still 200, rejected Bearer still 401,
editor-on-admin-route still controlled 403 with no mutation; fail-open
counter `authAuditPersistenceFailures` incremented by exactly the failing
emits; safe log lines only `auth_audit_emit_fail event=<taxonomy>
result=<taxonomy> error_class=audit_persistence_failure` — all PASS.

### Prohibited-value scans

Audit rows (fixed metadata columns only), backend server log, matrix logs,
Schemathesis reports, Playwright output, and this evidence: zero matches
for passwords, password hashes, Bearer credentials, session/session-key
material, DSNs, fixture credentials, IPs, User-Agents, or request values.

## Independent Review Verdicts (fresh-context, read-only, both published refs)

| Review | Verdict | Notes |
| --- | --- | --- |
| Standards | PASS (P1=0, P2=0) | 3 P3 nits, all pre-existing repo-wide conditions (L3-header position below `//go:build` tags; gofmt alignment in one integration test file; `.mjs` scripts without L3 headers) |
| Spec | PASS (P1=0, P2=0) | All four ADR contract points MET with file:line evidence; 4 P3 observations, all documented design |
| Security | PASS (P1=0, P2=0) | The previously reported P2 is adjudicated a **false positive** (math above: complete-block periods for a 32-byte key = proper divisors of 32 = `{1,2,4,8,16}` = the exact checked set); all surfaces reviewed sound; 6 P3 notes incl. one comment-wording clarity nit (no behavior change) |

With all three reviews at P1=0/P2=0, the re-verification satisfies the
closure precondition: #20 may proceed to closure review and #7 may return
to delivery closure per the parent-ticket protocol — pending the issue
owner's review of this evidence.

## Root WIP Preservation

Before fetch or repository mutation, backend root was on `main` at
`f276be8a1561e96a0d14441748f3017d79747060` (the fetched published ref).
The root WIP manifest was captured to
`/tmp/issue25-math-20260815-164155/preflight/backend-root/`
(`wip-tracked.patch`, `wip-staged.patch`, `wip-status.nul`,
`wip-untracked.txt`, `worktrees.txt`, `manifest-sha256.txt`) before any
change; the tracked, staged, NUL-safe status, and untracked manifests are
byte-identical to the state captured by the previous #25 task
(identical sha256 values), confirming the root has been untouched across
both tasks. The frontend checkout was created fresh by this task and is
clean at the published ref. Root services were observation-only; no stash,
restore, reset, clean, or overwrite occurred; pre-existing containers,
servers, and worktrees were not started, stopped, or modified.

## Cleanup

This task created no services, containers, or fixtures (pure
read-only verification of published refs; the verification matrix in the
superseded record was executed and cleaned in the earlier task). The
temporary probe and preflight manifests live under
`/tmp/issue25-math-20260815-164155/` (task-local). Both task worktrees are
clean apart from this evidence commit.

## Issue Safety

No issue was commented on, closed, or relabeled. #25, #20, and #7 remain
open. The task worktrees and branches are retained; removal requires
separate authorization. Nothing was merged, pushed, rebased, amended,
tagged, or deployed.
