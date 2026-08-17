// Package service provides tests for QueryDisclosureService.
// input: testing, internal/model, fakeDisclosureReader, fakeDisclosureWriter, fakeSchemaInspector, fakeTargetRepo
// output: TestPreflight*, TestPreflightRelatedRecords*, TestApply*, TestUpdatePolicy*, TestCreatePolicy* functions
// pos: Verifies fail-closed preflight/apply behavior and policy CRUD sentinel mapping
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// fakeDisclosureReader implements QueryDisclosureReader with an in-memory
// policy map keyed by "database.object.column".
type fakeDisclosureReader struct {
	policies map[string]model.ResultDisclosurePolicy
}

func (f *fakeDisclosureReader) ListByTarget(_ context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error) {
	var out []model.ResultDisclosurePolicy
	for _, p := range f.policies {
		if p.TargetResourceID == targetResourceID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeDisclosureReader) GetByScope(_ context.Context, _ uint64, database, object, column string) (model.ResultDisclosurePolicy, error) {
	key := database + "." + object + "." + column
	p, ok := f.policies[key]
	if !ok {
		return model.ResultDisclosurePolicy{}, sql.ErrNoRows
	}
	return p, nil
}

func disclosureScopeKey(database, object, column string) string {
	return database + "." + object + "." + column
}

// fakeDisclosureWriter implements QueryDisclosureWriter (no-op for tests).
type fakeDisclosureWriter struct {
	insertErr error
	updateErr error
}

func (f *fakeDisclosureWriter) Insert(_ context.Context, _ model.ResultDisclosurePolicyUpsertRequest) (uint64, error) {
	return 1, f.insertErr
}
func (f *fakeDisclosureWriter) Update(_ context.Context, _ model.ResultDisclosurePolicyUpsertRequest) error {
	return f.updateErr
}
func (f *fakeDisclosureWriter) Delete(_ context.Context, _ uint64, _, _, _ string) error {
	return nil
}

// testDSN is a valid MySQL DSN for tests that call mysql.ParseDSN.
const testDSN = "user:pass@tcp(localhost:3306)/testdb?parseTime=true"

func newTestDisclosureService(
	reader QueryDisclosureReader,
	inspector QuerySchemaInspector,
	targets QueryTargetRepository,
) *QueryDisclosureService {
	return NewQueryDisclosureService(reader, &fakeDisclosureWriter{}, inspector, targets)
}

// --- tests ---

// TestPreflight_CanceledInspectorRead_KeepsCancellationUnwrapable proves the
// preflight read error stays %w-wrapped inside the blocked wrap (Issue #35), so
// the execution service can classify a canceled disclosure read as
// failed/query_canceled instead of a genuine policy rejection. This test fails
// if the inner wrap degrades to %v, which is exactly the property the
// execution service's canceled-outcome test depends on.
func TestPreflight_CanceledInspectorRead_KeepsCancellationUnwrapable(t *testing.T) {
	t.Parallel()

	inspector := &fakeSchemaInspector{err: context.Canceled}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(&fakeDisclosureReader{}, inspector, targets)

	_, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT id FROM users",
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want the blocked wrap around the canceled read")
	}
	if !errors.Is(err, ErrQueryDisclosureBlocked) {
		t.Fatalf("Preflight() error = %v, want ErrQueryDisclosureBlocked wrap", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preflight read must stay unwrapable through the blocked wrap (Issue #35); errors.Is(context.Canceled) = false on %v", err)
	}
}

func TestPreflight_AllRawCopyAllowed(t *testing.T) {
	t.Parallel()

	// Given: a table with two columns, both with raw_copy_allowed policies.
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{
			Name: "users",
			Kind: "table",
			Columns: []ColumnSummary{
				{Name: "id", Position: 1, Type: "int", Nullable: "NO", Key: "PRI"},
				{Name: "name", Position: 2, Type: "varchar(255)", Nullable: "YES", Key: ""},
			},
		},
	}
	reader := &fakeDisclosureReader{
		policies: map[string]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "users", "id"):   {TargetResourceID: 1, Mode: model.ResultDisclosureRawCopyAllowed},
			disclosureScopeKey("testdb", "users", "name"): {TargetResourceID: 1, Mode: model.ResultDisclosureRawCopyAllowed},
		},
	}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: Preflight resolves disclosure for SELECT id, name FROM users.
	plan, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT id, name FROM users",
	})

	// Then: all columns are raw_copy_allowed with copyAllowed=true.
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if len(plan.Columns) != 2 {
		t.Fatalf("Preflight() returned %d columns, want 2", len(plan.Columns))
	}
	for _, cd := range plan.Columns {
		if cd.Mode != model.ResultDisclosureRawCopyAllowed {
			t.Errorf("column %s: mode = %q, want %q", cd.Provenance.OutputName, cd.Mode, model.ResultDisclosureRawCopyAllowed)
		}
		if !cd.CopyAllowed {
			t.Errorf("column %s: copyAllowed = false, want true", cd.Provenance.OutputName)
		}
	}
}

func TestPreflight_MaskedNoCopy(t *testing.T) {
	t.Parallel()

	// Given: a table with one column that has masked_no_copy policy.
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{
			Name: "users",
			Kind: "table",
			Columns: []ColumnSummary{
				{Name: "ssn", Position: 1, Type: "varchar(11)", Nullable: "YES", Key: ""},
			},
		},
	}
	reader := &fakeDisclosureReader{
		policies: map[string]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "users", "ssn"): {TargetResourceID: 1, Mode: model.ResultDisclosureMaskedNoCopy},
		},
	}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: Preflight resolves disclosure for SELECT ssn FROM users.
	plan, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT ssn FROM users",
	})

	// Then: the column is masked_no_copy with copyAllowed=false.
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if len(plan.Columns) != 1 {
		t.Fatalf("Preflight() returned %d columns, want 1", len(plan.Columns))
	}
	if plan.Columns[0].Mode != model.ResultDisclosureMaskedNoCopy {
		t.Errorf("mode = %q, want %q", plan.Columns[0].Mode, model.ResultDisclosureMaskedNoCopy)
	}
	if plan.Columns[0].CopyAllowed {
		t.Error("copyAllowed = true, want false")
	}
}

func TestPreflight_MissingPolicyBlocks(t *testing.T) {
	t.Parallel()

	// Given: a table with a column that has no disclosure policy.
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{
			Name: "users",
			Kind: "table",
			Columns: []ColumnSummary{
				{Name: "id", Position: 1, Type: "int", Nullable: "NO", Key: "PRI"},
			},
		},
	}
	reader := &fakeDisclosureReader{policies: map[string]model.ResultDisclosurePolicy{}}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: Preflight resolves disclosure for a column with no policy.
	_, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT id FROM users",
	})

	// Then: ErrQueryDisclosureBlocked is returned.
	if err == nil {
		t.Fatal("Preflight() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("Preflight() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestPreflight_MixedRawAndMasked(t *testing.T) {
	t.Parallel()

	// Given: a table with one raw and one masked column.
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{
			Name: "users",
			Kind: "table",
			Columns: []ColumnSummary{
				{Name: "id", Position: 1, Type: "int", Nullable: "NO", Key: "PRI"},
				{Name: "email", Position: 2, Type: "varchar(255)", Nullable: "YES", Key: ""},
			},
		},
	}
	reader := &fakeDisclosureReader{
		policies: map[string]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "users", "id"):    {TargetResourceID: 1, Mode: model.ResultDisclosureRawCopyAllowed},
			disclosureScopeKey("testdb", "users", "email"): {TargetResourceID: 1, Mode: model.ResultDisclosureMaskedNoCopy},
		},
	}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: Preflight resolves disclosure for SELECT id, email FROM users.
	plan, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT id, email FROM users",
	})

	// Then: id is raw, email is masked.
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	if len(plan.Columns) != 2 {
		t.Fatalf("Preflight() returned %d columns, want 2", len(plan.Columns))
	}

	idCol := plan.Columns[0]
	emailCol := plan.Columns[1]

	if idCol.Mode != model.ResultDisclosureRawCopyAllowed || !idCol.CopyAllowed {
		t.Errorf("id: mode=%q copyAllowed=%v, want raw_copy_allowed/true", idCol.Mode, idCol.CopyAllowed)
	}
	if emailCol.Mode != model.ResultDisclosureMaskedNoCopy || emailCol.CopyAllowed {
		t.Errorf("email: mode=%q copyAllowed=%v, want masked_no_copy/false", emailCol.Mode, emailCol.CopyAllowed)
	}
}

func TestPreflight_UnsupportedSQLBlocked(t *testing.T) {
	t.Parallel()

	// Given: a disclosure service with no special configuration.
	inspector := &fakeSchemaInspector{detail: nil}
	reader := &fakeDisclosureReader{policies: map[string]model.ResultDisclosurePolicy{}}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: Preflight is called with a multi-table JOIN (unsupported projection).
	_, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT a.id FROM users a JOIN orders b ON a.id = b.user_id",
	})

	// Then: the projection resolution fails and returns blocked.
	if err == nil {
		t.Fatal("Preflight() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("Preflight() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestPreflightRelatedRecords_ValidFKMetadata(t *testing.T) {
	t.Parallel()

	// Given: a referenced table with two columns, both with policies.
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{
			Name: "orders",
			Kind: "table",
			Columns: []ColumnSummary{
				{Name: "id", Position: 1, Type: "int", Nullable: "NO", Key: "PRI"},
				{Name: "total", Position: 2, Type: "decimal(10,2)", Nullable: "YES", Key: ""},
			},
		},
	}
	reader := &fakeDisclosureReader{
		policies: map[string]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "orders", "id"):    {TargetResourceID: 1, Mode: model.ResultDisclosureRawCopyAllowed},
			disclosureScopeKey("testdb", "orders", "total"): {TargetResourceID: 1, Mode: model.ResultDisclosureMaskedNoCopy},
		},
	}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: PreflightRelatedRecords resolves disclosure for orders table.
	plan, err := svc.PreflightRelatedRecords(context.Background(), testDSN, 1, "testdb", "orders")

	// Then: id is raw, total is masked.
	if err != nil {
		t.Fatalf("PreflightRelatedRecords() error = %v, want nil", err)
	}
	if len(plan.Columns) != 2 {
		t.Fatalf("PreflightRelatedRecords() returned %d columns, want 2", len(plan.Columns))
	}

	idCol := plan.Columns[0]
	totalCol := plan.Columns[1]

	if idCol.Mode != model.ResultDisclosureRawCopyAllowed || !idCol.CopyAllowed {
		t.Errorf("id: mode=%q copyAllowed=%v, want raw_copy_allowed/true", idCol.Mode, idCol.CopyAllowed)
	}
	if totalCol.Mode != model.ResultDisclosureMaskedNoCopy || totalCol.CopyAllowed {
		t.Errorf("total: mode=%q copyAllowed=%v, want masked_no_copy/false", totalCol.Mode, totalCol.CopyAllowed)
	}
}

func TestPreflightRelatedRecords_MissingPolicyBlocks(t *testing.T) {
	t.Parallel()

	// Given: a referenced table with no policies configured.
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{
			Name: "orders",
			Kind: "table",
			Columns: []ColumnSummary{
				{Name: "id", Position: 1, Type: "int", Nullable: "NO", Key: "PRI"},
			},
		},
	}
	reader := &fakeDisclosureReader{policies: map[string]model.ResultDisclosurePolicy{}}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: PreflightRelatedRecords resolves disclosure for a table with no policy.
	_, err := svc.PreflightRelatedRecords(context.Background(), testDSN, 1, "testdb", "orders")

	// Then: ErrQueryDisclosureBlocked is returned.
	if err == nil {
		t.Fatal("PreflightRelatedRecords() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("PreflightRelatedRecords() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestApply_TransformsRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		plan      DisclosurePlan
		columns   []model.QueryResultColumn
		rows      [][]any
		wantRows  [][]any
		wantModes []model.ResultDisclosureMode
		wantCopy  []bool
	}{
		{
			name: "all raw_copy_allowed preserves values",
			plan: DisclosurePlan{Columns: []ColumnDisclosure{
				{Mode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true},
				{Mode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true},
			}},
			columns: []model.QueryResultColumn{
				{Name: "id", DatabaseType: "int"},
				{Name: "name", DatabaseType: "varchar"},
			},
			rows:      [][]any{{1, "Alice"}, {2, "Bob"}},
			wantRows:  [][]any{{1, "Alice"}, {2, "Bob"}},
			wantModes: []model.ResultDisclosureMode{model.ResultDisclosureRawCopyAllowed, model.ResultDisclosureRawCopyAllowed},
			wantCopy:  []bool{true, true},
		},
		{
			name: "masked_no_copy replaces non-null values",
			plan: DisclosurePlan{Columns: []ColumnDisclosure{
				{Mode: model.ResultDisclosureMaskedNoCopy, CopyAllowed: false},
			}},
			columns: []model.QueryResultColumn{
				{Name: "ssn", DatabaseType: "varchar"},
			},
			rows:      [][]any{{"123-45-6789"}, {nil}},
			wantRows:  [][]any{{maskedReplacement}, {nil}},
			wantModes: []model.ResultDisclosureMode{model.ResultDisclosureMaskedNoCopy},
			wantCopy:  []bool{false},
		},
		{
			name: "mixed raw and masked per column",
			plan: DisclosurePlan{Columns: []ColumnDisclosure{
				{Mode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true},
				{Mode: model.ResultDisclosureMaskedNoCopy, CopyAllowed: false},
			}},
			columns: []model.QueryResultColumn{
				{Name: "id", DatabaseType: "int"},
				{Name: "email", DatabaseType: "varchar"},
			},
			rows:      [][]any{{1, "a@b.com"}, {2, nil}},
			wantRows:  [][]any{{1, maskedReplacement}, {2, nil}},
			wantModes: []model.ResultDisclosureMode{model.ResultDisclosureRawCopyAllowed, model.ResultDisclosureMaskedNoCopy},
			wantCopy:  []bool{true, false},
		},
		{
			name:      "empty rows preserved",
			plan:      DisclosurePlan{Columns: []ColumnDisclosure{{Mode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true}}},
			columns:   []model.QueryResultColumn{{Name: "id", DatabaseType: "int"}},
			rows:      [][]any{},
			wantRows:  [][]any{},
			wantModes: []model.ResultDisclosureMode{model.ResultDisclosureRawCopyAllowed},
			wantCopy:  []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Given: a disclosure plan and result columns/rows.
			svc := &QueryDisclosureService{}

			// When: Apply transforms the result set.
			gotColumns, gotRows, applyErr := svc.Apply(tt.plan, tt.columns, tt.rows)
			if applyErr != nil {
				t.Fatalf("Apply() returned unexpected error: %v", applyErr)
			}

			// Then: column metadata is updated and row values are transformed.
			for i, want := range tt.wantModes {
				if gotColumns[i].DisplayMode != want {
					t.Errorf("column[%d].DisplayMode = %q, want %q", i, gotColumns[i].DisplayMode, want)
				}
			}
			for i, want := range tt.wantCopy {
				if gotColumns[i].CopyAllowed != want {
					t.Errorf("column[%d].CopyAllowed = %v, want %v", i, gotColumns[i].CopyAllowed, want)
				}
			}
			if len(gotRows) != len(tt.wantRows) {
				t.Fatalf("got %d rows, want %d", len(gotRows), len(tt.wantRows))
			}
			for r, row := range gotRows {
				for c, val := range row {
					if val != tt.wantRows[r][c] {
						t.Errorf("row[%d][%d] = %#v, want %#v", r, c, val, tt.wantRows[r][c])
					}
				}
			}
		})
	}
}

func TestApply_EmptyPlanBlocks(t *testing.T) {
	t.Parallel()

	// Given: an empty disclosure plan (no columns resolved).
	svc := &QueryDisclosureService{}
	columns := []model.QueryResultColumn{{Name: "id", DatabaseType: "int"}}
	rows := [][]any{{42}}

	// When: Apply is called with an empty plan.
	_, _, applyErr := svc.Apply(DisclosurePlan{}, columns, rows)

	// Then: empty plan must be rejected (fail-closed). Raw rows must not pass through.
	if applyErr == nil {
		t.Fatal("Apply() error = nil, want ErrQueryDisclosureBlocked for empty plan")
	}
	if !isDisclosureBlocked(applyErr) {
		t.Errorf("Apply() error = %v, want wrapped ErrQueryDisclosureBlocked", applyErr)
	}
}

func TestPreflight_BlockedStoredModeBlocks(t *testing.T) {
	t.Parallel()

	// Given: a table with a column that has a blocked stored mode.
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{
			Name: "users",
			Kind: "table",
			Columns: []ColumnSummary{
				{Name: "id", Position: 1, Type: "int", Nullable: "NO", Key: "PRI"},
			},
		},
	}
	reader := &fakeDisclosureReader{
		policies: map[string]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "users", "id"): {TargetResourceID: 1, Mode: model.ResultDisclosureBlocked},
		},
	}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: Preflight resolves disclosure for a column with blocked stored mode.
	_, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT id FROM users",
	})

	// Then: ErrQueryDisclosureBlocked is returned.
	if err == nil {
		t.Fatal("Preflight() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("Preflight() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestPreflight_UnknownStoredModeBlocks(t *testing.T) {
	t.Parallel()

	// Given: a table with a column that has an unknown stored mode.
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{
			Name: "users",
			Kind: "table",
			Columns: []ColumnSummary{
				{Name: "id", Position: 1, Type: "int", Nullable: "NO", Key: "PRI"},
			},
		},
	}
	reader := &fakeDisclosureReader{
		policies: map[string]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "users", "id"): {TargetResourceID: 1, Mode: "unknown_mode"},
		},
	}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: Preflight resolves disclosure for a column with unknown stored mode.
	_, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT id FROM users",
	})

	// Then: ErrQueryDisclosureBlocked is returned.
	if err == nil {
		t.Fatal("Preflight() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("Preflight() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestApply_RejectsRawModeWithCopyAllowedFalse(t *testing.T) {
	t.Parallel()

	// Given: a plan with raw mode but copyAllowed=false.
	svc := &QueryDisclosureService{}
	plan := DisclosurePlan{Columns: []ColumnDisclosure{
		{Mode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: false},
	}}
	columns := []model.QueryResultColumn{{Name: "id", DatabaseType: "int"}}
	rows := [][]any{{42}}

	// When: Apply is called with invalid mode/copy pair.
	_, _, err := svc.Apply(plan, columns, rows)

	// Then: ErrQueryDisclosureBlocked is returned.
	if err == nil {
		t.Fatal("Apply() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("Apply() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestApply_RejectsMaskedModeWithCopyAllowedTrue(t *testing.T) {
	t.Parallel()

	// Given: a plan with masked mode but copyAllowed=true.
	svc := &QueryDisclosureService{}
	plan := DisclosurePlan{Columns: []ColumnDisclosure{
		{Mode: model.ResultDisclosureMaskedNoCopy, CopyAllowed: true},
	}}
	columns := []model.QueryResultColumn{{Name: "ssn", DatabaseType: "varchar"}}
	rows := [][]any{{"123-45-6789"}}

	// When: Apply is called with invalid mode/copy pair.
	_, _, err := svc.Apply(plan, columns, rows)

	// Then: ErrQueryDisclosureBlocked is returned.
	if err == nil {
		t.Fatal("Apply() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("Apply() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestApply_RejectsBlockedMode(t *testing.T) {
	t.Parallel()

	// Given: a plan with blocked mode.
	svc := &QueryDisclosureService{}
	plan := DisclosurePlan{Columns: []ColumnDisclosure{
		{Mode: model.ResultDisclosureBlocked, CopyAllowed: false},
	}}
	columns := []model.QueryResultColumn{{Name: "id", DatabaseType: "int"}}
	rows := [][]any{{42}}

	// When: Apply is called with blocked mode.
	_, _, err := svc.Apply(plan, columns, rows)

	// Then: ErrQueryDisclosureBlocked is returned.
	if err == nil {
		t.Fatal("Apply() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("Apply() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestApply_RejectsUnknownMode(t *testing.T) {
	t.Parallel()

	// Given: a plan with unknown mode.
	svc := &QueryDisclosureService{}
	plan := DisclosurePlan{Columns: []ColumnDisclosure{
		{Mode: "unknown_mode", CopyAllowed: false},
	}}
	columns := []model.QueryResultColumn{{Name: "id", DatabaseType: "int"}}
	rows := [][]any{{42}}

	// When: Apply is called with unknown mode.
	_, _, err := svc.Apply(plan, columns, rows)

	// Then: ErrQueryDisclosureBlocked is returned.
	if err == nil {
		t.Fatal("Apply() error = nil, want ErrQueryDisclosureBlocked")
	}
	if !isDisclosureBlocked(err) {
		t.Errorf("Apply() error = %v, want wrapped ErrQueryDisclosureBlocked", err)
	}
}

func TestPreflight_NonSelectStatementBlocks(t *testing.T) {
	t.Parallel()

	inspector := &fakeSchemaInspector{detail: nil}
	reader := &fakeDisclosureReader{policies: map[string]model.ResultDisclosurePolicy{}}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	tests := []struct {
		name string
		sql  string
	}{
		{"SHOW_TABLES", "SHOW TABLES"},
		{"DESCRIBE", "DESCRIBE users"},
		{"SHOW_DATABASES", "SHOW DATABASES"},
		{"SHOW_COLUMNS", "SHOW COLUMNS FROM users"},
		{"SHOW_STATUS", "SHOW STATUS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
				OriginalStatement: tt.sql,
			})
			if err == nil {
				t.Fatalf("Preflight(%q) error = nil, want ErrQueryDisclosureBlocked", tt.sql)
			}
			if !isDisclosureBlocked(err) {
				t.Errorf("Preflight(%q) error = %v, want wrapped ErrQueryDisclosureBlocked", tt.sql, err)
			}
		})
	}
}

func TestPreflight_SelectLiteralStillAllowed(t *testing.T) {
	t.Parallel()

	inspector := &fakeSchemaInspector{detail: nil}
	reader := &fakeDisclosureReader{policies: map[string]model.ResultDisclosurePolicy{}}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// SELECT 1 is a pure literal no-FROM query; it must still pass preflight
	// because resolveLiteralOnlyProjection produces columns with raw_copy_allowed.
	plan, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT 1",
	})
	if err != nil {
		t.Fatalf("Preflight(SELECT 1) error = %v, want nil", err)
	}
	if len(plan.Columns) != 1 {
		t.Fatalf("Preflight(SELECT 1) columns = %d, want 1", len(plan.Columns))
	}
	if plan.Columns[0].Mode != model.ResultDisclosureRawCopyAllowed {
		t.Errorf("Preflight(SELECT 1) mode = %q, want raw_copy_allowed", plan.Columns[0].Mode)
	}
}

func isDisclosureBlocked(err error) bool {
	return errors.Is(err, ErrQueryDisclosureBlocked)
}

// TestUpdatePolicy_NotFoundMapsToSentinel proves that updating a scope with no
// existing policy maps sql.ErrNoRows to ErrQueryDisclosurePolicyNotFound.
// WHY: the HTTP layer must answer 404 for a missing policy, not 500.
func TestUpdatePolicy_NotFoundMapsToSentinel(t *testing.T) {
	t.Parallel()

	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := NewQueryDisclosureService(&fakeDisclosureReader{}, &fakeDisclosureWriter{updateErr: sql.ErrNoRows}, nil, targets)
	req := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 1,
		DatabaseName:     "orders_db",
		ObjectName:       "customers",
		ColumnName:       "email",
		Mode:             model.ResultDisclosureMaskedNoCopy,
	}
	err := svc.UpdatePolicy(context.Background(), req)
	if !errors.Is(err, ErrQueryDisclosurePolicyNotFound) {
		t.Fatalf("UpdatePolicy error = %v, want ErrQueryDisclosurePolicyNotFound", err)
	}
}

// TestCreatePolicy_ConflictSentinelPassesThrough proves CreatePolicy forwards
// the repository conflict sentinel unchanged. WHY: the handler relies on the
// seam translating a duplicate scope into HTTP 409.
func TestCreatePolicy_ConflictSentinelPassesThrough(t *testing.T) {
	t.Parallel()

	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := NewQueryDisclosureService(&fakeDisclosureReader{}, &fakeDisclosureWriter{insertErr: ErrQueryDisclosurePolicyConflict}, nil, targets)
	req := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 1,
		DatabaseName:     "orders_db",
		ObjectName:       "customers",
		ColumnName:       "email",
		Mode:             model.ResultDisclosureMaskedNoCopy,
	}
	if _, err := svc.CreatePolicy(context.Background(), req); !errors.Is(err, ErrQueryDisclosurePolicyConflict) {
		t.Fatalf("CreatePolicy error = %v, want ErrQueryDisclosurePolicyConflict", err)
	}
}
