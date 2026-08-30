// Package mysql provides bounded topology relation repository tests.
// input: testing, time, sqlmock, and internal/model
// output: deterministic bounded topology relation and candidate read regression coverage
// pos: Verifies topology-only queries apply caller-owned row budgets before scanning
// note: if this file changes, update this header and module README.md.
package mysql

import (
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
