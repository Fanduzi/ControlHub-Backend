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

This decision records the evidence base, the lowest supported deployment
baseline, the acceptance budget, and the fail-loud handling path. It does
not change the production Argon2id parameters and does not alter the legacy
successful-login migration path.

The baseline was established by the repository owner on 2026-08-14 after an
independent review (P1 finding on the first draft): the draft selected the
GitHub Actions runner only as a reference environment and explicitly
renounced equivalence to any deployment, which Issue #27 does not permit.
The owner therefore accepted the 2-vCPU runner class as ControlHub's
lowest supported deployment baseline (section 1 below), making the CI
environment verifiably equivalent to it by construction.

## Evidence Base

### No deployment specification existed before this decision

An exhaustive search of the tracked repository found no documented deployment
specification: `README.md`, `Makefile`, `.env.example`, `.github/`, `docs/`
(decisions, releases, evidence, quality baseline, hardening checklist), and
`cmd/` contain no CPU, vCPU, RAM, memory, OS, or instance-tier requirement.
No Dockerfile, docker-compose, Kubernetes, or deployment manifest exists in
the repository or its history. A sibling `ControlHub-Frontend` checkout
contains no backend deployment specification either. Because no assumed
specification could be used, the owner established the baseline below.

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

1. **Lowest supported deployment baseline (owner-accepted 2026-08-14):**
   ControlHub's lowest supported deployment specification is hardware at
   least as capable as the GitHub Actions standard Linux runner used by
   backend CI — 2 vCPU (x86_64), 8 GB RAM, 14 GB SSD, Ubuntu 24.04 LTS. The
   CI environment is verifiably equivalent to this baseline by construction:
   `.github/workflows/backend-ci.yml` runs the release gates on exactly this
   hardware class, GitHub publishes the runner facts, and the exact image of
   every run is recorded in that run's "Set up job" log. The acceptance
   budget is therefore measured directly on the lowest supported deployment
   itself. A deployment weaker than this baseline is outside the supported
   deployment contract and must be escalated per the handling path below.
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
   input nor the hash is ever logged. The gate is build-tagged
   (`//go:build budget`) and runs only as the dedicated single-package
   measurement (`make argon2id-budget`, equivalently `go test -tags=budget
   ./internal/service -run '^TestArgon2idVerificationBudget$' -count=1 -v`).
   It is deliberately excluded from `go test ./...`: that command runs
   packages in parallel (`-p` = number of CPUs), and Argon2id wall time under
   package co-tenancy is inflated roughly twofold, producing flaky breaches
   that are not budget failures. Evidence: push CI run 31818969782 measured
   median 186.8 ms / p95 366.9 ms (min 183.6 ms) inside `go test ./...` on
   the 2-vCPU runner, while the same head's dedicated measurement passed 4 of
   4 runs at median 82.5-122.4 ms / p95 85.8-123.5 ms.
4. **Fail loud:** the gate is a Go test that fails (non-zero exit) when the
   median or p95 threshold is exceeded. It runs in the local
   `make argon2id-budget` target and as a dedicated CI step that uploads the
   raw output as a CI artifact (`.argon2id-budget/raw-output.txt`); it is
   intentionally not part of the default `go test ./...` suite (see
   collection method above).
5. **Handling path on breach (never lower the security parameters):**
   Argon2id `m=65536,t=3,p=1` is fixed by Issue #20 and this decision; a
   breach is never fixed by reducing cost. Instead: (a) re-measure with
   warmed caches and the dedicated single-package command; (b) record the
   environment state (runner image from the "Set up job" log, `goos/goarch/
   ncpu` reported by the test); (c) escalate the deployment-spec mismatch —
   either the environment is not the documented target, or the documented
   target cannot meet the accepted budget and the budget/deployment contract
   must be renegotiated with the issue owner before any parameter change.
   In particular, a deployment on hardware weaker than the 2-vCPU baseline is
   not a supported deployment and does not by itself trigger a parameter
   change.
6. **Local measurements** (e.g. an Apple M4 Pro) are at most upper-bound
   sanity references and are never acceptance evidence for the budget.

## Consequences

- The lowest supported deployment baseline (2 vCPU x86_64, 8 GB RAM, 14 GB
  SSD, Ubuntu 24.04) is now part of the supported-deployment contract: any
  release must pass the budget gate on that baseline, and every backend
  release re-proves it via the dedicated CI gate with a reproducible raw
  artifact.
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
