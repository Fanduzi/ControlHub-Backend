//go:build integration

// Package integration provides Testcontainers-backed tests for the Phase 38Q
// disclosure policy repository operations: CRUD lifecycle, duplicate insert
// rejection, not-found cases, and idempotent delete.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
)

// newDisclosureRepoTestDB returns a clean DB connection plus a disclosure
// repository. query_result_disclosure_policies has no FK on target_resource_id,
// so repository tests use synthetic resource ids without provisioning real
// resources.
func newDisclosureRepoTestDB(t *testing.T) (*sql.DB, *mysql.MySQLQueryDisclosureRepository) {
	t.Helper()
	db := setupTestDB(t)
	return db, mysql.NewQueryDisclosureRepository(db)
}

// TestQueryDisclosureRepository_CRUDLifecycle proves the full product-safe
// lifecycle: insert → get → list → update → delete. WHY: the product API
// relies on these five primitives behaving exactly this way.
func TestQueryDisclosureRepository_CRUDLifecycle(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 8800000001

	// 1. Insert a policy.
	req := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: rid,
		DatabaseName:     "orders_db",
		ObjectName:       "customers",
		ColumnName:       "email",
		Mode:             model.ResultDisclosureMaskedNoCopy,
	}
	id, err := repo.Insert(ctx, req)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id == 0 {
		t.Fatal("insert must return a non-zero ID")
	}

	// 2. Get by scope — must return the inserted row.
	got, err := repo.GetByScope(ctx, rid, "orders_db", "customers", "email")
	if err != nil {
		t.Fatalf("get by scope: %v", err)
	}
	if got.ID != id || got.Mode != model.ResultDisclosureMaskedNoCopy {
		t.Fatalf("get by scope: got %+v", got)
	}

	// 3. List by target — must return exactly one row.
	items, err := repo.ListByTarget(ctx, rid)
	if err != nil {
		t.Fatalf("list by target: %v", err)
	}
	if len(items) != 1 || items[0].ID != id {
		t.Fatalf("list by target: got %d items, want 1 with id %d", len(items), id)
	}

	// 4. Update mode.
	updateReq := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: rid,
		DatabaseName:     "orders_db",
		ObjectName:       "customers",
		ColumnName:       "email",
		Mode:             model.ResultDisclosureRawCopyAllowed,
	}
	if err := repo.Update(ctx, updateReq); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = repo.GetByScope(ctx, rid, "orders_db", "customers", "email")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Mode != model.ResultDisclosureRawCopyAllowed {
		t.Fatalf("after update: mode = %q, want raw_copy_allowed", got.Mode)
	}

	// 5. Delete — must succeed and leave no rows.
	if err := repo.Delete(ctx, rid, "orders_db", "customers", "email"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetByScope(ctx, rid, "orders_db", "customers", "email"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("after delete: err = %v, want sql.ErrNoRows", err)
	}
}

// TestQueryDisclosureRepository_DuplicateInsertRejected proves that inserting a
// policy with a scope that already exists returns an error (UNIQUE constraint on
// target_resource_id, database_name, object_name, column_name). WHY: the scope
// uniqueness invariant prevents conflicting policies for the same column.
func TestQueryDisclosureRepository_DuplicateInsertRejected(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 8800000002

	req := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: rid,
		DatabaseName:     "orders_db",
		ObjectName:       "customers",
		ColumnName:       "email",
		Mode:             model.ResultDisclosureMaskedNoCopy,
	}
	if _, err := repo.Insert(ctx, req); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Second insert with the same scope must fail.
	if _, err := repo.Insert(ctx, req); err == nil {
		t.Fatal("duplicate insert must be rejected")
	}
}

// TestQueryDisclosureRepository_GetByScope_NotFound proves that GetByScope
// returns sql.ErrNoRows when no matching policy exists (the fail-closed default
// is "blocked"). WHY: callers must distinguish "no policy" from errors.
func TestQueryDisclosureRepository_GetByScope_NotFound(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()

	_, err := repo.GetByScope(ctx, 8800000099, "nonexistent_db", "nonexistent_table", "nonexistent_col")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("not found: err = %v, want sql.ErrNoRows", err)
	}
}

// TestQueryDisclosureRepository_Update_NotFound proves that Update returns
// sql.ErrNoRows when no matching policy exists. WHY: callers must know the
// policy they tried to update does not exist.
func TestQueryDisclosureRepository_Update_NotFound(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()

	req := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 8800000099,
		DatabaseName:     "nonexistent_db",
		ObjectName:       "nonexistent_table",
		ColumnName:       "nonexistent_col",
		Mode:             model.ResultDisclosureRawCopyAllowed,
	}
	if err := repo.Update(ctx, req); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("update not found: err = %v, want sql.ErrNoRows", err)
	}
}

// TestQueryDisclosureRepository_Delete_Idempotent proves that deleting a scope
// that has no row is not an error. WHY: the product delete path must be safe to
// call regardless of whether a policy exists.
func TestQueryDisclosureRepository_Delete_Idempotent(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()

	// Delete a scope that was never inserted — must succeed.
	if err := repo.Delete(ctx, 8800000099, "nonexistent_db", "nonexistent_table", "nonexistent_col"); err != nil {
		t.Fatalf("idempotent delete must not error: %v", err)
	}
}

// TestQueryDisclosureRepository_ListByTarget_Empty proves that ListByTarget
// returns an empty slice (not nil) when no policies exist for the target. WHY:
// callers iterate the result without nil-checking.
func TestQueryDisclosureRepository_ListByTarget_Empty(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()

	items, err := repo.ListByTarget(ctx, 8800000099)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if items == nil {
		t.Fatal("list empty: items must be non-nil empty slice")
	}
	if len(items) != 0 {
		t.Fatalf("list empty: got %d items, want 0", len(items))
	}
}

// TestQueryDisclosureRepository_ListByTarget_MultipleColumns proves that
// ListByTarget returns policies ordered by database_name, object_name,
// column_name. WHY: the frontend renders policies in a predictable order.
func TestQueryDisclosureRepository_ListByTarget_MultipleColumns(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 8800000003

	// Insert out of alphabetical order.
	for _, req := range []model.ResultDisclosurePolicyUpsertRequest{
		{TargetResourceID: rid, DatabaseName: "z_db", ObjectName: "z_table", ColumnName: "z_col", Mode: model.ResultDisclosureMaskedNoCopy},
		{TargetResourceID: rid, DatabaseName: "a_db", ObjectName: "a_table", ColumnName: "a_col", Mode: model.ResultDisclosureRawCopyAllowed},
		{TargetResourceID: rid, DatabaseName: "a_db", ObjectName: "b_table", ColumnName: "a_col", Mode: model.ResultDisclosureMaskedNoCopy},
	} {
		if _, err := repo.Insert(ctx, req); err != nil {
			t.Fatalf("insert %+v: %v", req, err)
		}
	}

	items, err := repo.ListByTarget(ctx, rid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("list: got %d items, want 3", len(items))
	}
	// Verify ordering: a_db.a_table < a_db.b_table < z_db.z_table
	if items[0].ObjectName != "a_table" || items[1].ObjectName != "b_table" || items[2].ObjectName != "z_table" {
		t.Fatalf("ordering: got [%s.%s, %s.%s, %s.%s]",
			items[0].DatabaseName, items[0].ObjectName,
			items[1].DatabaseName, items[1].ObjectName,
			items[2].DatabaseName, items[2].ObjectName)
	}
}

// disclosureRowCount returns the number of disclosure policy rows for a target.
func disclosureRowCount(t *testing.T, db *sql.DB, targetResourceID uint64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`select count(*) from query_result_disclosure_policies where target_resource_id = ?`, targetResourceID).Scan(&n); err != nil {
		t.Fatalf("count disclosure rows: %v", err)
	}
	return n
}
