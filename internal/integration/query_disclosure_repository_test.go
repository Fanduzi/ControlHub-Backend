//go:build integration

// Package integration provides Testcontainers tests for disclosure policy repository operations.
// input: testing, database/sql, internal/model, internal/repository/mysql, internal/service
// output: TestQueryDisclosureRepository_* integration tests
// pos: Proves canonical five-part CRUD, conflict/not-found/idempotent semantics, and utf8mb4_bin name fidelity against real MySQL
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
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

func disclosurePolicyReq(rid uint64, database, schema, object, column string, mode model.ResultDisclosureMode) model.ResultDisclosurePolicyUpsertRequest {
	return model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: rid,
		DatabaseName:     database,
		SchemaName:       schema,
		ObjectName:       object,
		ColumnName:       column,
		Mode:             mode,
	}
}

// TestQueryDisclosureRepository_CRUDLifecycle proves the full product-safe
// lifecycle: insert → get → list → update → delete, on the legacy empty-schema
// (MySQL/TiDB) identity. WHY: the product API relies on these five primitives
// behaving exactly this way, and the empty-schema segment must keep working
// end to end.
func TestQueryDisclosureRepository_CRUDLifecycle(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 8800000001

	// 1. Insert a policy (legacy '' schema).
	req := disclosurePolicyReq(rid, "orders_db", "", "customers", "email", model.ResultDisclosureMaskedNoCopy)
	id, err := repo.Insert(ctx, req)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id == 0 {
		t.Fatal("insert must return a non-zero ID")
	}

	// 2. Get by exact scope — must return the inserted row with schema=''.
	got, err := repo.GetByScope(ctx, rid, "orders_db", "", "customers", "email")
	if err != nil {
		t.Fatalf("get by scope: %v", err)
	}
	if got.ID != id || got.Mode != model.ResultDisclosureMaskedNoCopy || got.SchemaName != "" {
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

	// 4. Update mode via the same five-part key.
	updateReq := disclosurePolicyReq(rid, "orders_db", "", "customers", "email", model.ResultDisclosureRawCopyAllowed)
	if err := repo.Update(ctx, updateReq); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = repo.GetByScope(ctx, rid, "orders_db", "", "customers", "email")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Mode != model.ResultDisclosureRawCopyAllowed {
		t.Fatalf("after update: mode = %q, want raw_copy_allowed", got.Mode)
	}
	// The database_name segment must still be the stored value, not emptied.
	if got.DatabaseName != "orders_db" {
		t.Fatalf("after update: database_name = %q, want %q", got.DatabaseName, "orders_db")
	}

	// 5. Delete — must succeed and leave no rows.
	if err := repo.Delete(ctx, rid, "orders_db", "", "customers", "email"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetByScope(ctx, rid, "orders_db", "", "customers", "email"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("after delete: err = %v, want sql.ErrNoRows", err)
	}
}

// TestQueryDisclosureRepository_DuplicateInsertRejected proves that inserting a
// policy with a scope that already exists returns the disclosure conflict
// sentinel (UNIQUE constraint on the canonical five-part key). WHY: the API
// layer must answer 409 for a duplicate scope, and the sentinel is the seam
// that makes that possible.
func TestQueryDisclosureRepository_DuplicateInsertRejected(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 8800000002

	req := disclosurePolicyReq(rid, "sales_db", "app", "orders", "id", model.ResultDisclosureMaskedNoCopy)
	if _, err := repo.Insert(ctx, req); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Second insert with the same five-part scope must map MySQL 1062 to the
	// conflict sentinel.
	if _, err := repo.Insert(ctx, req); !errors.Is(err, service.ErrQueryDisclosurePolicyConflict) {
		t.Fatalf("duplicate insert err = %v, want service.ErrQueryDisclosurePolicyConflict", err)
	}
}

// TestQueryDisclosureRepository_GetByScope_NotFound proves that GetByScope
// returns sql.ErrNoRows when no matching policy exists (the fail-closed default
// is "blocked"). WHY: callers must distinguish "no policy" from errors.
func TestQueryDisclosureRepository_GetByScope_NotFound(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()

	_, err := repo.GetByScope(ctx, 8800000099, "nonexistent_db", "", "nonexistent_table", "nonexistent_col")
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

	req := disclosurePolicyReq(8800000099, "nonexistent_db", "", "nonexistent_table", "nonexistent_col", model.ResultDisclosureRawCopyAllowed)
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
	if err := repo.Delete(ctx, 8800000099, "nonexistent_db", "", "nonexistent_table", "nonexistent_col"); err != nil {
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
// ListByTarget returns policies ordered by the canonical key segments
// database_name, schema_name, object_name, column_name. WHY: the frontend
// renders policies in a predictable order and schema rows must interleave
// deterministically with empty-schema ones.
func TestQueryDisclosureRepository_ListByTarget_MultipleColumns(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 8800000003

	// Insert out of canonical order ('' sorts before any non-empty schema under
	// utf8mb4_bin, so a_db '' precedes a_db 'analytics').
	for _, req := range []model.ResultDisclosurePolicyUpsertRequest{
		disclosurePolicyReq(rid, "z_db", "", "z_table", "z_col", model.ResultDisclosureMaskedNoCopy),
		disclosurePolicyReq(rid, "a_db", "analytics", "a_table", "a_col", model.ResultDisclosureMaskedNoCopy),
		disclosurePolicyReq(rid, "a_db", "", "a_table", "a_col", model.ResultDisclosureRawCopyAllowed),
		disclosurePolicyReq(rid, "a_db", "", "b_table", "a_col", model.ResultDisclosureMaskedNoCopy),
	} {
		if _, err := repo.Insert(ctx, req); err != nil {
			t.Fatalf("insert %+v: %v", req, err)
		}
	}

	items, err := repo.ListByTarget(ctx, rid)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 4 {
		t.Fatalf("list: got %d items, want 4", len(items))
	}
	// Verify ordering: a_db.''.a_table < a_db.''.b_table < a_db.analytics.a_table < z_db.''.z_table
	type seg struct{ db, schema, object string }
	var got []seg
	for _, it := range items {
		got = append(got, seg{it.DatabaseName, it.SchemaName, it.ObjectName})
	}
	want := []seg{
		{"a_db", "", "a_table"},
		{"a_db", "", "b_table"},
		{"a_db", "analytics", "a_table"},
		{"z_db", "", "z_table"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ordering[%d] = %+v, want %+v (full list %+v)", i, got[i], want[i], got)
		}
	}
}

// --- T9-A: canonical five-part scope proofs against real MySQL ---

// TestQueryDisclosureRepository_SchemaScopeIsolation proves (acceptance A/B)
// that under one target and database, app.orders.id and analytics.orders.id
// coexist as independent policies: each exact key hits its own row, updates
// and deletes on one schema never touch the sibling, and a missing schema
// never borrows another schema's row.
func TestQueryDisclosureRepository_SchemaScopeIsolation(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 8800000010

	appReq := disclosurePolicyReq(rid, "sales_db", "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)
	anReq := disclosurePolicyReq(rid, "sales_db", "analytics", "orders", "id", model.ResultDisclosureMaskedNoCopy)
	appID, err := repo.Insert(ctx, appReq)
	if err != nil {
		t.Fatalf("insert app scope: %v", err)
	}
	anID, err := repo.Insert(ctx, anReq)
	if err != nil {
		t.Fatalf("insert analytics scope: %v", err)
	}
	if appID == anID {
		t.Fatal("distinct five-part scopes must produce distinct rows")
	}

	// Exact keys resolve independently.
	app, err := repo.GetByScope(ctx, rid, "sales_db", "app", "orders", "id")
	if err != nil || app.Mode != model.ResultDisclosureRawCopyAllowed || app.ID != appID {
		t.Fatalf("get app scope: %+v err=%v", app, err)
	}
	an, err := repo.GetByScope(ctx, rid, "sales_db", "analytics", "orders", "id")
	if err != nil || an.Mode != model.ResultDisclosureMaskedNoCopy || an.ID != anID {
		t.Fatalf("get analytics scope: %+v err=%v", an, err)
	}

	// No cross-schema fallback: neither '' nor an absent schema borrows rows.
	for _, schema := range []string{"", "public", "reporting"} {
		if _, err := repo.GetByScope(ctx, rid, "sales_db", schema, "orders", "id"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("get schema %q: err = %v, want sql.ErrNoRows (no fallback)", schema, err)
		}
	}

	// Updating app must not affect analytics.
	upd := disclosurePolicyReq(rid, "sales_db", "app", "orders", "id", model.ResultDisclosureMaskedNoCopy)
	if err := repo.Update(ctx, upd); err != nil {
		t.Fatalf("update app scope: %v", err)
	}
	an, err = repo.GetByScope(ctx, rid, "sales_db", "analytics", "orders", "id")
	if err != nil || an.Mode != model.ResultDisclosureMaskedNoCopy {
		t.Fatalf("analytics after app update: %+v err=%v (must be untouched)", an, err)
	}

	// Deleting app must not affect analytics.
	if err := repo.Delete(ctx, rid, "sales_db", "app", "orders", "id"); err != nil {
		t.Fatalf("delete app scope: %v", err)
	}
	if _, err := repo.GetByScope(ctx, rid, "sales_db", "app", "orders", "id"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("app scope after delete: err = %v, want sql.ErrNoRows", err)
	}
	if _, err := repo.GetByScope(ctx, rid, "sales_db", "analytics", "orders", "id"); err != nil {
		t.Fatalf("analytics scope after app delete: err = %v, want row intact", err)
	}
}

// TestQueryDisclosureRepository_FivePartKeyIsolation proves (acceptance C) that
// identical name segments under a different database or a different target are
// independent keys, and that the legacy empty schema and a named schema never
// collide.
func TestQueryDisclosureRepository_FivePartKeyIsolation(t *testing.T) {
	db, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const ridA, ridB uint64 = 8800000011, 8800000012

	// Same names, different schema — both persist under one target+database.
	for _, req := range []model.ResultDisclosurePolicyUpsertRequest{
		disclosurePolicyReq(ridA, "sales_db", "", "orders", "id", model.ResultDisclosureRawCopyAllowed),
		disclosurePolicyReq(ridA, "sales_db", "app", "orders", "id", model.ResultDisclosureMaskedNoCopy),
		// Same schema/object/column under a different database.
		disclosurePolicyReq(ridA, "other_db", "app", "orders", "id", model.ResultDisclosureMaskedNoCopy),
		// Same everything under a different target.
		disclosurePolicyReq(ridB, "sales_db", "app", "orders", "id", model.ResultDisclosureMaskedNoCopy),
	} {
		if _, err := repo.Insert(ctx, req); err != nil {
			t.Fatalf("insert %+v: %v", req, err)
		}
	}

	// '' and 'app' under the same database are distinct rows.
	if _, err := repo.GetByScope(ctx, ridA, "sales_db", "", "orders", "id"); err != nil {
		t.Fatalf("'' schema must be an independent key: %v", err)
	}
	if _, err := repo.GetByScope(ctx, ridA, "sales_db", "app", "orders", "id"); err != nil {
		t.Fatalf("'app' schema must be an independent key: %v", err)
	}
	// A database without a matching row is fail-closed, not borrowed.
	if _, err := repo.GetByScope(ctx, ridA, "third_db", "app", "orders", "id"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("absent database scope: err = %v, want sql.ErrNoRows", err)
	}
	if disclosureRowCount(t, db, ridA) != 3 || disclosureRowCount(t, db, ridB) != 1 {
		t.Fatalf("row counts = (%d, %d), want (3, 1)", disclosureRowCount(t, db, ridA), disclosureRowCount(t, db, ridB))
	}
}

// TestQueryDisclosureRepository_CanonicalNameFidelity proves (acceptance E)
// that canonical names survive request → insert → lookup byte-identically:
// case differences are distinct rows (utf8mb4_bin), Unicode and characters
// only legal inside quoted PostgreSQL identifiers are stored verbatim, and no
// layer lowercases, trims, or rewrites the value.
func TestQueryDisclosureRepository_CanonicalNameFidelity(t *testing.T) {
	_, repo := newDisclosureRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 8800000013

	cases := []model.ResultDisclosurePolicyUpsertRequest{
		// Case-sensitive siblings — utf8mb4_bin keeps them distinct.
		disclosurePolicyReq(rid, "SalesDB", "App", "Orders", "ID", model.ResultDisclosureRawCopyAllowed),
		disclosurePolicyReq(rid, "salesdb", "app", "orders", "id", model.ResultDisclosureMaskedNoCopy),
		// Unicode canonical names.
		disclosurePolicyReq(rid, "销售库", "数据层", "订单表", "金额列", model.ResultDisclosureMaskedNoCopy),
		// Characters only legal inside a quoted PostgreSQL identifier, stored
		// as the canonical name (quote characters are part of the stored value
		// only when they are part of the name — here a literal quote and a
		// space exercise that).
		disclosurePolicyReq(rid, `db"name`, `sch ema`, `obj".name`, `col name`, model.ResultDisclosureMaskedNoCopy),
	}
	for _, req := range cases {
		id, err := repo.Insert(ctx, req)
		if err != nil {
			t.Fatalf("insert %+v: %v", req, err)
		}
		got, err := repo.GetByScope(ctx, rid, req.DatabaseName, req.SchemaName, req.ObjectName, req.ColumnName)
		if err != nil {
			t.Fatalf("get %+v: %v", req, err)
		}
		if got.ID != id || got.DatabaseName != req.DatabaseName || got.SchemaName != req.SchemaName ||
			got.ObjectName != req.ObjectName || got.ColumnName != req.ColumnName || got.Mode != req.Mode {
			t.Fatalf("fidelity: stored %+v, want %+v", got, req)
		}
	}

	// Same name differing only by case must miss under the binary collation.
	if _, err := repo.GetByScope(ctx, rid, "SALESDB", "app", "orders", "id"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("uppercased database must not match: err = %v, want sql.ErrNoRows", err)
	}
	if _, err := repo.GetByScope(ctx, rid, "salesdb", "APP", "orders", "id"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("uppercased schema must not match: err = %v, want sql.ErrNoRows", err)
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
