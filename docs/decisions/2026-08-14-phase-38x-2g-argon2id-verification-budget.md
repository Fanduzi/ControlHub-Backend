# Decision: Argon2id Verification Budget And Target Environment

**Status:** Accepted

## Context

Issue #20 mandated Argon2id password storage with memory=64MiB, time cost 3,
and parallelism 1 (`m=65536,t=3,p=1`), and required the implementation to
validate an approximately 250 ms verification budget on the lowest supported
deployment specification. Issue #24 delivered the Argon2id migration but
closed without a reproducible proof of that budget; the only measurements
available were local developer machines (an Apple M4 Pro at about 103 ms),
which prove nothing about a lowest supported deployment. Issue #27 requires
the budget proof to be established on a documented target environment.

This decision records the evidence base, the documented target environment,
the acceptance budget, and the fail-loud handling path. It does not change
the production Argon2id parameters and does not alter the legacy
successful-login migration path.

## Evidence Base

### No deployment specification exists

An exhaustive search of the tracked repository found no documented deployment
specification: `README.md`, `Makefile`, `.env.example`, `.github/`, `docs/`
(decisions, releases, evidence, quality baseline, hardening checklist), and
`cmd/` contain no CPU, vCPU, RAM, memory, OS, or instance-tier requirement.
No Dockerfile, docker-compose, Kubernetes, or deployment manifest exists in
the repository. A sibling `ControlHub-Frontend` checkout contains no backend
deployment specification either. There is therefore no assumed specification
that could be used, and none is invented here.

### The documented execution environment is CI

The only documented environment that runs backend release gates is GitHub
Actions: `.github/workflows/backend-ci.yml` runs `release-local-gates` and
`release-docker-gates` on the standard GitHub-hosted `ubuntu-latest` runner.

GitHub documents the standard Linux runner hardware for private/internal
repositories as 2 vCPU (x86_64), 8 GB RAM, 14 GB SSD, running the Ubuntu
24.04 LTS image for the `ubuntu-latest` label (GitHub-hosted runners
reference, docs.github.com). Each job runs on a fresh VM. `ubuntu-latest` is
an alias that may migrate to a newer stable image; the exact image used by a
run is recorded in that run's "Set up job" log and is captured as CI evidence.

## Decision

1. **Target environment for the budget proof:** the GitHub Actions standard
   `ubuntu-latest` runner for this private repository — 2 vCPU (x86_64), 8 GB
   RAM, 14 GB SSD, Ubuntu 24.04 LTS. Rationale: the repository documents no
   deployment specification (evidence above), and this CI environment is the
   only documented execution environment for the backend release gates. It is
   a modest two-vCPU baseline and is verifiable: its hardware is
   GitHub-published and the exact image is recorded per run. This does not
   claim the CI runner equals any production deployment; a deployment weaker
   than this environment must document its own specification, and a budget
   breach on any environment is handled per the handling path below.
2. **Acceptance budget:** median verification time ≤ 250 ms and p95 ≤ 300 ms
   (250 ms + 20% tolerance), measured at the real password-verification seam
   (`VerifyPassword` in `internal/service/password_hasher.go`, the seam used
   by production login at `internal/service/auth_service.go`) against an
   Argon2id hash produced with the exact production parameters
   (`m=65536,t=3,p=1`). Thresholds are written in the test
   (`internal/service/password_hasher_budget_test.go`) and in the release
   evidence, not implied.
3. **Collection method:** one discarded warm-up sample, then 20 measured
   sequential in-process samples (no `t.Parallel`). Median is the mean of the
   two middle sorted samples; p95 uses the nearest-rank method (sample 19 of
   20 sorted). The sample input is a fixed non-secret string; neither the
   input nor the hash is ever logged. The dedicated single-package run
   (`make argon2id-budget`) is the authoritative measurement; a run inside
   `go test ./...` may share CPU with sibling packages and is still a valid
   fail-loud check.
4. **Fail loud:** the gate is a Go test that fails (non-zero exit) when the
   median or p95 threshold is exceeded. It runs in the default test suite
   (`go test ./...`), in the local `make argon2id-budget` target, and as a
   dedicated CI step that uploads the raw output as a CI artifact
   (`.argon2id-budget/raw-output.txt`).
5. **Handling path on breach (never lower the security parameters):**
   Argon2id `m=65536,t=3,p=1` is fixed by Issue #20 and this decision; a
   breach is never fixed by reducing cost. Instead: (a) re-measure with
   warmed caches and the dedicated single-package command; (b) record the
   environment state (runner image from the "Set up job" log, `goos/goarch/
   ncpu` reported by the test); (c) escalate the deployment-spec mismatch —
   either the environment is not the documented target, or the documented
   target cannot meet the accepted budget and the budget/deployment contract
   must be renegotiated with the issue owner before any parameter change.
6. **Local measurements** (e.g. an Apple M4 Pro) are at most upper-bound
   sanity references and are never acceptance evidence for the budget.

## Consequences

- Every backend release re-proves the budget on the documented CI target via
  the dedicated gate, producing a reproducible raw artifact.
- A future PR that slows verification (e.g. changes parameters or adds work
  to the seam) fails CI with a non-zero exit instead of silently drifting
  past the budget.
- The proof unblocks Issue #25's independent re-verification; Issues #20 and
  #7 remain open until that re-verification passes.

## References

- Parent issue: `Fanduzi/ControlHub-Backend#27`
- Decision: `docs/decisions/2026-08-12-phase-38x-authentication-hardening-decisions.md` (§4)
- Production parameters: `internal/service/password_hasher.go`
- Budget gate: `internal/service/password_hasher_budget_test.go`
- CI: `.github/workflows/backend-ci.yml`
- GitHub-hosted runners reference (hardware table): https://docs.github.com/en/actions/reference/runners/github-hosted-runners
