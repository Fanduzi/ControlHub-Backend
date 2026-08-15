//go:build budget

// Package service provides the Argon2id verification-budget gate that proves
// the production password-verification seam stays within the documented
// budget on the documented target environment. The file is build-tagged
// (`//go:build budget`) so it runs only as the dedicated single-package
// measurement via `make argon2id-budget`; it is excluded from `go test
// ./...` because package-parallel CPU contention inflates Argon2id wall time
// (see docs/decisions/2026-08-14-phase-38x-2g-argon2id-verification-budget.md).
// input: internal/service (VerifyPassword, HashPasswordArgon2id), docs/decisions/2026-08-14-phase-38x-2g-argon2id-verification-budget.md
// output: TestArgon2idVerificationBudget
// pos: Measures multi-sample VerifyPassword wall time with the exact production parameters (m=65536,t=3,p=1) and fails loud if the documented budget is exceeded, without ever lowering the security parameters
// note: if this file changes, update header and README.md
package service

import (
	"math"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"
)

// Documented verification-budget thresholds. These values are part of the
// accepted security contract; see
// docs/decisions/2026-08-14-phase-38x-2g-argon2id-verification-budget.md.
// They must never be raised to make a slow environment pass — the handling
// path for a budget breach keeps the Argon2id parameters unchanged.
const (
	// argon2BudgetMedianMS is the median budget: <= 250 ms.
	argon2BudgetMedianMS = 250
	// argon2BudgetP95MS is the p95 budget: 250 ms + 20% tolerance = <= 300 ms.
	argon2BudgetP95MS = 300
	// argon2BudgetSamples is the number of measured samples collected after
	// one discarded warm-up sample.
	argon2BudgetSamples = 20
)

// TestArgon2idVerificationBudget measures the wall-clock time of
// VerifyPassword against an Argon2id hash produced with the exact production
// parameters (memory=64MiB, time=3, parallelism=1, encoded m=65536,t=3,p=1)
// and fails loud when the documented budget is exceeded. It exercises the
// same seam as production login (auth_service.go: VerifyPassword against a
// stored Argon2id hash).
//
// Collection method: one warm-up verification is discarded, then
// argon2BudgetSamples sequential in-process verifications are timed (no
// t.Parallel). Median is the mean of the two middle sorted samples; p95 uses
// the nearest-rank method. The dedicated single-package run
// (`make argon2id-budget`) is the authoritative measurement; a run inside
// `go test ./...` may share CPU with sibling packages.
//
// The sample input is a fixed non-secret string. Neither the input nor the
// hash is ever logged.
func TestArgon2idVerificationBudget(t *testing.T) {
	const samplePassword = "controlhub-argon2id-budget-sample"

	hash := HashPasswordArgon2id(samplePassword)

	// One discarded warm-up sample moves the first 64 MiB Argon2 block
	// allocation and any first-call effects out of the measurement window.
	if !VerifyPassword(samplePassword, hash) {
		t.Fatal("warm-up verification failed; the verification seam is broken")
	}

	samples := make([]time.Duration, 0, argon2BudgetSamples)
	for i := 0; i < argon2BudgetSamples; i++ {
		start := time.Now()
		if !VerifyPassword(samplePassword, hash) {
			t.Fatal("sample verification failed; the verification seam is broken")
		}
		samples = append(samples, time.Since(start))
	}

	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

	median := (samples[(len(samples)-1)/2] + samples[len(samples)/2]) / 2
	p95 := samples[nearestRankIndex(0.95, len(samples))]

	medianBudget := time.Duration(argon2BudgetMedianMS) * time.Millisecond
	p95Budget := time.Duration(argon2BudgetP95MS) * time.Millisecond

	result := "PASS"
	if median > medianBudget || p95 > p95Budget {
		result = "FAIL"
	}
	t.Logf("argon2id-budget: result=%s samples=%d median=%s p95=%s min=%s max=%s budget_median<=%dms budget_p95<=%dms",
		result, len(samples), median, p95, samples[0], samples[len(samples)-1],
		argon2BudgetMedianMS, argon2BudgetP95MS)
	t.Logf("argon2id-budget: environment goos=%s goarch=%s ncpu=%d image_os=%s",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), os.Getenv("ImageOS"))

	if median > medianBudget || p95 > p95Budget {
		t.Errorf("argon2id verification budget exceeded on this environment: median=%s (> %s) p95=%s (> %s). "+
			"Handling path (Argon2id parameters must NOT be lowered): re-measure with warmed caches, record the "+
			"environment state, then escalate the deployment-spec mismatch. See "+
			"docs/decisions/2026-08-14-phase-38x-2g-argon2id-verification-budget.md.",
			median, medianBudget, p95, p95Budget)
	}
}

// nearestRankIndex returns the 0-based index of the p-th percentile of n
// sorted samples using the nearest-rank method: ceil(p*n)-1.
func nearestRankIndex(p float64, n int) int {
	idx := int(math.Ceil(p*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return idx
}
