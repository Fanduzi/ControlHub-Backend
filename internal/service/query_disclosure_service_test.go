// Package service provides tests for QueryDisclosureService.
// input: testing, internal/model, fakeDisclosureReader, fakeDisclosureWriter, fakeSchemaInspector, fakeTargetRepo
// output: TestPreflight*, TestPreflightRelatedRecords*, TestApply*, TestUpdatePolicy*, TestCreatePolicy*, TestDeletePolicy* functions
// pos: Verifies fail-closed preflight/apply behavior, policy CRUD sentinel mapping, and the T9-A engine-conditional schema gate
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
	policies map[disclosureScopeKeyT]model.ResultDisclosurePolicy
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

func (f *fakeDisclosureReader) GetByScope(_ context.Context, _ uint64, database, schema, object, column string) (model.ResultDisclosurePolicy, error) {
	p, ok := f.policies[disclosureScopeKey(database, schema, object, column)]
	if !ok {
		return model.ResultDisclosurePolicy{}, sql.ErrNoRows
	}
	return p, nil
}

// disclosureScopeKey is a struct (not a joined string) so same-named objects in
// different schemas can never collide through a separator character.
type disclosureScopeKeyT struct {
	database, schema, object, column string
}

func disclosureScopeKey(database, schema, object, column string) disclosureScopeKeyT {
	return disclosureScopeKeyT{database, schema, object, column}
}

// fakeDisclosureWriter implements QueryDisclosureWriter. It records the exact
// scope arguments the service passed so tests can prove the canonical
// five-part key travels intact.
type fakeDisclosureWriter struct {
	insertErr    error
	updateErr    error
	insertCalled bool
	insertReq    model.ResultDisclosurePolicyUpsertRequest
	updateCalled bool
	updateReq    model.ResultDisclosurePolicyUpsertRequest
	deleteCalled bool
	deleteTarget uint64
	deleteScope  disclosureScopeKeyT
}

func (f *fakeDisclosureWriter) Insert(_ context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error) {
	f.insertCalled = true
	f.insertReq = req
	return 1, f.insertErr
}
func (f *fakeDisclosureWriter) Update(_ context.Context, req model.ResultDisclosurePolicyUpsertRequest) error {
	f.updateCalled = true
	f.updateReq = req
	return f.updateErr
}
func (f *fakeDisclosureWriter) Delete(_ context.Context, targetResourceID uint64, database, schema, object, column string) error {
	f.deleteCalled = true
	f.deleteTarget = targetResourceID
	f.deleteScope = disclosureScopeKey(database, schema, object, column)
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

// TestPreflight_InspectorReadFailure_ReturnsBackendSentinel proves disclosure
// machinery failures (inspector/read infra) are NOT policy rejections: Preflight
// returns the backend sentinel with the cause %w-wrapped (Issue #35 AC 4), so
// the execution service records them as terminal failed/canceled/timeout
// evidence instead of rejected. This test fails if the inspector error is
// folded into the blocked wrap or the cause loses its %w.
func TestPreflight_InspectorReadFailure_ReturnsBackendSentinel(t *testing.T) {
	t.Parallel()

	inspector := &fakeSchemaInspector{err: errors.New("inspector unreachable")}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(&fakeDisclosureReader{}, inspector, targets)

	_, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT id FROM users",
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want the backend-failure wrap around the inspector error")
	}
	if errors.Is(err, ErrQueryDisclosureBlocked) {
		t.Fatalf("Preflight() inspector failure must not be a policy rejection: %v", err)
	}
	if !errors.Is(err, ErrQueryDisclosureBackendFailure) {
		t.Fatalf("Preflight() error = %v, want ErrQueryDisclosureBackendFailure", err)
	}
}

// TestPreflight_CanceledInspectorRead_KeepsCancellationUnwrapable proves a
// canceled disclosure read stays errors.Is-unwrapable through the backend
// sentinel wrap, so the execution service classifies it failed/query_canceled
// instead of a policy rejection (Issue #35).
func TestPreflight_CanceledInspectorRead_KeepsCancellationUnwrapable(t *testing.T) {
	t.Parallel()

	inspector := &fakeSchemaInspector{err: context.Canceled}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(&fakeDisclosureReader{}, inspector, targets)

	_, err := svc.Preflight(context.Background(), testDSN, 1, GuardedQuery{
		OriginalStatement: "SELECT id FROM users",
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want the backend-failure wrap around the canceled read")
	}
	if !errors.Is(err, ErrQueryDisclosureBackendFailure) {
		t.Fatalf("Preflight() error = %v, want ErrQueryDisclosureBackendFailure", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preflight read must stay unwrapable through the backend wrap (Issue #35); errors.Is(context.Canceled) = false on %v", err)
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
		policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "", "users", "id"):   {TargetResourceID: 1, Mode: model.ResultDisclosureRawCopyAllowed},
			disclosureScopeKey("testdb", "", "users", "name"): {TargetResourceID: 1, Mode: model.ResultDisclosureRawCopyAllowed},
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
		policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "", "users", "ssn"): {TargetResourceID: 1, Mode: model.ResultDisclosureMaskedNoCopy},
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
	reader := &fakeDisclosureReader{policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{}}
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
		policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "", "users", "id"):    {TargetResourceID: 1, Mode: model.ResultDisclosureRawCopyAllowed},
			disclosureScopeKey("testdb", "", "users", "email"): {TargetResourceID: 1, Mode: model.ResultDisclosureMaskedNoCopy},
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
	reader := &fakeDisclosureReader{policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{}}
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
		policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "", "orders", "id"):    {TargetResourceID: 1, Mode: model.ResultDisclosureRawCopyAllowed},
			disclosureScopeKey("testdb", "", "orders", "total"): {TargetResourceID: 1, Mode: model.ResultDisclosureMaskedNoCopy},
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
	reader := &fakeDisclosureReader{policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{}}
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

// TestPreflightRelatedRecords_InspectorFailure_ReturnsBackendSentinel proves
// related-record disclosure machinery failures (inspector read infrastructure)
// are NOT policy rejections: PreflightRelatedRecords wraps them in
// ErrQueryDisclosureBackendFailure with the cause %w-preserved (Issue #35 AC 4
// / Issue #36), so the navigation service classifies them as fixed failed
// disclosure evidence exactly like core execution — never as a rejection.
func TestPreflightRelatedRecords_InspectorFailure_ReturnsBackendSentinel(t *testing.T) {
	t.Parallel()

	// Given: the schema inspector fails while reading the referenced table.
	inspectorErr := errors.New("inspector connection refused")
	inspector := &fakeSchemaInspector{
		err: inspectorErr,
	}
	reader := &fakeDisclosureReader{policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{}}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{{ResourceID: 1}}}
	svc := newTestDisclosureService(reader, inspector, targets)

	// When: PreflightRelatedRecords tries to resolve disclosure for the table.
	_, err := svc.PreflightRelatedRecords(context.Background(), testDSN, 1, "testdb", "orders")

	// Then: the backend sentinel is returned with the raw cause preserved for
	// classification, and it is NOT a policy rejection.
	if err == nil {
		t.Fatal("PreflightRelatedRecords() error = nil, want ErrQueryDisclosureBackendFailure")
	}
	if !errors.Is(err, ErrQueryDisclosureBackendFailure) {
		t.Errorf("PreflightRelatedRecords() error = %v, want wrapped ErrQueryDisclosureBackendFailure", err)
	}
	if !errors.Is(err, inspectorErr) {
		t.Errorf("PreflightRelatedRecords() must wrap-preserve the inspector cause for classification: %v", err)
	}
	if isDisclosureBlocked(err) {
		t.Errorf("PreflightRelatedRecords() error = %v, must not be a policy rejection", err)
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
		policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "", "users", "id"): {TargetResourceID: 1, Mode: model.ResultDisclosureBlocked},
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
		policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{
			disclosureScopeKey("testdb", "", "users", "id"): {TargetResourceID: 1, Mode: "unknown_mode"},
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
	reader := &fakeDisclosureReader{policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{}}
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
	reader := &fakeDisclosureReader{policies: map[disclosureScopeKeyT]model.ResultDisclosurePolicy{}}
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

// --- T9-A: canonical five-part scope and the engine-conditional schema gate ---

func pgDisclosureTarget(id uint64) model.QueryTarget {
	return model.QueryTarget{ResourceID: id, ConnectionContext: model.QueryTargetConnectionContext{Engine: "postgresql"}}
}

func mysqlDisclosureTarget(id uint64) model.QueryTarget {
	return model.QueryTarget{ResourceID: id, ConnectionContext: model.QueryTargetConnectionContext{Engine: "mysql"}}
}

// TestCreatePolicy_PostgresRequiresExplicitSchema proves a PostgreSQL policy
// scope can never be written with a missing schema: the service returns a
// controlled validation error and the writer is never reached. WHY: silently
// resolving schema to "public" or the connection default_schema would create
// a policy under an identity the operator never stated (G10 fail-closed).
func TestCreatePolicy_PostgresRequiresExplicitSchema(t *testing.T) {
	t.Parallel()

	writer := &fakeDisclosureWriter{}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{pgDisclosureTarget(7)}}
	svc := NewQueryDisclosureService(&fakeDisclosureReader{}, writer, nil, targets)
	req := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 7,
		DatabaseName:     "sales_db",
		ObjectName:       "orders",
		ColumnName:       "email",
		Mode:             model.ResultDisclosureRawCopyAllowed,
	}
	_, err := svc.CreatePolicy(context.Background(), req)
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("CreatePolicy error = %v, want ErrQueryValidationFailed", err)
	}
	if writer.insertCalled {
		t.Fatal("writer must not be reached when schema is missing for postgresql")
	}
}

// TestCreatePolicy_PostgresSchemaPreservesCanonicalNames proves a PostgreSQL
// scope keeps wide canonical names — case, Unicode, and characters only legal
// inside quoted identifiers — verbatim through service validation into the
// writer. WHY: the policy identity is a canonical name, not a SQL fragment; the
// service must not lowercase, trim, or ASCII-filter it.
func TestCreatePolicy_PostgresSchemaPreservesCanonicalNames(t *testing.T) {
	t.Parallel()

	writer := &fakeDisclosureWriter{}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{pgDisclosureTarget(7)}}
	svc := NewQueryDisclosureService(&fakeDisclosureReader{}, writer, nil, targets)
	req := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 7,
		DatabaseName:     "销售库",
		SchemaName:       "MixedCase.Schema",
		ObjectName:       `"Quoted Table"`,
		ColumnName:       "café",
		Mode:             model.ResultDisclosureMaskedNoCopy,
	}
	id, err := svc.CreatePolicy(context.Background(), req)
	if err != nil {
		t.Fatalf("CreatePolicy error = %v, want nil for wide postgresql names", err)
	}
	if id != 1 || !writer.insertCalled {
		t.Fatalf("writer insert result = %d called=%v", id, writer.insertCalled)
	}
	if writer.insertReq != req {
		t.Fatalf("writer req = %+v, want verbatim %+v", writer.insertReq, req)
	}
}

// TestCreatePolicy_MySQLRejectsNonEmptySchema proves a schema segment on a
// MySQL-scoped policy is an invalid combination, not a quiet migration of
// meaning. WHY: MySQL policy identity keeps the legacy empty schema.
func TestCreatePolicy_MySQLRejectsNonEmptySchema(t *testing.T) {
	t.Parallel()

	writer := &fakeDisclosureWriter{}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{mysqlDisclosureTarget(3)}}
	svc := NewQueryDisclosureService(&fakeDisclosureReader{}, writer, nil, targets)
	req := model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 3,
		DatabaseName:     "orders_db",
		SchemaName:       "app",
		ObjectName:       "orders",
		ColumnName:       "email",
		Mode:             model.ResultDisclosureRawCopyAllowed,
	}
	_, err := svc.CreatePolicy(context.Background(), req)
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("CreatePolicy error = %v, want ErrQueryValidationFailed", err)
	}
	if writer.insertCalled {
		t.Fatal("writer must not be reached for a mysql scope with a schema")
	}
}

// TestCreatePolicy_MySQLKeepsStrictIdentifierRule proves the ASCII identifier
// contract MySQL/TiDB scopes always had still applies through the real service
// path. WHY: relaxing the model-level rule for PostgreSQL must not widen the
// MySQL contract by accident.
func TestCreatePolicy_MySQLKeepsStrictIdentifierRule(t *testing.T) {
	t.Parallel()

	for _, database := range []string{"orders; DROP TABLE", "my db", "täble"} {
		writer := &fakeDisclosureWriter{}
		targets := &fakeTargetRepo{targets: []model.QueryTarget{mysqlDisclosureTarget(3)}}
		svc := NewQueryDisclosureService(&fakeDisclosureReader{}, writer, nil, targets)
		req := model.ResultDisclosurePolicyUpsertRequest{
			TargetResourceID: 3,
			DatabaseName:     database,
			ObjectName:       "orders",
			ColumnName:       "email",
			Mode:             model.ResultDisclosureRawCopyAllowed,
		}
		if _, err := svc.CreatePolicy(context.Background(), req); !errors.Is(err, ErrQueryValidationFailed) {
			t.Fatalf("CreatePolicy(%q) error = %v, want ErrQueryValidationFailed", database, err)
		}
		if writer.insertCalled {
			t.Fatalf("writer must not be reached for invalid mysql database %q", database)
		}
	}
}

// TestDeletePolicy_FivePartScopeReachesWriter proves DeletePolicy binds all
// five canonical segments verbatim. WHY: deleting (t,db,app,obj,col) must
// never fall through to the empty-schema or a different schema's row.
func TestDeletePolicy_FivePartScopeReachesWriter(t *testing.T) {
	t.Parallel()

	writer := &fakeDisclosureWriter{}
	targets := &fakeTargetRepo{targets: []model.QueryTarget{pgDisclosureTarget(7)}}
	svc := NewQueryDisclosureService(&fakeDisclosureReader{}, writer, nil, targets)
	if err := svc.DeletePolicy(context.Background(), 7, "sales_db", "analytics", "orders", "email"); err != nil {
		t.Fatalf("DeletePolicy error = %v, want nil", err)
	}
	want := disclosureScopeKey("sales_db", "analytics", "orders", "email")
	if !writer.deleteCalled || writer.deleteTarget != 7 || writer.deleteScope != want {
		t.Fatalf("delete scope = (%d, %+v) called=%v, want (7, %+v)", writer.deleteTarget, writer.deleteScope, writer.deleteCalled, want)
	}
}

// TestDeletePolicy_SchemaGateMatchesWrites proves delete applies the same
// engine-conditional contract as create/update: postgresql requires an
// explicit schema, mysql requires none.
func TestDeletePolicy_SchemaGateMatchesWrites(t *testing.T) {
	t.Parallel()

	pgWriter := &fakeDisclosureWriter{}
	pgTargets := &fakeTargetRepo{targets: []model.QueryTarget{pgDisclosureTarget(7)}}
	pgSvc := NewQueryDisclosureService(&fakeDisclosureReader{}, pgWriter, nil, pgTargets)
	if err := pgSvc.DeletePolicy(context.Background(), 7, "sales_db", "", "orders", "email"); !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("postgresql delete without schema error = %v, want ErrQueryValidationFailed", err)
	}
	if pgWriter.deleteCalled {
		t.Fatal("writer must not be reached for a schema-less postgresql delete")
	}

	myWriter := &fakeDisclosureWriter{}
	myTargets := &fakeTargetRepo{targets: []model.QueryTarget{mysqlDisclosureTarget(3)}}
	mySvc := NewQueryDisclosureService(&fakeDisclosureReader{}, myWriter, nil, myTargets)
	if err := mySvc.DeletePolicy(context.Background(), 3, "orders_db", "app", "orders", "email"); !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("mysql delete with schema error = %v, want ErrQueryValidationFailed", err)
	}
	if myWriter.deleteCalled {
		t.Fatal("writer must not be reached for a schema'd mysql delete")
	}
}
