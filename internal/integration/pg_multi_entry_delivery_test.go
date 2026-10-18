//go:build integration

// Package integration — delivery-contract edge cases through the new
// PG-MULTI-ENTRY saved/navigation entries.
// input: disposable PG16 fixtures (orders/smallvals plus a dedicated
// inherited-row navigation fixture with planner parallelism disabled),
// real MySQL production dependencies
// output: TestPGMultiEntryDelivery_* — cell-width boundaries, masked and
// NULL flag semantics (masked-long included), empty/literal-NULL cells,
// capability gate behavior (unknown refusal with persisted-success id,
// duplicate capability, duplicate public column matrix alignment), zero
// rows, G6 page bounds, sentinel-matrix flags across pages, a dedicated
// nav_cases boundary table (8192/8193/CJK/4-byte/NULL/empty/literal-NULL
// under raw and masked_no_copy scopes), witness-free projection, and CSV
// eligibility on real final pages
// pos: stage 4 packet item 6 — the shared delivery boundary exercised
// through both keyed entries, not just the ordinary path
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// The saved entry delivers the same governed page contract: exact 8192/8193
// boundaries, multibyte cut points, masked/NULL flags — masked-long keeps
// [MASKED]+flag over a real 9000-byte source and refuses no-cap with
// persisted success — empty and literal NULL cells, zero rows including a
// LIMIT 0 constant-long page, page bounds, capability gating, and a CSV
// decision on the real final page.
func TestPGMultiEntryDelivery_Saved(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-dlv-s-"+strings.ReplaceAll(t.Name(), "/", "-"))
	for _, col := range []string{"id", "qty"} {
		insertPolicy(t, disc, targetID, "discdb", "app", "orders", col, model.ResultDisclosureRawCopyAllowed)
	}
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "note", model.ResultDisclosureMaskedNoCopy)
	for _, col := range []string{"id", "tag"} {
		insertPolicy(t, disc, targetID, "discdb", "app", "smallvals", col, model.ResultDisclosureRawCopyAllowed)
	}

	mkStmt := func(name, sql string, params []model.QuerySavedStatementParameterDefinition) model.QuerySavedStatement {
		return createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
			Name: name, Statement: sql, Scope: model.QuerySavedStatementPersonal, Parameters: params})
	}
	capable := func(key string, values map[string]json.RawMessage) model.QuerySavedStatementExecuteRequest {
		r := savedRequest(key, values)
		r.Capabilities = []string{service.CapabilityCellTruncated}
		return r
	}
	const B = service.ResultCellMaxBytes

	// Width boundaries: exact-fit cells stay whole and unflagged; the first
	// byte over cuts at the rune boundary — 8193 ASCII → 8192, CJK 8193 →
	// 8190 (2730 three-byte runes), a 4-byte rune after an 8190-byte prefix
	// cannot fit → still 8190 flagged. Empty string and literal "NULL" are
	// distinct from SQL NULL, and a validated integer parameter feeds the
	// raw long value (never an oversized request input).
	boundary := mkStmt("width boundary", `
		SELECT repeat('a', 8191) AS a8191,
		       repeat('a', 8192) AS a8192,
		       repeat('a', 8193) AS a8193,
		       repeat('中', 2731) AS zh8193,
		       '🙂' || repeat('a', 8188) AS em8192,
		       repeat('中', 2730) || '🙂' AS zh4b,
		       ''::text AS empty_cell,
		       'NULL'::text AS lit_null,
		       NULL::text AS real_null,
		       repeat('x', :n::int) AS dyn`,
		[]model.QuerySavedStatementParameterDefinition{{Name: "n", Type: model.QuerySavedStatementParameterInteger}})
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, boundary.ID,
		capable("pgmulti-dlv-s-b", map[string]json.RawMessage{"n": json.RawMessage(`9000`)}))
	if err != nil {
		t.Fatalf("boundary execute: %v", err)
	}
	if len(out.Result.Rows) != 1 {
		t.Fatalf("boundary rows = %+v", out.Result.Rows)
	}
	row, marks := out.Result.Rows[0], out.Result.CellTruncated[0]
	cell := func(i int) (string, bool, bool) {
		if i >= len(row) || i >= len(marks) {
			t.Fatalf("column %d missing (cols=%d marks=%d)", i, len(row), len(marks))
		}
		if row[i] == nil {
			return "", true, marks[i]
		}
		return *row[i], false, marks[i]
	}
	if v, isNull, tr := cell(0); isNull || len(v) != 8191 || tr {
		t.Fatalf("a8191 = %d null=%v flag=%v", len(v), isNull, tr)
	}
	if v, isNull, tr := cell(1); isNull || len(v) != B || tr {
		t.Fatalf("a8192 = %d null=%v flag=%v", len(v), isNull, tr)
	}
	if v, isNull, tr := cell(2); isNull || len(v) != B || !tr {
		t.Fatalf("a8193 = %d null=%v flag=%v, want 8192 cut flagged", len(v), isNull, tr)
	}
	if v, isNull, tr := cell(3); isNull || len(v) != 8190 || !tr || v != strings.Repeat("中", 2730) {
		t.Fatalf("zh8193 = %d null=%v flag=%v, want 8190-byte rune-boundary cut", len(v), isNull, tr)
	}
	if v, isNull, tr := cell(4); isNull || len(v) != B || tr {
		t.Fatalf("em8192 = %d null=%v flag=%v, want exact fit", len(v), isNull, tr)
	}
	if v, isNull, tr := cell(5); isNull || len(v) != 8190 || !tr {
		t.Fatalf("zh4b = %d null=%v flag=%v, want 4-byte rune dropped at 8190", len(v), isNull, tr)
	}
	if v, isNull, tr := cell(6); isNull || v != "" || tr {
		t.Fatalf("empty cell = %q null=%v flag=%v", v, isNull, tr)
	}
	if v, isNull, tr := cell(7); isNull || v != "NULL" || tr {
		t.Fatalf("literal NULL cell = %q null=%v flag=%v", v, isNull, tr)
	}
	if _, isNull, tr := cell(8); !isNull || tr {
		t.Fatalf("real NULL cell null=%v flag=%v", isNull, tr)
	}
	if v, isNull, tr := cell(9); isNull || len(v) != B || !tr {
		t.Fatalf("dyn 9000 = %d null=%v flag=%v, want 8192 flagged", len(v), isNull, tr)
	}
	// The projection carries only the declared columns — no witness leak.
	for _, c := range out.Result.Columns {
		if strings.HasPrefix(c.Name, "__chub_") {
			t.Fatalf("witness column leaked into projection: %+v", out.Result.Columns)
		}
	}

	// Masked policy (installed above) over a LONG source cell: the admin
	// update makes orders.id=1's note 9000 bytes — masking replaces the
	// value while the positional flag still records the truncation.
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	execPG(t, admin, `UPDATE app.orders SET note = repeat('m', 9000) WHERE id = 1`)
	defer func() { _ = admin.Close(context.Background()) }()
	masked := mkStmt("masked column", `SELECT id, note FROM app.orders WHERE id = 1`, nil)
	out, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, masked.ID,
		capable("pgmulti-dlv-s-m", nil))
	if err != nil {
		t.Fatalf("masked execute: %v", err)
	}
	if v, isNull, tr := func() (string, bool, bool) {
		c := out.Result.Rows[0][1]
		if c == nil {
			return "", true, out.Result.CellTruncated[0][1]
		}
		return *c, false, out.Result.CellTruncated[0][1]
	}(); isNull || v != "[MASKED]" || !tr {
		t.Fatalf("masked-long cell = %q null=%v flag=%v, want [MASKED]/true", v, isNull, tr)
	}
	if out.Result.Columns[1].CopyAllowed {
		t.Fatal("masked column became copyable")
	}
	cols, rows, matrix := csvArgs(out.Result)
	if err := service.DecideCSVExport(cols, rows, matrix); !errors.Is(err, service.ErrCSVExportNotPermitted) {
		t.Fatalf("csv on masked page = %v, want not permitted", err)
	}
	// The same masked-long page without the capability is a delivery
	// refusal — persisted success, real id, no rows.
	noCapMask, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, masked.ID,
		savedRequest("pgmulti-dlv-s-mnc", nil))
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("no-cap masked-long = %v, want upgrade required", err)
	}
	if noCapMask.ExecutionID == 0 || noCapMask.Result != nil || noCapMask.Status != model.QueryExecutionSuccess {
		t.Fatalf("no-cap masked outcome = %+v, want persisted success id with no Result", noCapMask)
	}
	// Masked NULL stays NULL unflagged — no capability needed. Row id=2's
	// note is untouched NULL.
	maskNull := mkStmt("masked null", `SELECT note FROM app.orders WHERE id = 2`, nil)
	out, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, maskNull.ID,
		savedRequest("pgmulti-dlv-s-mn", nil))
	if err != nil {
		t.Fatalf("masked null execute: %v", err)
	}
	if out.Result.Rows[0][0] != nil || out.Result.CellTruncated[0][0] {
		t.Fatalf("masked NULL = %+v flag=%v, want nil/false", out.Result.Rows[0][0], out.Result.CellTruncated[0][0])
	}

	// LIMIT 0: a constant long expression still produces a clean
	// zero-row/zero-matrix page.
	lim0 := mkStmt("limit zero", `SELECT repeat('z', 9000) AS v LIMIT 0`, nil)
	out, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, lim0.ID,
		savedRequest("pgmulti-dlv-s-l0", nil))
	if err != nil {
		t.Fatalf("limit0 execute: %v", err)
	}
	if len(out.Result.Rows) != 0 || len(out.Result.CellTruncated) != 0 {
		t.Fatalf("limit0 page = %+v, want zero rows and zero matrix", out.Result)
	}

	// Zero rows: clean page delivers without any capability.
	zero := mkStmt("zero rows", `SELECT id, tag FROM app.smallvals WHERE id > 9000 ORDER BY id`, nil)
	out, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, zero.ID,
		savedRequest("pgmulti-dlv-s-z", nil))
	if err != nil {
		t.Fatalf("zero execute: %v", err)
	}
	if len(out.Result.Rows) != 0 || len(out.Result.CellTruncated) != 0 || out.Result.Page.HasMore {
		t.Fatalf("zero page = %+v", out.Result)
	}

	// G6 page bounds: page 2 of 25 on 50 rows is the last page.
	page2 := mkStmt("page bounds", `SELECT id, tag FROM app.smallvals ORDER BY id`, nil)
	req := savedRequest("pgmulti-dlv-s-p", nil)
	req.Pagination = &model.QueryExecutePaginationRequest{Page: 2, PageSize: 25}
	out, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, page2.ID, req)
	if err != nil {
		t.Fatalf("page2 execute: %v", err)
	}
	if len(out.Result.Rows) != 25 || out.Result.Page.HasMore || *out.Result.Rows[0][0] != "26" {
		t.Fatalf("page2 = %d rows hasMore=%v first=%v", len(out.Result.Rows), out.Result.Page.HasMore, out.Result.Rows[0])
	}

	// Sentinel matrix: 25 clean rows delivered, the oversized 26th row is
	// the undelivered sentinel — row count is the page size and every flag
	// stays false.
	sentinel := mkStmt("oversized sentinel", `
		SELECT CASE WHEN s.id <= 25 THEN s.tag ELSE repeat('z', 9000) END AS v
		FROM app.smallvals s ORDER BY s.id`, nil)
	req = savedRequest("pgmulti-dlv-s-st", nil)
	req.Pagination = &model.QueryExecutePaginationRequest{Page: 1, PageSize: 25}
	out, err = svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, sentinel.ID, req)
	if err != nil {
		t.Fatalf("sentinel execute: %v", err)
	}
	if len(out.Result.Rows) != 25 || len(out.Result.CellTruncated) != 25 || !out.Result.Page.HasMore {
		t.Fatalf("sentinel page = %d rows matrix=%d hasMore=%v",
			len(out.Result.Rows), len(out.Result.CellTruncated), out.Result.Page.HasMore)
	}
	for r, m := range out.Result.CellTruncated {
		if len(m) != 1 || m[0] {
			t.Fatalf("sentinel row %d flag = %v — the undelivered sentinel polluted the matrix", r, m)
		}
	}

	// Unknown capability: a truncated page with an unrecognized token is
	// refused — only the exact capability satisfies the gate. The persisted
	// attempt was a success: the refusal carries the real execution id and
	// no rows.
	unknown := mkStmt("unknown cap", `SELECT repeat('u', 9000) AS v`, nil)
	bad := savedRequest("pgmulti-dlv-s-u", nil)
	bad.Capabilities = []string{"totally-unknown-cap"}
	noCap, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, unknown.ID, bad)
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("unknown cap = %v, want upgrade required", err)
	}
	if noCap.ExecutionID == 0 || noCap.Result != nil || noCap.Status != model.QueryExecutionSuccess {
		t.Fatalf("no-cap outcome = %+v, want persisted success id with no Result", noCap)
	}

	// The exact capability duplicated on a NEW key is still satisfied —
	// capability sets, not multiplicities, decide the gate.
	dupCap := savedRequest("pgmulti-dlv-s-dc", nil)
	dupCap.Capabilities = []string{service.CapabilityCellTruncated, service.CapabilityCellTruncated}
	dupOut, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, unknown.ID, dupCap)
	if err != nil || dupOut.Result == nil || dupOut.Result.Rows[0][0] == nil ||
		len(*dupOut.Result.Rows[0][0]) != 8192 || !dupOut.Result.CellTruncated[0][0] {
		t.Fatalf("duplicate-cap execute = %v rows %+v, want truncated delivery", err, dupOut.Result)
	}

	// Duplicate public column names keep the positional matrix aligned —
	// each cell's flag belongs to its own position, not its name.
	dupCols := mkStmt("dup columns", `SELECT repeat('q', 9000) AS x, 'short' AS x`, nil)
	dupColOut, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, dupCols.ID,
		capable("pgmulti-dlv-s-dx", nil))
	if err != nil {
		t.Fatalf("dup-column execute: %v", err)
	}
	if len(dupColOut.Result.Rows[0]) != 2 || len(dupColOut.Result.CellTruncated[0]) != 2 ||
		!dupColOut.Result.CellTruncated[0][0] || dupColOut.Result.CellTruncated[0][1] ||
		dupColOut.Result.Rows[0][1] == nil || *dupColOut.Result.Rows[0][1] != "short" {
		t.Fatalf("dup-column matrix = %+v, want positional [true,false]", dupColOut.Result)
	}

	// CSV eligibility judged on real produced pages: the masked page is
	// denied; the clean final page of smallvals is eligible.
	maskedPage, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, masked.ID,
		capable("pgmulti-dlv-s-csv1", nil))
	if err != nil {
		t.Fatalf("masked page execute: %v", err)
	}
	cols, rows, matrix = csvArgs(maskedPage.Result)
	if err := service.DecideCSVExport(cols, rows, matrix); !errors.Is(err, service.ErrCSVExportNotPermitted) {
		t.Fatalf("csv on masked saved page = %v, want not permitted", err)
	}
	clean := mkStmt("csv page", `SELECT id, tag FROM app.smallvals WHERE id <= 3 ORDER BY id`, nil)
	cleanOut, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, clean.ID,
		capable("pgmulti-dlv-s-csv2", nil))
	if err != nil {
		t.Fatalf("clean page execute: %v", err)
	}
	cols, rows, matrix = csvArgs(cleanOut.Result)
	if err := service.DecideCSVExport(cols, rows, matrix); err != nil {
		t.Fatalf("clean saved page denied csv: %v", err)
	}
}

// The navigation entry over an inheritance fixture: app.nav_parent carries
// one short note for the locator; app.nav_child (INHERITS) repeats the same
// referenced id nine times and app.nav_child_long once with an oversized
// note — eleven matches where the last is the undelivered page sentinel.
// A concrete native-text oracle pins the expected page content.
func TestPGMultiEntryDelivery_Navigation(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-dlv-n-"+strings.ReplaceAll(t.Name(), "/", "-"))
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	for _, stmt := range []string{
		`CREATE TABLE app.nav_parent (id bigint PRIMARY KEY, note text)`,
		`INSERT INTO app.nav_parent VALUES (1, 'parent-note'), (2, 'other')`,
		// Inherited rows legally repeat the parent's referenced key — the
		// source FK still validates against the parent's existing key.
		`CREATE TABLE app.nav_child (extra int) INHERITS (app.nav_parent)`,
		`INSERT INTO app.nav_child SELECT 1, 'child-' || g::text, g FROM generate_series(1, 9) g`,
		`CREATE TABLE app.nav_child_long (extra int) INHERITS (app.nav_parent)`,
		`INSERT INTO app.nav_child_long VALUES (1, repeat('z', 9000), 0)`,
		`CREATE TABLE app.navp_src (id bigint PRIMARY KEY, pid bigint,
		    CONSTRAINT fk_navp FOREIGN KEY (pid) REFERENCES app.nav_parent(id))`,
		`GRANT SELECT ON app.nav_parent TO ` + pgDiscRole,
		`GRANT SELECT ON app.nav_child TO ` + pgDiscRole,
		`GRANT SELECT ON app.nav_child_long TO ` + pgDiscRole,
		`GRANT SELECT ON app.navp_src TO ` + pgDiscRole,
		`ALTER ROLE ` + pgDiscRole + ` SET max_parallel_workers_per_gather = 0`,
	} {
		execPG(t, admin, stmt)
	}
	navPolicy(t, disc, targetID, "discdb", "app", "nav_parent", "id", "note")

	// Concrete native-text oracle for the same scope — no ORDER BY is
	// promised for navigation; the oracle establishes the real 11-row
	// scope and the long row's presence.
	var oracle []string
	rows, err := admin.Query(ctx, `SELECT note FROM app.nav_parent WHERE id = $1 LIMIT 11`, 1)
	if err != nil {
		t.Fatalf("oracle: %v", err)
	}
	for rows.Next() {
		var note string
		if err := rows.Scan(&note); err != nil {
			t.Fatalf("oracle scan: %v", err)
		}
		oracle = append(oracle, note)
	}
	rows.Close()
	if len(oracle) != 11 {
		t.Fatalf("oracle rows = %d, want 11 (parent + 9 child + 1 long child)", len(oracle))
	}

	// The oracle's last row is the oversized sentinel — this fixture's
	// concrete scan order places the long inherited row at index 10 (a
	// fixture fact, not a general navigation ordering claim).
	if len(oracle[10]) != 9000 {
		t.Fatalf("oracle[10] length = %d, want the 9000-byte long sentinel last", len(oracle[10]))
	}
	for i, note := range oracle[:10] {
		if len(note) >= 8192 {
			t.Fatalf("oracle[%d] length = %d, want clean short note in the first ten", i, len(note))
		}
	}

	src := navSource("discdb", "app", "navp_src", "fk_navp")
	req := navRequest("pgmulti-dlv-n1", src, []string{"1"})
	req.Page, req.PageSize = 1, 10
	// No capability: the page itself is clean — the over-width row is the
	// undelivered sentinel, so the gate never engages.
	out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID, req)
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if out.Relation == nil || out.Relation.ReferencedObject != "nav_parent" {
		t.Fatalf("relation = %+v, want nav_parent", out.Relation)
	}
	// Ten clean rows delivered; the eleventh match is the oversized
	// sentinel — never delivered, never flagging a delivered cell.
	if len(out.Result.Rows) != 10 || len(out.Result.CellTruncated) != 10 || !out.Result.Page.HasMore {
		t.Fatalf("nav page = %d rows matrix=%d hasMore=%v, want 10/10/true",
			len(out.Result.Rows), len(out.Result.CellTruncated), out.Result.Page.HasMore)
	}
	// The delivered MULTISET equals the oracle's first ten — duplicates
	// preserved, not a subset direction.
	delivered := map[string]int{}
	for r, row := range out.Result.Rows {
		if len(row) != 2 || row[0] == nil || *row[0] != "1" {
			t.Fatalf("nav row %d = %+v, want id=1", r, row)
		}
		for c, m := range out.Result.CellTruncated[r] {
			if m {
				t.Fatalf("delivered cell %d/%d truncated — sentinel flag leaked into the page", r, c)
			}
		}
		if row[1] == nil {
			t.Fatalf("nav row %d note NULL", r)
		}
		delivered[*row[1]]++
	}
	want := map[string]int{}
	for _, note := range oracle[:10] {
		want[note]++
	}
	if len(delivered) != len(want) {
		t.Fatalf("delivered multiset %+v ≠ oracle page %+v", delivered, want)
	}
	for note, n := range want {
		if delivered[note] != n {
			t.Fatalf("delivered count for %q = %d, want %d", note, delivered[note], n)
		}
	}
	// Multiraw projection carries only the target's public columns.
	if len(out.Result.Columns) != 2 || out.Result.Columns[0].Name != "id" || out.Result.Columns[1].Name != "note" {
		t.Fatalf("nav projection = %+v, want id+note only", out.Result.Columns)
	}
	cleanPage := out.Result // stashed for CSV eligibility below

	// Page 2 carries only the oversized sentinel row — a FRESH capable key
	// delivers it truncated and flagged; a fresh no-cap key refuses while
	// the persisted attempt stays success.
	page2cap := navRequest("pgmulti-dlv-np2c", src, []string{"1"})
	page2cap.Page, page2cap.PageSize = 2, 10
	page2cap.Capabilities = []string{service.CapabilityCellTruncated}
	p2, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID, page2cap)
	if err != nil {
		t.Fatalf("capable page-2 navigate: %v", err)
	}
	if len(p2.Result.Rows) != 1 || p2.Result.Rows[0][1] == nil ||
		len(*p2.Result.Rows[0][1]) != 8192 || !p2.Result.CellTruncated[0][1] || p2.Result.CellTruncated[0][0] {
		t.Fatalf("page-2 rows = %+v, want the 8192-byte sentinel flagged on the note cell only", p2.Result)
	}
	page2nocap := navRequest("pgmulti-dlv-np2n", src, []string{"1"})
	page2nocap.Page, page2nocap.PageSize = 2, 10
	p2n, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID, page2nocap)
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("no-cap page-2 = %v, want upgrade required", err)
	}
	if p2n.ExecutionID == 0 || p2n.Result != nil || p2n.Status != model.QueryExecutionSuccess {
		t.Fatalf("no-cap page-2 outcome = %+v, want persisted success id with no Result", p2n)
	}

	// Zero-row legal no-match: clean empty page, no capability needed.
	out, err = svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-dlv-n2", src, []string{"777"}))
	if err != nil {
		t.Fatalf("zero-row navigate: %v", err)
	}
	if len(out.Result.Rows) != 0 || len(out.Result.CellTruncated) != 0 {
		t.Fatalf("zero-row page = %+v", out.Result)
	}

	// CSV eligibility: the stashed clean first page is copyable; the
	// truncated sentinel page is denied.
	cols, rws, matrix := csvArgs(cleanPage)
	if err := service.DecideCSVExport(cols, rws, matrix); err != nil {
		t.Fatalf("clean nav page denied csv: %v", err)
	}
	cols, rws, matrix = csvArgs(p2.Result)
	if err := service.DecideCSVExport(cols, rws, matrix); !errors.Is(err, service.ErrCSVExportNotPermitted) {
		t.Fatalf("truncated sentinel page csv = %v, want not permitted", err)
	}
}

// TestPGMultiEntryDelivery_NavigationBoundaries exercises the cell-width
// gate against real PostgreSQL boundary rows through production
// navigation: 8192 clean / 8193 refused-then-capable, CJK and 4-byte-rune
// lengths, SQL NULL vs empty vs the literal text "NULL", and a second
// target-policy scope where the same over-width cell is masked without
// copy. Locators are FK/ordinal values only — never SQL input.
func TestPGMultiEntryDelivery_NavigationBoundaries(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetClean := setupPGClaimTarget(t, db, fix, "pgmulti-dlv-nb-clean")
	targetMasked := setupPGClaimTarget(t, db, fix, "pgmulti-dlv-nb-masked")

	meta := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = meta.Close(context.Background()) }()
	for _, stmt := range []string{
		`CREATE TABLE app.nav_cases (id bigint PRIMARY KEY, note text)`,
		`CREATE TABLE app.nav_cases_src (id bigint PRIMARY KEY, cid bigint,
		    CONSTRAINT fk_nav_cases FOREIGN KEY (cid) REFERENCES app.nav_cases(id))`,
		`INSERT INTO app.nav_cases VALUES (1, repeat('a', 8192))`, // exact limit
		`INSERT INTO app.nav_cases VALUES (2, repeat('b', 8193))`, // one byte over
		`INSERT INTO app.nav_cases VALUES (3, repeat('中', 2731))`, // 8193 bytes, 2731 runes
		`INSERT INTO app.nav_cases VALUES (4, repeat('🙂', 2048))`, // 8192 bytes, 2048 runes
		`INSERT INTO app.nav_cases VALUES (5, repeat('🙂', 2049))`, // 8196 bytes, 2049 runes
		`INSERT INTO app.nav_cases VALUES (6, NULL)`,              // SQL NULL
		`INSERT INTO app.nav_cases VALUES (7, '')`,                // empty
		`INSERT INTO app.nav_cases VALUES (8, 'NULL')`,            // literal text "NULL"
		`INSERT INTO app.nav_cases_src (id, cid) VALUES (1, 1)`,   // after parent rows — real FK
		`GRANT SELECT ON app.nav_cases TO ` + pgDiscRole,
		`GRANT SELECT ON app.nav_cases_src TO ` + pgDiscRole,
	} {
		execPG(t, meta, stmt)
	}

	// Two target-policy scopes over the same tables: raw vs masked.
	navPolicy(t, disc, targetClean, "discdb", "app", "nav_cases", "id", "note")
	insertPolicy(t, disc, targetMasked, "discdb", "app", "nav_cases", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetMasked, "discdb", "app", "nav_cases", "note", model.ResultDisclosureMaskedNoCopy)

	src := navSource("discdb", "app", "nav_cases_src", "fk_nav_cases")
	locator := func(key, id string, caps []string) service.PGClaimedNavigationRequest {
		r := navRequest(key, src, []string{id})
		r.Capabilities = caps
		return r
	}
	noteCell := func(res *service.PGDisclosedResult) (string, bool, bool) {
		c := res.Rows[0][1]
		if c == nil {
			return "", true, res.CellTruncated[0][1]
		}
		return *c, false, res.CellTruncated[0][1]
	}
	wantShape := func(res *service.PGDisclosedResult) {
		t.Helper()
		if len(res.Rows) != 1 || len(res.Rows[0]) != 2 || len(res.CellTruncated) != 1 || len(res.CellTruncated[0]) != 2 {
			t.Fatalf("nav boundary page = %+v, want 1 row × 2 aligned matrix cells", res)
		}
	}

	// Clean boundary values deliver under no capability, with exact final
	// text, UTF-8 validity and byte lengths.
	for _, tc := range []struct {
		key, id, want string
		null          bool
		wantBytes     int
	}{
		{"pgmulti-dlv-nb-1", "1", strings.Repeat("a", 8192), false, 8192},
		{"pgmulti-dlv-nb-4", "4", strings.Repeat("🙂", 2048), false, 8192},
		{"pgmulti-dlv-nb-7", "7", "", false, 0},
		{"pgmulti-dlv-nb-8", "8", "NULL", false, 4}, // literal "NULL" ≠ SQL NULL: distinct
	} {
		out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetClean, locator(tc.key, tc.id, nil))
		if err != nil {
			t.Fatalf("nav %s: %v", tc.key, err)
		}
		wantShape(out.Result)
		v, isNull, tr := noteCell(out.Result)
		if isNull != tc.null || (!isNull && (v != tc.want || len(v) != tc.wantBytes || !utf8.ValidString(v))) || tr {
			t.Fatalf("nav %s cell = %q null=%v flag=%v (want %d bytes, null=%v, flag=false)",
				tc.key, v, isNull, tr, tc.wantBytes, tc.null)
		}
	}

	// SQL NULL: nil cell, never flagged.
	out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetClean,
		locator("pgmulti-dlv-nb-6", "6", nil))
	if err != nil {
		t.Fatalf("nav NULL row: %v", err)
	}
	wantShape(out.Result)
	if _, isNull, tr := noteCell(out.Result); !isNull || tr {
		t.Fatalf("nav NULL cell = %+v flag=%v, want nil/false", out.Result.Rows[0][1], tr)
	}

	// 8193 bytes without the capability: upgrade refusal — real execution
	// id, persisted success status, evidence, history row and claim link,
	// but no delivered Result.
	histBefore := countWhere(t, db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetClean)
	linkBefore := countWhere(t, db, `SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ?`, targetClean)
	auditBefore := navAuditEvents(t, db, targetClean)
	refused, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetClean,
		locator("pgmulti-dlv-nb-2", "2", nil))
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("8193 no-cap = %v, want upgrade required", err)
	}
	if refused.ExecutionID == 0 || refused.Result != nil || refused.Status != model.QueryExecutionSuccess || refused.Evidence == nil {
		t.Fatalf("8193 no-cap outcome = %+v, want persisted success id+evidence with no Result", refused)
	}
	if refused.Evidence.Status != refused.Status {
		t.Fatalf("evidence status %q ≠ outcome status %q", refused.Evidence.Status, refused.Status)
	}
	if status, _ := keyedSavedRow(t, db, targetClean, "pgmulti-dlv-nb-2"); status != "success" {
		t.Fatalf("keyed persisted status = %q, want success", status)
	}
	if countWhere(t, db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetClean) != histBefore+1 ||
		countWhere(t, db, `SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ?`, targetClean) != linkBefore+1 ||
		navAuditEvents(t, db, targetClean) != auditBefore+1 {
		t.Fatal("upgrade refusal did not persist exactly one success attempt (history/audit/link)")
	}
	// SAME key retried WITH the capability is still a duplicate — the
	// refused attempt's claim stays occupied and no result materializes.
	dup, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetClean,
		locator("pgmulti-dlv-nb-2", "2", []string{service.CapabilityCellTruncated}))
	if !errors.Is(err, service.ErrQueryExecutionAlreadyExists) || dup.Result != nil {
		t.Fatalf("same-key capable retry = %v result=%+v, want duplicate with no result", err, dup.Result)
	}
	// Fresh keys with the capability — the exact singleton spelling and a
	// duplicated-capability spelling — deliver the ASCII prefix flagged.
	for _, tc := range []struct {
		key  string
		caps []string
	}{
		{"pgmulti-dlv-nb-2c", []string{service.CapabilityCellTruncated}},
		{"pgmulti-dlv-nb-2cd", []string{service.CapabilityCellTruncated, service.CapabilityCellTruncated}},
	} {
		got, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetClean,
			locator(tc.key, "2", tc.caps))
		if err != nil {
			t.Fatalf("capable 8193 nav %s: %v", tc.key, err)
		}
		wantShape(got.Result)
		v, isNull, tr := noteCell(got.Result)
		if isNull || v != strings.Repeat("b", 8192) || !utf8.ValidString(v) || !tr || got.Result.CellTruncated[0][0] {
			t.Fatalf("capable %s cell len=%d null=%v flags=%v, want the 8192-byte 'b' prefix flagged on the value cell only",
				tc.key, len(v), isNull, got.Result.CellTruncated[0])
		}
	}
	// Over-limit multibyte rows under capability: 中×2731 (8193 bytes)
	// cuts at the rune boundary to 8190 (2730 three-byte runes); 🙂×2049
	// (8196 bytes) returns exactly 8192 bytes.
	got3, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetClean,
		locator("pgmulti-dlv-nb-3c", "3", []string{service.CapabilityCellTruncated}))
	if err != nil {
		t.Fatalf("capable 8193-CJK nav: %v", err)
	}
	v, isNull, tr := noteCell(got3.Result)
	if isNull || len(v) != 8190 || v != strings.Repeat("中", 2730) || !utf8.ValidString(v) || !tr {
		t.Fatalf("capable 8193-CJK cell len=%d null=%v flag=%v, want 8190 bytes flagged", len(v), isNull, tr)
	}
	got5, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetClean,
		locator("pgmulti-dlv-nb-5c", "5", []string{service.CapabilityCellTruncated}))
	if err != nil {
		t.Fatalf("capable 8196 nav: %v", err)
	}
	v, isNull, tr = noteCell(got5.Result)
	if isNull || v != strings.Repeat("🙂", 2048) || !utf8.ValidString(v) || !tr {
		t.Fatalf("capable 8196 cell len=%d null=%v flag=%v, want the 2048-rune prefix flagged", len(v), isNull, tr)
	}
	// Unknown capability does not alter permissions — still the refusal.
	unknown, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetClean,
		locator("pgmulti-dlv-nb-2u", "2", []string{"arbitrary_capability"}))
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) || unknown.Result != nil {
		t.Fatalf("unknown-cap 8193 = %v result=%+v, want upgrade refusal with no result", err, unknown.Result)
	}

	// Masked scope: the long cell is replaced by the fixed marker AND the
	// truncation flag stays set — masked is not copyable and CSV is denied.
	maskCap, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetMasked,
		locator("pgmulti-dlv-nb-mc", "2", []string{service.CapabilityCellTruncated}))
	if err != nil {
		t.Fatalf("capable masked nav: %v", err)
	}
	wantShape(maskCap.Result)
	v, isNull, tr = noteCell(maskCap.Result)
	if isNull || v != "[MASKED]" || !tr {
		t.Fatalf("masked-long cell = %q null=%v flag=%v, want [MASKED]/true", v, isNull, tr)
	}
	if maskCap.Result.Columns[1].CopyAllowed {
		t.Fatal("masked nav column became copyable")
	}
	cols, rws, matrix := csvArgs(maskCap.Result)
	if err := service.DecideCSVExport(cols, rws, matrix); !errors.Is(err, service.ErrCSVExportNotPermitted) {
		t.Fatalf("masked nav page csv = %v, want not permitted", err)
	}
	// No-cap masked-long: upgrade refusal, persisted success, no result.
	maskNoCap, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetMasked,
		locator("pgmulti-dlv-nb-mn", "2", nil))
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) || maskNoCap.Result != nil ||
		maskNoCap.Status != model.QueryExecutionSuccess || maskNoCap.ExecutionID == 0 {
		t.Fatalf("no-cap masked-long = %v outcome=%+v, want upgrade refusal + persisted success id", err, maskNoCap)
	}
	// Masked NULL stays nil unflagged with no capability needed.
	maskNull, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetMasked,
		locator("pgmulti-dlv-nb-mnull", "6", nil))
	if err != nil {
		t.Fatalf("masked null nav: %v", err)
	}
	if _, isNull, tr := noteCell(maskNull.Result); !isNull || tr {
		t.Fatalf("masked NULL nav = %+v flag=%v, want nil/false", maskNull.Result.Rows[0][1], tr)
	}
}
