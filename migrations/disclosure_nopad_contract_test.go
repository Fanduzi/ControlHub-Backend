// Package migrations_test verifies migration SQL contracts without a database.
// input: os, strings, and testing
// output: migration 00030 disclosure NO PAD collation and guarded-rollback contract tests
// pos: Fast pre-MySQL regression coverage for disclosure canonical-identity storage
// note: if this file changes, update this header and module README.md.
package migrations_test

import (
	"os"
	"strings"
	"testing"
)

func TestDisclosureNoPadMigrationContract(t *testing.T) {
	raw, err := os.ReadFile("00030_disclosure_policy_nopad_collation.sql")
	if err != nil {
		t.Fatalf("read migration 00030: %v", err)
	}
	sql := strings.ToLower(strings.Join(strings.Fields(string(raw)), " "))

	for _, clause := range []string{
		"modify database_name varchar(128) not null collate utf8mb4_0900_bin",
		"modify schema_name varchar(128) not null default '' collate utf8mb4_0900_bin",
		"modify object_name varchar(128) not null collate utf8mb4_0900_bin",
		"modify column_name varchar(128) not null collate utf8mb4_0900_bin",
		"create procedure guard_disclosure_nopad_down_30()",
		"octet_length(database_name) <> octet_length(trim(trailing ' ' from database_name))",
		"octet_length(schema_name) <> octet_length(trim(trailing ' ' from schema_name))",
		"octet_length(object_name) <> octet_length(trim(trailing ' ' from object_name))",
		"octet_length(column_name) <> octet_length(trim(trailing ' ' from column_name))",
		"signal sqlstate '45000'",
		"cannot roll back migration 00030 while disclosure policies carry trailing-space canonical names",
		"cannot roll back migration 00030 while disclosure policies distinguish names only by trailing spaces",
	} {
		if !strings.Contains(sql, clause) {
			t.Errorf("migration 00030 missing contract clause %q", clause)
		}
	}

	// The downgrade must run its collision guard before any DDL reverts the
	// collation, so a dangerous rollback can never partially apply.
	callIdx := strings.Index(sql, "call guard_disclosure_nopad_down_30()")
	downDDL := strings.Index(sql, "collate utf8mb4_bin")
	if callIdx < 0 || downDDL < 0 || callIdx > downDDL {
		t.Fatal("migration 00030 down must run the collision guard before collation-revert DDL")
	}
	if strings.Contains(sql, "drop table") || strings.Contains(sql, "truncate") {
		t.Fatal("migration 00030 must not delete or truncate policy data")
	}
}
