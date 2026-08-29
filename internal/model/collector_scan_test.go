// Package model provides domain entities for the resource management system.
// input: testing and time
// output: collector complete-scan lifecycle transition contract tests
// pos: Pure regression coverage for durable collector CI state
// note: if this file changes, update this header and module README.md.
package model

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestApplyCollectorScanThirdCompleteOmissionBecomesMissing(t *testing.T) {
	state := CollectorCIState{LastSeenScanID: "scan-0"}

	for scanNumber := 1; scanNumber <= 3; scanNumber++ {
		completedAt := time.Date(2026, 8, 30, 12, scanNumber, 0, 0, time.UTC)
		var err error
		state, err = ApplyCollectorScan(state, CollectorScan{
			ID:          fmt.Sprintf("scan-%d", scanNumber),
			Result:      CollectorScanResultComplete,
			CompletedAt: completedAt,
		}, false)
		if err != nil {
			t.Fatalf("complete omission %d: %v", scanNumber, err)
		}
		if got, want := state.ConsecutiveCompleteScanOmissions, uint8(scanNumber); got != want {
			t.Fatalf("omission %d count = %d, want %d", scanNumber, got, want)
		}
		if scanNumber < 3 && state.MissingSince != nil {
			t.Fatalf("omission %d marked missing early at %v", scanNumber, state.MissingSince)
		}
		if scanNumber == 3 && (state.MissingSince == nil || !state.MissingSince.Equal(completedAt)) {
			t.Fatalf("third omission missingSince = %v, want %v", state.MissingSince, completedAt)
		}
	}
}

func TestApplyCollectorScanIgnoresNonCompleteAndRepeatedScans(t *testing.T) {
	missingSince := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	initial := CollectorCIState{
		ConsecutiveCompleteScanOmissions: 3,
		LastSeenScanID:                   "scan-seen",
		LastCompletedScanID:              "scan-complete",
		MissingSince:                     &missingSince,
	}

	for _, scan := range []CollectorScan{
		{ID: "scan-incomplete", Result: CollectorScanResultIncomplete, CompletedAt: missingSince.Add(time.Hour)},
		{ID: "scan-failed", Result: CollectorScanResultFailed, CompletedAt: missingSince.Add(2 * time.Hour)},
		{ID: "scan-complete", Result: CollectorScanResultComplete, CompletedAt: missingSince.Add(3 * time.Hour)},
	} {
		got, err := ApplyCollectorScan(initial, scan, false)
		if err != nil {
			t.Fatalf("ApplyCollectorScan(%s): %v", scan.Result, err)
		}
		if !reflect.DeepEqual(got, initial) {
			t.Fatalf("scan %q changed state: got %#v, want %#v", scan.ID, got, initial)
		}
	}
}

func TestApplyCollectorScanRediscoveryClearsMissingState(t *testing.T) {
	missingSince := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	state := CollectorCIState{
		ConsecutiveCompleteScanOmissions: 3,
		LastSeenScanID:                   "scan-old",
		LastCompletedScanID:              "scan-old",
		MissingSince:                     &missingSince,
	}
	completedAt := missingSince.Add(time.Hour)

	got, err := ApplyCollectorScan(state, CollectorScan{ID: "scan-new", Result: CollectorScanResultComplete, CompletedAt: completedAt}, true)
	if err != nil {
		t.Fatalf("rediscovery: %v", err)
	}
	if got.ConsecutiveCompleteScanOmissions != 0 || got.MissingSince != nil || got.LastSeenScanID != "scan-new" || got.LastCompletedScanID != "scan-new" {
		t.Fatalf("rediscovered state = %#v, want reset with truthful scan IDs", got)
	}
}

func TestApplyCollectorScanDoesNotAgeNeverSeenCI(t *testing.T) {
	completedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	got, err := ApplyCollectorScan(CollectorCIState{}, CollectorScan{ID: "scan-1", Result: CollectorScanResultComplete, CompletedAt: completedAt}, false)
	if err != nil {
		t.Fatalf("never-seen CI: %v", err)
	}
	if got != (CollectorCIState{}) {
		t.Fatalf("never-seen CI state = %#v, want unchanged", got)
	}
}

func TestCollectorScanLedgerEntryMatchesOnlyIdenticalRetry(t *testing.T) {
	hash := [32]byte{1, 2, 3}
	entry := CollectorScanLedgerEntry{PayloadHash: hash, Result: CollectorScanResultComplete}
	if !entry.MatchesRetry(hash, CollectorScanResultComplete) {
		t.Fatal("identical retry did not match")
	}
	conflictingHash := hash
	conflictingHash[0]++
	if entry.MatchesRetry(conflictingHash, CollectorScanResultComplete) || entry.MatchesRetry(hash, CollectorScanResultFailed) {
		t.Fatal("conflicting payload or result matched completed ledger entry")
	}
}
