// Package migrations_test verifies migration SQL contracts without a database.
// input: os, strings, and testing
// output: migration 00027 collector ledger/state schema contract tests
// pos: Fast pre-MySQL regression coverage for collector lifecycle persistence
// note: if this file changes, update this header and module README.md.
package migrations_test

import (
	"os"
	"strings"
	"testing"
)

func TestCollectorScanMigrationContract(t *testing.T) {
	raw, err := os.ReadFile("00027_collector_scan_lifecycle.sql")
	if err != nil {
		t.Fatalf("read migration 00027: %v", err)
	}
	sql := strings.ToLower(strings.Join(strings.Fields(string(raw)), " "))

	for _, clause := range []string{
		"create table collector_scan_ledger",
		"payload_hash binary(32) not null",
		"unique key uq_collector_scan_ledger_principal_scan (machine_principal_id, collector_scan_id)",
		"key idx_collector_scan_ledger_principal_completed (machine_principal_id, completed_at)",
		"constraint chk_collector_scan_ledger_scan_id check (collector_scan_id <> '')",
		"constraint chk_collector_scan_ledger_result check (result in ('complete', 'incomplete', 'failed'))",
		"create table collector_ci_scan_states",
		"primary key (machine_principal_id, resource_id)",
		"last_seen_collector_scan_id varchar(255) character set ascii collate ascii_bin not null",
		"last_completed_collector_scan_id varchar(255) character set ascii collate ascii_bin null",
		"key idx_collector_ci_scan_states_resource (resource_id, machine_principal_id)",
		"key idx_collector_ci_scan_states_missing (machine_principal_id, missing_since)",
		"constraint chk_collector_ci_scan_states_last_seen check (last_seen_collector_scan_id <> '')",
		"constraint chk_collector_ci_scan_states_omissions check (consecutive_complete_scan_omissions <= 3)",
		"constraint chk_collector_ci_scan_states_missing check ((consecutive_complete_scan_omissions = 3) = (missing_since is not null))",
	} {
		if !strings.Contains(sql, clause) {
			t.Errorf("migration 00027 missing contract clause %q", clause)
		}
	}
	if strings.Contains(sql, "foreign key") {
		t.Fatal("migration 00027 must preserve application-owned identity integrity without foreign keys")
	}
}
