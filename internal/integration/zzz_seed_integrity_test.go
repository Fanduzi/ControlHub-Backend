//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/fan/controlhub/internal/repository/mysql"
)

// TestSeedClusterIntactAfterSuite is a regression guard against shared-DB
// contamination by tests that freely mutate the globalEnv seed database
// (historically TestOpenAPIFuzz, which ran Schemathesis writes against
// globalEnv). It MUST run after such tests in source order (the zzz_ file
// prefix makes it the last test in the package) so it observes whatever state
// they leave behind.
//
// WHY this matters: TestResourceRepository_DatabaseClusterOperationalSummary
// asserts analytics-ch-cluster-prod has ReplicaMemberCount == 2, computed from
// member profiles whose role is 'replica'. When a prior test blanked or deleted
// those seed profile rows, the rollup dropped to 1 and the suite flaked. This
// test encodes that invariant directly and fails loudly if shared seed data is
// mutated — independent of the rollup query path — so contamination cannot
// hide behind a later test's tolerance.
func TestSeedClusterIntactAfterSuite(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	cluster, err := resRepo.GetResource(lookupResourceID(t, db, "analytics-ch-cluster-prod"))
	if err != nil {
		t.Fatalf("get analytics-ch-cluster-prod: %v", err)
	}
	if cluster.DatabaseOperationalSummary == nil {
		t.Fatal("expected DatabaseOperationalSummary for analytics-ch-cluster-prod")
	}
	if got := cluster.DatabaseOperationalSummary.ReplicaMemberCount; got != 2 {
		t.Errorf("analytics-ch-cluster-prod ReplicaMemberCount = %d, want 2 (seed replica profiles must not be blanked/deleted by other tests)", got)
	}
	if got := cluster.DatabaseOperationalSummary.MemberCount; got != 2 {
		t.Errorf("analytics-ch-cluster-prod MemberCount = %d, want 2", got)
	}

	// Directly assert each member's profile role survived: 'replica' is the
	// value the seed migration plants. An empty role here is exactly the
	// corruption signature that broke the rollup.
	for _, name := range []string{"analytics-ch-node-01-prod", "analytics-ch-node-02-prod"} {
		var role string
		err := db.QueryRowContext(ctx,
			`SELECT pi.role FROM resource_profiles_database_instance pi
			  JOIN resources r ON r.id = pi.resource_id
			  WHERE r.name = ?`, name).Scan(&role)
		if err != nil {
			t.Fatalf("read profile role for %s: %v", name, err)
		}
		if role != "replica" {
			t.Errorf("seed profile role for %s = %q, want %q (must survive the full suite)", name, role, "replica")
		}
	}
}
