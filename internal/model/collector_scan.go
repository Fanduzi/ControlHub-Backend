// Package model provides domain entities for the resource management system.
// input: fmt and time
// output: collector scan ledger values, per-principal read projections, and pure per-CI lifecycle transitions
// pos: Domain contract for complete-scan omission, retry semantics, and operator-visible collector presence
// note: if this file changes, update this header and module README.md.
package model

import (
	"fmt"
	"time"
)

type CollectorScanResult string

const (
	CollectorScanResultComplete   CollectorScanResult = "complete"
	CollectorScanResultIncomplete CollectorScanResult = "incomplete"
	CollectorScanResultFailed     CollectorScanResult = "failed"
	MaxCompleteScanOmissions                          = uint8(3)
)

type CollectorPresenceStatus string

const (
	CollectorPresenceStatusPresent CollectorPresenceStatus = "present"
	CollectorPresenceStatusMissing CollectorPresenceStatus = "missing"
)

type CollectorPresence struct {
	Status               CollectorPresenceStatus `json:"status"`
	Source               string                  `json:"source"`
	MachinePrincipalID   uint64                  `json:"machinePrincipalId"`
	MachinePrincipalName string                  `json:"machinePrincipalName"`
	MissingSince         *time.Time              `json:"missingSince"`
}

type CollectorScan struct {
	ID          string
	Result      CollectorScanResult
	CompletedAt time.Time
}

type CollectorScanLedgerEntry struct {
	MachinePrincipalID uint64
	CollectorScanID    string
	PayloadHash        [32]byte
	Result             CollectorScanResult
	CompletedAt        time.Time
}

func (e CollectorScanLedgerEntry) MatchesRetry(payloadHash [32]byte, result CollectorScanResult) bool {
	return e.PayloadHash == payloadHash && e.Result == result
}

type CollectorCIState struct {
	MachinePrincipalID               uint64
	ResourceID                       uint64
	ConsecutiveCompleteScanOmissions uint8
	LastSeenScanID                   string
	LastCompletedScanID              string
	MissingSince                     *time.Time
}

func ApplyCollectorScan(state CollectorCIState, scan CollectorScan, seen bool) (CollectorCIState, error) {
	if scan.ID == "" || scan.CompletedAt.IsZero() {
		return state, fmt.Errorf("collector scan ID and completion time are required")
	}
	if scan.Result != CollectorScanResultComplete && scan.Result != CollectorScanResultIncomplete && scan.Result != CollectorScanResultFailed {
		return state, fmt.Errorf("collector scan result is not supported")
	}
	if state.ConsecutiveCompleteScanOmissions > MaxCompleteScanOmissions {
		return state, fmt.Errorf("complete-scan omissions exceed the durable cap")
	}
	if scan.Result == CollectorScanResultComplete && state.LastCompletedScanID == scan.ID {
		return state, nil
	}
	if seen {
		state.LastSeenScanID = scan.ID
		state.ConsecutiveCompleteScanOmissions = 0
		state.MissingSince = nil
		if scan.Result == CollectorScanResultComplete {
			state.LastCompletedScanID = scan.ID
		}
		return state, nil
	}
	if scan.Result != CollectorScanResultComplete || state.LastSeenScanID == "" {
		return state, nil
	}
	if state.ConsecutiveCompleteScanOmissions < MaxCompleteScanOmissions {
		state.ConsecutiveCompleteScanOmissions++
	}
	state.LastCompletedScanID = scan.ID
	if state.ConsecutiveCompleteScanOmissions == MaxCompleteScanOmissions && state.MissingSince == nil {
		missingSince := scan.CompletedAt
		state.MissingSince = &missingSince
	}
	return state, nil
}
