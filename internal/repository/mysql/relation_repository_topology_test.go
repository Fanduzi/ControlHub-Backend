// Package mysql provides bounded topology relation repository tests.
// input: database/sql/driver, testing, time, sqlmock, and internal/model
// output: deterministic bounded topology relation, candidate, and one-observation-per-candidate regression coverage
// pos: Verifies topology-only queries apply caller-owned row budgets and bounded health projection before scanning
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"database/sql/driver"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/fan/controlhub/internal/model"
)

func TestRelationRepositoryListTopologyRelationsByResourceIDsUsesDeterministicLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"id", "from_resource_id", "to_resource_id", "relation_type", "created_at"}).
		AddRow(11, 7, 9, "depends_on", time.Unix(1, 0))
	mock.ExpectQuery(`(?s)select id, from_resource_id, to_resource_id, relation_type, created_at\s+from resource_relations\s+where from_resource_id in \(\?, \?\) or to_resource_id in \(\?, \?\)\s+order by relation_type, from_resource_id, to_resource_id, id\s+limit \?`).
		WithArgs(uint64(7), uint64(8), uint64(7), uint64(8), 5).
		WillReturnRows(rows)

	items, err := NewRelationRepository(db).ListTopologyRelationsByResourceIDs([]uint64{7, 8}, model.TopologyDirectionBoth, "", 5)
	if err != nil {
		t.Fatalf("list topology relations: %v", err)
	}
	if len(items) != 1 || items[0].ID != 11 {
		t.Fatalf("items = %+v, want relation 11", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRelationRepositoryListTopologyCandidatesUsesDeterministicLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`(?s)select r\.id,.*from resources r\s+where r\.environment_id = \? and r\.archived_at is null.*order by.*r\.name, r\.id\s+limit \?`).
		WithArgs(uint64(9), sqlmock.AnyArg(), 201).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	items, err := NewRelationRepository(db).ListTopologyCandidates(9, 201)
	if err != nil {
		t.Fatalf("list topology candidates: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRelationRepositoryListTopologyCandidatesReadsOneObservationPerCandidate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	candidates := sqlmock.NewRows([]string{"id", "resource_type", "resource_subtype", "name", "display_name", "environment_id", "owner_id", "lifecycle_status", "health_status", "labels"})
	selectedHealth := sqlmock.NewRows([]string{"resource_id", "health_status", "observed_at", "observer"})
	observedAt := time.Date(2026, time.August, 30, 1, 0, 0, 0, time.UTC)
	for i := 1; i <= 201; i++ {
		id := uint64(i)
		name := fmt.Sprintf("candidate-%03d", i)
		candidates.AddRow(id, "domain_name", "dns", name, name, 9, 1, "running", "critical", "{}")
		selectedHealth.AddRow(id, "healthy", observedAt, "observer")
	}
	mock.ExpectQuery(`(?s)select r\.id,.*from resources r.*limit \?`).
		WithArgs(uint64(9), sqlmock.AnyArg(), 201).
		WillReturnRows(candidates)
	healthArgs := make([]driver.Value, 0, 202)
	healthArgs = append(healthArgs, sqlmock.AnyArg())
	for i := 1; i <= 201; i++ {
		healthArgs = append(healthArgs, uint64(i))
	}
	mock.ExpectQuery(`(?s)row_number\(\) over \(\s*partition by resource_id.*observer.*where observation_rank = 1`).
		WithArgs(healthArgs...).
		WillReturnRows(selectedHealth)

	items, err := NewRelationRepository(db).ListTopologyCandidates(9, 201)
	if err != nil {
		t.Fatalf("list topology candidates: %v", err)
	}
	if len(items) != 201 {
		t.Fatalf("items = %d, want 201", len(items))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
