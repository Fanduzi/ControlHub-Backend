// Package mysql tests batched list identity and profile-summary reads.
// input: context, testing, sqlmock, and resource models
// output: ListResources and GetResourcesByIDs issue one IN query per table
// pos: Guards list/topology neighbor reads from per-row identity and profile round-trips
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/fan/controlhub/internal/model"
)

func TestListResourcesLoadsIdentityAndProfileSummariesInBatches(t *testing.T) {
	// A two-host page used to run aliases + external IDs + profile QueryRow
	// per resource. Inventory list is the hot path; page size goes to 500.
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`select count\(\*\) from resources r where r.archived_at is null`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	createdAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	listRows := sqlmock.NewRows([]string{
		"id", "resource_type", "resource_subtype", "name", "display_name",
		"environment_id", "owner_id", "lifecycle_status", "health_status",
		"origin", "labels", "created_at", "updated_at",
		"archived_at", "archived_by", "archive_reason", "cluster_id",
	}).
		AddRow(7, "host", "vm", "host-a", "Host A", 1, 1, "running", "healthy", "manual", "{}", createdAt, createdAt, nil, nil, nil, nil).
		AddRow(8, "host", "vm", "host-b", "Host B", 1, 1, "running", "healthy", "manual", "{}", createdAt, createdAt, nil, nil, nil, nil)
	mock.ExpectQuery(`(?s)select r\.id, r\.resource_type.*from resources r where r\.archived_at is null order by r\.name limit \? offset \?`).
		WithArgs(20, 0).
		WillReturnRows(listRows)
	mock.ExpectQuery(`select resource_id, alias from resource_aliases where resource_id in \(\?, \?\) order by resource_id, alias`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "alias"}).AddRow(7, "alias-a"))
	mock.ExpectQuery(`select resource_id, external_system, external_value from resource_external_identifiers where resource_id in \(\?, \?\) order by resource_id, external_system, external_value`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "external_system", "external_value"}).AddRow(7, "legacy", "ext-a"))
	mock.ExpectQuery(`select resource_id, hostname, ip_address from resource_profiles_host where resource_id in \(\?, \?\)`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "hostname", "ip_address"}).AddRow(7, "host-a.internal", "10.0.0.7"))
	mock.ExpectQuery(`select resource_id, health_status, observed_at, observer from resource_health_observations where resource_id in \(\?, \?\)`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "health_status", "observed_at", "observer"}))
	mock.ExpectQuery(`(?s)from collector_ci_scan_states s`).
		WithArgs(uint64(7), uint64(8), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "machine_principal_id", "name", "missing_since"}))

	items, total, err := NewResourceRepository(db).ListResources(context.Background(), model.ResourceListQuery{
		Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("items = %d total = %d, want 2/2", len(items), total)
	}
	if len(items[0].Aliases) != 1 || items[0].Aliases[0] != "alias-a" || items[0].ExternalID != "ext-a" {
		t.Fatalf("identity = aliases %#v externalId %q", items[0].Aliases, items[0].ExternalID)
	}
	if items[0].ProfileSummary == nil || items[0].ProfileSummary.Hostname != "host-a.internal" || items[0].ProfileSummary.IP != "10.0.0.7" {
		t.Fatalf("profile summary = %+v", items[0].ProfileSummary)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetResourcesByIDsLoadsRowsInOneQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	createdAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "resource_type", "resource_subtype", "name", "display_name",
		"environment_id", "owner_id", "lifecycle_status", "health_status",
		"origin", "labels", "created_at", "updated_at",
		"archived_at", "archived_by", "archive_reason", "cluster_id",
	}).
		AddRow(7, "host", "vm", "host-a", "Host A", 1, 1, "running", "healthy", "manual", "{}", createdAt, createdAt, nil, nil, nil, nil).
		AddRow(8, "host", "vm", "host-b", "Host B", 1, 1, "running", "healthy", "manual", "{}", createdAt, createdAt, nil, nil, nil, nil)
	mock.ExpectQuery(`(?s)select r\.id, r\.resource_type.*from resources r where r\.id in \(\?, \?\)`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(rows)
	mock.ExpectQuery(`select resource_id, alias from resource_aliases where resource_id in \(\?, \?\) order by resource_id, alias`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "alias"}))
	mock.ExpectQuery(`select resource_id, external_system, external_value from resource_external_identifiers where resource_id in \(\?, \?\) order by resource_id, external_system, external_value`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "external_system", "external_value"}))
	mock.ExpectQuery(`select resource_id, hostname, ip_address from resource_profiles_host where resource_id in \(\?, \?\)`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "hostname", "ip_address"}))
	mock.ExpectQuery(`select resource_id, health_status, observed_at, observer from resource_health_observations where resource_id in \(\?, \?\)`).
		WithArgs(uint64(7), uint64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "health_status", "observed_at", "observer"}))
	mock.ExpectQuery(`(?s)from collector_ci_scan_states s`).
		WithArgs(uint64(7), uint64(8), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "machine_principal_id", "name", "missing_since"}))

	got, err := NewResourceRepository(db).GetResourcesByIDs([]uint64{7, 8})
	if err != nil {
		t.Fatalf("GetResourcesByIDs: %v", err)
	}
	if len(got) != 2 || got[7] == nil || got[8] == nil {
		t.Fatalf("got %d resources, want 7 and 8", len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
