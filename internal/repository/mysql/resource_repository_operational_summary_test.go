// Package mysql tests fail-closed cluster operational summary attachment.
// input: context, database/sql, errors, testing, sqlmock, and resource models
// output: member-rollup query/scan/observation failures surface as errors instead of empty summaries
// pos: SQL-level regression for Issue #88 silent cluster rollup swallow
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/fan/controlhub/internal/model"
)

func TestAttachDatabaseOperationalSummariesSkipsQueryWhenNoClusters(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	items := []model.Resource{{ID: 1, ResourceType: model.ResourceTypeHost}}
	if err := NewResourceRepository(db).attachDatabaseOperationalSummaries(t.Context(), items); err != nil {
		t.Fatalf("attachDatabaseOperationalSummaries: %v", err)
	}
	if items[0].DatabaseOperationalSummary != nil {
		t.Fatalf("host summary = %+v, want none", items[0].DatabaseOperationalSummary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFetchDatabaseOperationalSummariesQueryErrorFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	queryErr := errors.New("member rollup unavailable")
	mock.ExpectQuery("child.display_name").
		WithArgs(uint64(9)).
		WillReturnError(queryErr)

	items := []model.Resource{{ID: 9, ResourceType: model.ResourceTypeDatabaseCluster}}
	err = NewResourceRepository(db).attachDatabaseOperationalSummaries(t.Context(), items)
	if err == nil {
		t.Fatal("attachDatabaseOperationalSummaries error = nil, want member rollup failure")
	}
	if !errors.Is(err, queryErr) {
		t.Fatalf("error = %v, want wrapped %v", err, queryErr)
	}
	if items[0].DatabaseOperationalSummary != nil {
		t.Fatalf("summary = %+v, want none after rollup failure", items[0].DatabaseOperationalSummary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFetchDatabaseOperationalSummariesScanErrorFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	mock.ExpectQuery("child.display_name").
		WithArgs(uint64(9)).
		WillReturnRows(sqlmock.NewRows([]string{
			"cluster_id", "id", "display_name", "resource_type", "lifecycle_status", "health_status",
		}).AddRow(9, 10, "node-01", "database_instance", "running", "healthy").RowError(0, errors.New("scan interrupted")))

	err = NewResourceRepository(db).attachDatabaseOperationalSummaries(t.Context(), []model.Resource{
		{ID: 9, ResourceType: model.ResourceTypeDatabaseCluster},
	})
	if err == nil {
		t.Fatal("attachDatabaseOperationalSummaries error = nil, want scan failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestBuildDatabaseOperationalSummaryQueryErrorFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	queryErr := errors.New("member rollup unavailable")
	mock.ExpectQuery("child.display_name").
		WithArgs(uint64(9)).
		WillReturnError(queryErr)

	summary, err := NewResourceRepository(db).buildDatabaseOperationalSummary(t.Context(), 9)
	if err == nil {
		t.Fatal("buildDatabaseOperationalSummary error = nil, want member rollup failure")
	}
	if !errors.Is(err, queryErr) {
		t.Fatalf("error = %v, want wrapped %v", err, queryErr)
	}
	if summary != nil {
		t.Fatalf("summary = %+v, want nil after rollup failure", summary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
