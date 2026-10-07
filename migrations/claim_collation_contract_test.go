// Package migrations_test verifies migration SQL contracts without a database.
// input: os, strings, and testing
// output: migration 00031 claim-identity exact collation and guarded-rollback contract tests
// pos: Fast pre-MySQL regression coverage for the execution-claim opaque-key identity storage
// note: if this file changes, update this header and module README.md.
package migrations_test

import (
	"os"
	"strings"
	"testing"
)

func TestClaimCollationMigrationContract(t *testing.T) {
	raw, err := os.ReadFile("00031_claim_identity_collation.sql")
	if err != nil {
		t.Fatalf("read migration 00031: %v", err)
	}
	sql := strings.ToLower(strings.Join(strings.Fields(string(raw)), " "))

	for _, clause := range []string{
		"modify client_execution_id varchar(64) not null collate utf8mb4_0900_bin",
		"modify database_name varchar(128) not null default '' collate utf8mb4_0900_bin",
		"modify schema_name varchar(128) not null default '' collate utf8mb4_0900_bin",
		"modify request_digest char(64) not null collate utf8mb4_0900_bin",
		"modify client_execution_id varchar(64) null collate utf8mb4_0900_bin",
		"create procedure guard_claim_collation_down_31()",
		"signal sqlstate '45000'",
		"exists (select 1 from query_execution_claims limit 1)",
		"exists (select 1 from query_executions where client_execution_id is not null limit 1)",
		"cannot roll back migration 00031 while claim or keyed-execution data exists",
	} {
		if !strings.Contains(sql, clause) {
			t.Errorf("migration 00031 missing contract clause %q", clause)
		}
	}

	// The downgrade must run its guards before any DDL reverts the collation,
	// so a dangerous rollback can never partially apply.
	callIdx := strings.Index(sql, "call guard_claim_collation_down_31()")
	downDDL := strings.Index(sql[callIdx+1:], "collate utf8mb4_0900_ai_ci")
	if callIdx < 0 || downDDL < 0 {
		t.Fatal("migration 00031 down must run the collision guard before collation-revert DDL")
	}
	if strings.Contains(sql, "drop table") || strings.Contains(sql, "truncate") || strings.Contains(sql, "delete from") {
		t.Fatal("migration 00031 must not delete or rewrite claim/execution data")
	}
}
