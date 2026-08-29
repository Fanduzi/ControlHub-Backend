// Package mysql tests atomic bulk resource confirmation.
// input: context, errors, regexp, testing, time, sqlmock, and service contracts
// output: bulk transaction, externalId persistence, locked validation, and read-only preview coverage
// pos: Focused repository bulk mutation behavior, including normal update validation parity
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/fan/controlhub/internal/service"
)

func TestConfirmBulkResourceMutationTransaction(t *testing.T) {
	updatedAt := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	request := service.BulkResourceMutationRequest{
		Targets:    []service.BulkResourceMutationTarget{{ResourceID: 7, ExpectedVersion: updatedAt.Format(time.RFC3339Nano)}},
		FieldPatch: map[string]any{"displayName": "after"},
		Labels:     service.LabelOperations{Add: map[string]string{"team": "platform"}},
	}
	preview, err := service.PreviewBulkResourceMutation(request, []service.ResourceMutationSnapshot{{
		ID: 7, Version: updatedAt.Format(time.RFC3339Nano),
		Fields: map[string]any{
			"name": "resource-7", "resourceSubtype": "api", "displayName": "before",
			"environmentId": uint64(1), "ownerId": uint64(1), "lifecycleStatus": "running",
			"healthStatus": "unknown", "externalId": "",
		},
		Labels: map[string]string{},
	}})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		auditErr  error
		committed bool
	}{
		{name: "commit", committed: true},
		{name: "audit failure rolls back", auditErr: errors.New("audit failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectBegin()
			expectBulkResourceForUpdate(mock, updatedAt)
			expectBulkHealthObservations(mock)
			expectBulkResourceForUpdate(mock, updatedAt)
			mock.ExpectExec(`(?i)update resources`).WillReturnResult(sqlmock.NewResult(0, 1))
			audit := mock.ExpectExec(`(?i)insert into audit_events`)
			if tc.auditErr != nil {
				audit.WillReturnError(tc.auditErr)
				mock.ExpectRollback()
			} else {
				audit.WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			}

			_, err = NewResourceRepository(db).ConfirmBulkResourceMutation(context.Background(), request, preview.Fingerprint, 9)
			if tc.committed && err != nil {
				t.Fatalf("confirm: %v", err)
			}
			if !tc.committed && !errors.Is(err, tc.auditErr) {
				t.Fatalf("confirm error = %v, want %v", err, tc.auditErr)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfirmBulkResourceMutationPersistsExternalID(t *testing.T) {
	updatedAt := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	request := service.BulkResourceMutationRequest{
		Targets:    []service.BulkResourceMutationTarget{{ResourceID: 7, ExpectedVersion: updatedAt.Format(time.RFC3339Nano)}},
		FieldPatch: map[string]any{"externalId": "after"},
	}
	preview, err := service.PreviewBulkResourceMutation(request, []service.ResourceMutationSnapshot{{
		ID: 7, Version: updatedAt.Format(time.RFC3339Nano),
		Fields: map[string]any{
			"name": "resource-7", "resourceSubtype": "api", "displayName": "before",
			"environmentId": uint64(1), "ownerId": uint64(1), "lifecycleStatus": "running",
			"healthStatus": "unknown", "externalId": "before",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	expectBulkResourceForUpdate(mock, updatedAt, "before")
	expectBulkHealthObservations(mock)
	expectBulkResourceForUpdate(mock, updatedAt, "before")
	mock.ExpectExec(`(?i)update resources`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`delete from resource_external_identifiers where resource_id = ?`)).WithArgs(uint64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`insert into resource_external_identifiers (resource_id, external_system, external_value) values (?, ?, ?)`)).WithArgs(uint64(7), "legacy", "after").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`(?i)insert into audit_events`).WithArgs(uint64(9), uint64(7), "inventory.resource.bulk_updated", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if _, err := NewResourceRepository(db).ConfirmBulkResourceMutation(context.Background(), request, preview.Fingerprint, 9); err != nil {
		t.Fatalf("confirm externalId mutation: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmBulkResourceMutationRevalidatesArchivedResourceUnderLock(t *testing.T) {
	updatedAt := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	request := service.BulkResourceMutationRequest{
		Targets:    []service.BulkResourceMutationTarget{{ResourceID: 7, ExpectedVersion: updatedAt.Format(time.RFC3339Nano)}},
		FieldPatch: map[string]any{"displayName": "after"},
	}
	reviewed, err := service.PreviewBulkResourceMutation(request, []service.ResourceMutationSnapshot{{
		ID: 7, Version: updatedAt.Format(time.RFC3339Nano),
		Fields: map[string]any{
			"name": "resource-7", "resourceSubtype": "api", "displayName": "before",
			"environmentId": uint64(1), "ownerId": uint64(1), "lifecycleStatus": "running",
			"healthStatus": "healthy", "externalId": "",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	expectBulkResourceForUpdate(mock, updatedAt, updatedAt.Add(-time.Hour))
	expectBulkHealthObservations(mock)
	mock.ExpectRollback()

	preview, err := NewResourceRepository(db).ConfirmBulkResourceMutation(context.Background(), request, reviewed.Fingerprint, 9)
	if !errors.Is(err, service.ErrBulkResourceMutationConflict) || len(preview.Items) != 1 || len(preview.Items[0].Errors) == 0 {
		t.Fatalf("confirm preview = %#v, err = %v", preview, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewBulkResourceMutationReadsWithoutWrites(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	updatedAt := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	mock.ExpectQuery(`select r\.id, r\.resource_type,[\s\S]*from resources r where r\.id = \?`).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{
		"id", "resource_type", "resource_subtype", "name", "display_name", "environment_id", "owner_id", "lifecycle_status", "health_status", "origin", "labels", "created_at", "updated_at", "archived_at", "archived_by", "archive_reason", "cluster_id",
	}).AddRow(7, "domain_name", "dns", "example.com", "Example", 1, 1, "running", "healthy", "manual", `{"team":"platform"}`, updatedAt, updatedAt, nil, nil, nil, nil))
	mock.ExpectQuery(regexp.QuoteMeta(`select alias from resource_aliases where resource_id = ? order by alias`)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"alias"}))
	mock.ExpectQuery(regexp.QuoteMeta(`select external_system, external_value from resource_external_identifiers where resource_id = ? order by external_system, external_value`)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"external_system", "external_value"}))
	expectBulkHealthObservations(mock)
	mock.ExpectQuery(`select r\.id, r\.resource_type,[\s\S]*from resources r where r\.id = \?`).WithArgs(uint64(8)).WillReturnError(sql.ErrNoRows)

	preview, err := NewResourceRepository(db).PreviewBulkResourceMutation(context.Background(), service.BulkResourceMutationRequest{
		Targets: []service.BulkResourceMutationTarget{{ResourceID: 7, ExpectedVersion: updatedAt.Format(time.RFC3339Nano)}, {ResourceID: 8, ExpectedVersion: "missing"}},
		Labels:  service.LabelOperations{Add: map[string]string{"tier": "edge"}},
	})
	if err != nil || preview.Confirmable || len(preview.Items) != 2 || preview.Items[0].ResourceID != 7 || len(preview.Items[1].Errors) == 0 {
		t.Fatalf("preview = %#v, err = %v", preview, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewBulkResourceMutationUsesNormalUpdateValidation(t *testing.T) {
	updatedAt := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	archivedAt := updatedAt.Add(-time.Hour)
	for _, tc := range []struct {
		name       string
		fieldPatch map[string]any
		labels     service.LabelOperations
		archivedAt any
	}{
		{name: "archived CI", fieldPatch: map[string]any{"displayName": "after"}, archivedAt: archivedAt},
		{name: "name", fieldPatch: map[string]any{"name": "INVALID"}},
		{name: "subtype", fieldPatch: map[string]any{"resourceSubtype": "invalid"}},
		{name: "display name", fieldPatch: map[string]any{"displayName": ""}},
		{name: "lifecycle status", fieldPatch: map[string]any{"lifecycleStatus": "invalid"}},
		{name: "health status", fieldPatch: map[string]any{"healthStatus": "invalid"}},
		{name: "environment reference", fieldPatch: map[string]any{"environmentId": uint64(999)}},
		{name: "owner reference", fieldPatch: map[string]any{"ownerId": uint64(999)}},
		{name: "reserved label", labels: service.LabelOperations{Add: map[string]string{"api-token": "unsafe"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			expectBulkResourceRead(mock, updatedAt, tc.archivedAt)

			preview, err := NewResourceRepository(db).PreviewBulkResourceMutation(context.Background(), service.BulkResourceMutationRequest{
				Targets:    []service.BulkResourceMutationTarget{{ResourceID: 7, ExpectedVersion: updatedAt.Format(time.RFC3339Nano)}},
				FieldPatch: tc.fieldPatch,
				Labels:     tc.labels,
			})
			if err != nil {
				t.Fatalf("preview: %v", err)
			}
			if preview.Confirmable || len(preview.Items) != 1 || len(preview.Items[0].Errors) == 0 {
				t.Fatalf("invalid mutation was confirmable: %#v", preview)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func expectBulkResourceForUpdate(mock sqlmock.Sqlmock, updatedAt time.Time, state ...any) {
	var archivedAt any
	var externalID string
	for _, value := range state {
		switch value := value.(type) {
		case time.Time:
			archivedAt = value
		case string:
			externalID = value
		}
	}
	rows := sqlmock.NewRows([]string{
		"id", "resource_type", "resource_subtype", "name", "display_name",
		"environment_id", "owner_id", "lifecycle_status", "health_status",
		"origin", "labels", "created_at", "updated_at",
		"archived_at", "archived_by", "archive_reason",
	})
	if archivedAt != nil {
		rows.AddRow(7, "service", "api", "resource-7", "before", 1, 1, "running", nil, "manual", `{}`, updatedAt, updatedAt, archivedAt, nil, nil)
	} else {
		rows.AddRow(7, "service", "api", "resource-7", "before", 1, 1, "running", nil, "manual", `{}`, updatedAt, updatedAt, nil, nil, nil)
	}
	mock.ExpectQuery(regexp.QuoteMeta("select " + resourceColumns + " from resources where id = ? for update")).
		WithArgs(uint64(7)).
		WillReturnRows(rows)
	mock.ExpectQuery(regexp.QuoteMeta(`select alias from resource_aliases where resource_id = ? order by alias`)).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"alias"}))
	identifierRows := sqlmock.NewRows([]string{"external_system", "external_value"})
	if externalID != "" {
		identifierRows.AddRow("legacy", externalID)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`select external_system, external_value from resource_external_identifiers where resource_id = ? order by external_system, external_value`)).
		WithArgs(uint64(7)).
		WillReturnRows(identifierRows)
}

func expectBulkResourceRead(mock sqlmock.Sqlmock, updatedAt time.Time, archivedAt any) {
	mock.ExpectQuery(`select r\.id, r\.resource_type,[\s\S]*from resources r where r\.id = \?`).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{
		"id", "resource_type", "resource_subtype", "name", "display_name", "environment_id", "owner_id", "lifecycle_status", "health_status", "origin", "labels", "created_at", "updated_at", "archived_at", "archived_by", "archive_reason", "cluster_id",
	}).AddRow(7, "service", "api", "resource-7", "before", 1, 1, "running", "healthy", "manual", `{}`, updatedAt, updatedAt, archivedAt, nil, nil, nil))
	mock.ExpectQuery(regexp.QuoteMeta(`select alias from resource_aliases where resource_id = ? order by alias`)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"alias"}))
	mock.ExpectQuery(regexp.QuoteMeta(`select external_system, external_value from resource_external_identifiers where resource_id = ? order by external_system, external_value`)).WithArgs(uint64(7)).WillReturnRows(sqlmock.NewRows([]string{"external_system", "external_value"}))
	expectBulkHealthObservations(mock)
}

func expectBulkHealthObservations(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(`select resource_id, health_status, observed_at, observer
		from resource_health_observations where resource_id in (?)`)).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "health_status", "observed_at", "observer"}))
}
