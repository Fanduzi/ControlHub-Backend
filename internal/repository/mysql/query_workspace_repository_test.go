// Package mysql provides tests for query workspace persistence.
// input: context, database/sql, encoding/json, errors, testing, time, sqlmock, go-sql-driver/mysql, internal/model, internal/service
// output: missing workspace, owner isolation, exact SQL arguments, insert/update OCC, and service-parity conflict mapping tests
// pos: SQL contract coverage for the one-row-per-owner query workspace aggregate
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	gomysql "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func TestQueryWorkspaceGetMissingReturnsVersionZeroAndEmptyWorksheets(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT worksheets, version, updated_at FROM query_workspaces WHERE owner_user_id = \?`).
		WithArgs(uint64(41)).
		WillReturnError(sql.ErrNoRows)

	workspace, err := NewQueryWorkspaceRepository(db).Get(context.Background(), 41)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if workspace.OwnerUserID != 41 || workspace.Version != 0 || workspace.Worksheets == nil || len(workspace.Worksheets) != 0 {
		t.Fatalf("missing workspace = %+v, want owner 41, version 0, non-nil empty worksheets", workspace)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestQueryWorkspaceGetIsOwnerScopedAndDoesNotResolveTargets(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	updatedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	stored := []model.QueryWorkspaceWorksheet{{ID: "kept", Name: "Deleted target", TargetResourceID: 999999, Statement: "invalid sql ("}}
	raw, _ := json.Marshal(stored)
	mock.ExpectQuery(`SELECT worksheets, version, updated_at FROM query_workspaces WHERE owner_user_id = \?`).
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"worksheets", "version", "updated_at"}).AddRow(raw, 3, updatedAt))

	workspace, err := NewQueryWorkspaceRepository(db).Get(context.Background(), 7)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if workspace.OwnerUserID != 7 || workspace.Version != 3 || len(workspace.Worksheets) != 1 || workspace.Worksheets[0] != stored[0] {
		t.Fatalf("workspace = %+v, want stored target and statement unchanged", workspace)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestQueryWorkspacePutVersionZeroInsertsVersionOneWithExactArgs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	req := workspacePutRequest(0)
	raw, _ := json.Marshal(req.Worksheets)
	mock.ExpectExec(`INSERT INTO query_workspaces \(owner_user_id, worksheets, version\) VALUES \(\?, \?, 1\)`).
		WithArgs(uint64(12), raw).
		WillReturnResult(sqlmock.NewResult(0, 1))

	version, err := NewQueryWorkspaceRepository(db).Put(context.Background(), 12, req)
	if err != nil || version != 1 {
		t.Fatalf("Put() = (%d, %v), want (1, nil)", version, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestQueryWorkspacePutDuplicateInsertIsConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectExec(`INSERT INTO query_workspaces`).
		WillReturnError(&gomysql.MySQLError{Number: 1062, Message: "duplicate owner"})

	_, err = NewQueryWorkspaceRepository(db).Put(context.Background(), 12, workspacePutRequest(0))
	if !errors.Is(err, ErrQueryWorkspaceConflict) {
		t.Fatalf("Put() error = %v, want ErrQueryWorkspaceConflict", err)
	}
	if !errors.Is(err, service.ErrQueryWorkspaceConflict) {
		t.Fatalf("Put() error = %v, want service ErrQueryWorkspaceConflict parity", err)
	}
}

func TestQueryWorkspacePutOCCRaceAllowsOnlyFirstUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	req := workspacePutRequest(3)
	raw, _ := json.Marshal(req.Worksheets)
	update := `UPDATE query_workspaces SET worksheets = \?, version = version \+ 1 WHERE owner_user_id = \? AND version = \?`
	mock.ExpectExec(update).WithArgs(raw, uint64(21), uint64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(update).WithArgs(raw, uint64(21), uint64(3)).WillReturnResult(sqlmock.NewResult(0, 0))
	repo := NewQueryWorkspaceRepository(db)

	version, err := repo.Put(context.Background(), 21, req)
	if err != nil || version != 4 {
		t.Fatalf("first Put() = (%d, %v), want (4, nil)", version, err)
	}
	if _, err := repo.Put(context.Background(), 21, req); !errors.Is(err, ErrQueryWorkspaceConflict) {
		t.Fatalf("racing Put() error = %v, want ErrQueryWorkspaceConflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func workspacePutRequest(expectedVersion uint64) model.QueryWorkspacePutRequest {
	return model.QueryWorkspacePutRequest{
		ExpectedVersion: expectedVersion,
		Worksheets: []model.QueryWorkspaceWorksheet{{
			ID:               "worksheet-1",
			Name:             "Worksheet 1",
			TargetResourceID: 55,
			Statement:        "\nDELETE FROM unfinished ",
		}},
	}
}
