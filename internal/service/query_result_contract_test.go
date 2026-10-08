// Package service tests the T8-A result-contract components.
// input: encoding/json, errors, fmt, strings, testing, time, unicode/utf8, internal/model
// output: acceptance coverage A-E — byte-boundary truncation, native-text fidelity, matrix alignment, capability gate ordering, CSV eligibility, and both response-envelope JSON contracts
// pos: Proves the result-contract component on raw (untruncated) inputs; production scanning, evidence, and HTTP wiring are not changed or claimed by this slice
// note: if this file changes, update this header and module README.md.
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/fan/controlhub/internal/model"
)

func copyableColumn(name string) model.QueryResultColumn {
	return model.QueryResultColumn{
		Name:         name,
		DatabaseType: "VARCHAR",
		DisplayMode:  model.ResultDisclosureRawCopyAllowed,
		CopyAllowed:  true,
	}
}

// ---------------------------------------------------------------------------
// A. Byte/rune boundaries — 8192 bytes, rune-safe, no decoration.
// ---------------------------------------------------------------------------

func TestTruncateCellTextByteBoundaries(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantLen   int // exact output bytes
		wantTrunc bool
	}{
		{"ascii 8191", strings.Repeat("a", 8191), 8191, false},
		{"ascii 8192", strings.Repeat("a", 8192), 8192, false},
		{"ascii 8193", strings.Repeat("a", 8193), 8192, true},
		// 2730×3 = 8190 ≤ 8192 → unchanged.
		{"chinese exact-fit 8190", strings.Repeat("中", 2730), 8190, false},
		// 2731×3 = 8193 > 8192 → longest rune-safe prefix = 8190 bytes.
		{"chinese over 8193", strings.Repeat("中", 2731), 8190, true},
		// 2048×4 = 8192 exactly → unchanged.
		{"4-byte exact 8192", strings.Repeat("🙂", 2048), 8192, false},
		// 2049×4 = 8196 → cut at 8192 (2048 complete runes).
		{"4-byte over 8196", strings.Repeat("🙂", 2049), 8192, true},
		// Multibyte rune lands exactly at the cap: 8189+3=8192 → unchanged.
		{"rune ends at cap", strings.Repeat("a", 8189) + "中", 8192, false},
		// 8190+3=8193 → prefix 8190 (rune start), flag true.
		{"rune crosses cap", strings.Repeat("a", 8190) + "中", 8190, true},
		// 8191+3=8194 → prefix 8191, flag true.
		{"rune starts past cap", strings.Repeat("a", 8191) + "中", 8191, true},
		{"empty", "", 0, false},
		{"literal NULL text", "NULL", 4, false},
		{"mixed over cap", strings.Repeat("é", 100) + strings.Repeat("x", 8200) + "中", 8192, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated, err := TruncateCellText(tc.input, ResultCellMaxBytes)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if truncated != tc.wantTrunc {
				t.Fatalf("truncated=%v want %v", truncated, tc.wantTrunc)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("len=%d want %d", len(got), tc.wantLen)
			}
			if !utf8.ValidString(got) {
				t.Fatal("output is not valid UTF-8")
			}
			if !truncated && got != tc.input {
				t.Fatal("untruncated output must equal input byte-for-byte")
			}
			if truncated {
				if strings.HasSuffix(got, "...") || strings.Contains(got, "truncated") {
					t.Fatal("output must not carry ellipsis or notes")
				}
				if !strings.HasPrefix(tc.input, got) {
					t.Fatal("output must be a strict byte prefix of input")
				}
			}
		})
	}
}

func TestTruncateCellTextRejectsInvalidInput(t *testing.T) {
	if _, _, err := TruncateCellText(string([]byte{0xff, 0xfe, 0xfd}), ResultCellMaxBytes); err == nil {
		t.Fatal("invalid UTF-8 must be rejected, not silently repaired")
	}
	if _, _, err := TruncateCellText("ok", 0); err == nil {
		t.Fatal("non-positive budget must be rejected")
	}
}

// ---------------------------------------------------------------------------
// B. Native fidelity — text stays raw; nothing is parsed or reformatted.
// ---------------------------------------------------------------------------

func TestTruncateCellValueFidelityAndShapes(t *testing.T) {
	// Over-long JSON text is cut, never re-closed or quoted.
	longJSON := `{"k":"` + strings.Repeat("v", 9000) + `"}`
	out, truncated, err := TruncateCellValue(longJSON, ResultCellMaxBytes)
	if err != nil || !truncated {
		t.Fatalf("long JSON: out=%v trunc=%v err=%v", err, truncated, err)
	}
	if s := out.(string); len(s) != ResultCellMaxBytes || strings.HasSuffix(s, `"}]`) {
		t.Fatalf("truncated JSON must be a raw prefix, got tail %q", s[len(s)-8:])
	}
	// Declared non-text shapes pass through untouched — no restringify.
	now := time.Date(2026, 1, 2, 3, 4, 5, 999, time.UTC)
	for _, v := range []any{int64(-9223372036854775808), float64(3.14159), true, now} {
		got, trunc, err := TruncateCellValue(v, ResultCellMaxBytes)
		if err != nil || trunc || got != v {
			t.Fatalf("%T must pass through: got=%v trunc=%v err=%v", v, got, trunc, err)
		}
	}
	// nil / "" / "NULL" stay three distinct values.
	for _, v := range []any{nil, "", "NULL"} {
		got, trunc, err := TruncateCellValue(v, ResultCellMaxBytes)
		if err != nil || trunc || got != v {
			t.Fatalf("%#v must stay distinct: got=%#v", v, got)
		}
	}
	// Undeclared shapes (raw []byte, maps) are rejected — never stringified.
	for _, v := range []any{[]byte("blob"), map[string]any{"a": 1}, 7} {
		if _, _, err := TruncateCellValue(v, ResultCellMaxBytes); err == nil {
			t.Fatalf("%T must be rejected, not coerced", v)
		}
	}
}

// ---------------------------------------------------------------------------
// C. Matrix — alignment, clone isolation, prior-flag preservation.
// ---------------------------------------------------------------------------

func TestTruncateRowAndMatrixAlignment(t *testing.T) {
	long := strings.Repeat("a", 9000)
	row, flags, err := TruncateRow([]any{long, "ok", nil, int64(5)}, nil, ResultCellMaxBytes)
	if err != nil {
		t.Fatalf("truncate row: %v", err)
	}
	if len(row) != 4 || row[0].(string) != long[:ResultCellMaxBytes] || row[1] != "ok" || row[2] != nil || row[3] != int64(5) {
		t.Fatalf("row values wrong: %v", row)
	}
	if fmt.Sprint(flags) != "[true false false false]" {
		t.Fatalf("flags wrong: %v", flags)
	}
	// An upstream true is never cleared by reprocessing the truncated value.
	_, flags2, err := TruncateRow(row, flags, ResultCellMaxBytes)
	if err != nil {
		t.Fatalf("reprocess: %v", err)
	}
	if !flags2[0] {
		t.Fatal("re-processing an already-truncated cell must keep true")
	}
	if _, _, err := TruncateRow([]any{"a"}, []bool{false, false}, ResultCellMaxBytes); err == nil {
		t.Fatal("prior flag width mismatch must be rejected")
	}
}

func TestValidateCellTruncatedMatrix(t *testing.T) {
	rows := [][]any{{"x", "y"}, {"p", "q"}}
	// Aligned matrix on a non-empty page is legal evidence.
	if err := validateCellTruncatedMatrix(2, rows, [][]bool{{true, false}, {false, false}}); err != nil {
		t.Fatalf("aligned matrix: %v", err)
	}
	// A non-empty page without a matrix is missing evidence — rejected, not
	// treated as a known-clean page.
	if err := validateCellTruncatedMatrix(2, rows, nil); err == nil {
		t.Fatal("nil matrix on a non-empty page must be rejected as missing evidence")
	}
	// Row width is enforced even when the matrix supplies no evidence.
	if err := validateCellTruncatedMatrix(1, [][]any{{"x", "y"}}, nil); err == nil {
		t.Fatal("row wider than public columns must be rejected even with nil matrix")
	}
	// Legal empty pages: zero rows with nil or zero-row matrix.
	if err := validateCellTruncatedMatrix(2, nil, nil); err != nil {
		t.Fatalf("zero-row nil matrix: %v", err)
	}
	if err := validateCellTruncatedMatrix(2, nil, [][]bool{}); err != nil {
		t.Fatalf("zero-row empty matrix: %v", err)
	}
	// A residual non-empty matrix on a zero-row page is corrupt evidence.
	for _, residual := range [][][]bool{{{false}}, {{true}}} {
		if err := validateCellTruncatedMatrix(2, nil, residual); err == nil {
			t.Fatalf("residual matrix %v on a zero-row page must be rejected", residual)
		}
	}
	for _, bad := range [][][]bool{
		{{true}},                               // row count mismatch
		{{true, false}, {false}},               // ragged matrix row
		{{true, false, false}, {false, false}}, // matrix wider than columns
	} {
		if err := validateCellTruncatedMatrix(2, rows, bad); err == nil {
			t.Fatalf("malformed matrix %v must be rejected", bad)
		}
	}
	// Clone must not alias the producer's backing array.
	src := [][]bool{{true, false}}
	clone := CloneCellTruncated(src)
	clone[0][0] = false
	if !src[0][0] {
		t.Fatal("clone mutated the source matrix")
	}
	if CloneCellTruncated(nil) != nil {
		t.Fatal("nil matrix must clone to nil — no info ≠ clean")
	}
}

// ---------------------------------------------------------------------------
// D. Capability gate — post-finalize, upstream error wins.
// ---------------------------------------------------------------------------

func TestDecideResultDeliveryCapabilityGate(t *testing.T) {
	cols := []model.QueryResultColumn{copyableColumn("a")}
	cleanRows := [][]any{{"v"}}
	cleanMatrix := [][]bool{{false}}
	truncMatrix := [][]bool{{true}}
	truncRows := [][]any{{strings.Repeat("a", ResultCellMaxBytes)}}

	t.Run("no capability + clean page delivered", func(t *testing.T) {
		d, err := DecideResultDelivery(ResultDeliveryInput{Columns: cols, Rows: cleanRows, CellTruncated: cleanMatrix})
		if err != nil || len(d.Rows) != 1 {
			t.Fatalf("clean page must deliver: %v %+v", err, d)
		}
	})
	t.Run("no capability + truncated page refused without rows", func(t *testing.T) {
		d, err := DecideResultDelivery(ResultDeliveryInput{Columns: cols, Rows: truncRows, CellTruncated: truncMatrix})
		if !errors.Is(err, ErrResultContractUpgradeRequired) {
			t.Fatalf("want upgrade-required, got %v", err)
		}
		if d.Rows != nil || d.CellTruncated != nil {
			t.Fatalf("refusal must carry no rows/matrix: %+v", d)
		}
	})
	t.Run("capable client receives truncated page and matrix", func(t *testing.T) {
		d, err := DecideResultDelivery(ResultDeliveryInput{
			Columns: cols, Rows: truncRows, CellTruncated: truncMatrix,
			Capabilities: []string{CapabilityCellTruncated},
		})
		if err != nil || !d.CellTruncated[0][0] {
			t.Fatalf("capable delivery lost flag: %v %+v", err, d)
		}
	})
	t.Run("unknown and duplicate capabilities", func(t *testing.T) {
		for _, caps := range [][]string{
			{"celltruncated"}, {"cellTruncated "}, {"cellTruncatedX"}, {"other"},
		} {
			if _, err := DecideResultDelivery(ResultDeliveryInput{Columns: cols, Rows: truncRows, CellTruncated: truncMatrix, Capabilities: caps}); !errors.Is(err, ErrResultContractUpgradeRequired) {
				t.Fatalf("caps %v must not satisfy the gate", caps)
			}
		}
		if _, err := DecideResultDelivery(ResultDeliveryInput{
			Columns: cols, Rows: truncRows, CellTruncated: truncMatrix,
			Capabilities: []string{CapabilityCellTruncated, CapabilityCellTruncated, "other"},
		}); err != nil {
			t.Fatalf("duplicate capability must not break delivery: %v", err)
		}
	})
	t.Run("upstream error wins over capability refusal", func(t *testing.T) {
		upstream := fmt.Errorf("%w: could not record query attempt", ErrQueryBackendFailure)
		// Even with a truncated page AND no capability, the evidence failure is
		// returned unchanged — the capability error never masks it.
		_, err := DecideResultDelivery(ResultDeliveryInput{UpstreamErr: upstream, Columns: cols, Rows: truncRows, CellTruncated: truncMatrix})
		if !errors.Is(err, ErrQueryBackendFailure) || errors.Is(err, ErrResultContractUpgradeRequired) {
			t.Fatalf("upstream must win: %v", err)
		}
	})
	t.Run("non-empty page without matrix is missing evidence, not clean", func(t *testing.T) {
		// Really truncate a raw over-long value through the production
		// primitive, then drop the produced matrix before the delivery gate —
		// both capability states must fail with the fixed internal evidence
		// error, never as a clean page or a missing capability.
		row, _, err := TruncateRow([]any{strings.Repeat("a", 9000)}, nil, ResultCellMaxBytes)
		if err != nil {
			t.Fatalf("truncate: %v", err)
		}
		for _, caps := range [][]string{nil, {CapabilityCellTruncated}} {
			d, err := DecideResultDelivery(ResultDeliveryInput{
				Columns: cols, Rows: [][]any{row}, Capabilities: caps,
			})
			if err == nil || errors.Is(err, ErrResultContractUpgradeRequired) {
				t.Fatalf("caps=%v missing matrix must be the internal evidence error, got %v", caps, err)
			}
			if d.Rows != nil || d.CellTruncated != nil {
				t.Fatalf("missing-evidence refusal must carry no rows: %+v", d)
			}
		}
	})
	t.Run("row width checked even without matrix", func(t *testing.T) {
		_, err := DecideResultDelivery(ResultDeliveryInput{
			Columns: []model.QueryResultColumn{copyableColumn("a")},
			Rows:    [][]any{{"x", "y"}}, // width 2 vs 1 column
		})
		if err == nil || errors.Is(err, ErrResultContractUpgradeRequired) {
			t.Fatalf("misaligned row must fail as evidence error, got %v", err)
		}
	})
	t.Run("zero-row pages: nil or empty matrix legal, residual corrupt", func(t *testing.T) {
		for _, m := range [][][]bool{nil, {}} {
			if _, err := DecideResultDelivery(ResultDeliveryInput{Columns: cols, CellTruncated: m}); err != nil {
				t.Fatalf("empty page with matrix %v must deliver: %v", m, err)
			}
		}
		for _, m := range [][][]bool{{{false}}, {{true}}} {
			if _, err := DecideResultDelivery(ResultDeliveryInput{Columns: cols, CellTruncated: m}); err == nil {
				t.Fatalf("residual matrix %v on empty page must be rejected", m)
			}
		}
	})
	t.Run("corrupt matrix fails loud even for capable client", func(t *testing.T) {
		_, err := DecideResultDelivery(ResultDeliveryInput{
			Columns: cols, Rows: truncRows, CellTruncated: [][]bool{{true, true}},
			Capabilities: []string{CapabilityCellTruncated},
		})
		if !errors.Is(err, errResultContractInvalid) {
			t.Fatalf("corrupt matrix must not deliver success: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// E. CSV export eligibility — no endpoint, shared decision only.
// ---------------------------------------------------------------------------

func TestDecideCSVExport(t *testing.T) {
	cols := []model.QueryResultColumn{copyableColumn("a"), copyableColumn("b")}
	rows := [][]any{{"x", "y"}}

	t.Run("clean copyable page allowed", func(t *testing.T) {
		if err := DecideCSVExport(cols, rows, [][]bool{{false, false}}); err != nil {
			t.Fatalf("clean page: %v", err)
		}
	})
	t.Run("single truncated cell denies the whole page", func(t *testing.T) {
		if err := DecideCSVExport(cols, rows, [][]bool{{false, true}}); !errors.Is(err, ErrCSVExportNotPermitted) {
			t.Fatalf("truncated page must deny: %v", err)
		}
	})
	t.Run("masked or blocked columns deny even when clean", func(t *testing.T) {
		for _, mode := range []model.ResultDisclosureMode{model.ResultDisclosureMaskedNoCopy, model.ResultDisclosureBlocked} {
			blocked := cols[0]
			blocked.DisplayMode = mode
			blocked.CopyAllowed = false
			if err := DecideCSVExport([]model.QueryResultColumn{blocked, cols[1]}, rows, [][]bool{{false, false}}); !errors.Is(err, ErrCSVExportNotPermitted) {
				t.Fatalf("mode %s must deny export", mode)
			}
		}
		blocked := cols[0]
		blocked.CopyAllowed = false // raw mode but copy disallowed
		if err := DecideCSVExport([]model.QueryResultColumn{blocked, cols[1]}, rows, [][]bool{{false, false}}); !errors.Is(err, ErrCSVExportNotPermitted) {
			t.Fatal("copyAllowed=false must deny")
		}
	})
	t.Run("missing or corrupt matrix is not authorized-clean", func(t *testing.T) {
		if err := DecideCSVExport(cols, rows, nil); !errors.Is(err, ErrCSVExportNotPermitted) {
			t.Fatalf("nil matrix with rows must deny: %v", err)
		}
		if err := DecideCSVExport(cols, rows, [][]bool{{false}}); !errors.Is(err, ErrCSVExportNotPermitted) {
			t.Fatalf("narrow matrix must deny: %v", err)
		}
	})
	t.Run("zero rows can export headers when all columns copyable", func(t *testing.T) {
		if err := DecideCSVExport(cols, nil, nil); err != nil {
			t.Fatalf("empty page: %v", err)
		}
		if err := DecideCSVExport(cols, nil, [][]bool{}); err != nil {
			t.Fatalf("empty page + empty matrix: %v", err)
		}
	})
	t.Run("residual matrix on zero rows denies", func(t *testing.T) {
		for _, m := range [][][]bool{{{false, false}}, {{true, false}}} {
			if err := DecideCSVExport(cols, nil, m); !errors.Is(err, ErrCSVExportNotPermitted) {
				t.Fatalf("residual matrix %v on empty page must deny: %v", m, err)
			}
		}
	})
	t.Run("row width is checked even without matrix", func(t *testing.T) {
		oneCol := []model.QueryResultColumn{copyableColumn("a")}
		if err := DecideCSVExport(oneCol, [][]any{{"x", "y"}}, nil); !errors.Is(err, ErrCSVExportNotPermitted) {
			t.Fatalf("misaligned row with nil matrix must deny: %v", err)
		}
	})
	t.Run("no columns denies", func(t *testing.T) {
		if err := DecideCSVExport(nil, nil, nil); !errors.Is(err, ErrCSVExportNotPermitted) {
			t.Fatalf("column-less page must deny: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// Envelope adaptation: both response shapes carry the matrix per JSON contract.
// ---------------------------------------------------------------------------

func TestCellTruncatedEnvelopeContracts(t *testing.T) {
	cols := []model.QueryResultColumn{copyableColumn("a"), copyableColumn("b")}
	rows := [][]any{{"v1", "v2"}}
	matrix := [][]bool{{false, true}}

	attached, err := attachCellTruncated(cols, rows, matrix)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	// The adapter clones: mutating the attached copy leaves the producer's
	// matrix untouched.
	attached[0][1] = false
	if !matrix[0][1] {
		t.Fatal("adapter must not alias producer matrix")
	}
	attached[0][1] = true

	exec := model.QueryExecuteResponse{Columns: cols, Rows: rows, RowCount: 1, CellTruncated: attached}
	raw, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("marshal execute: %v", err)
	}
	if !strings.Contains(string(raw), `"cellTruncated":[[false,true]]`) {
		t.Fatalf("execute envelope must carry the matrix: %s", raw)
	}
	var execBack model.QueryExecuteResponse
	if err := json.Unmarshal(raw, &execBack); err != nil || !execBack.CellTruncated[0][1] {
		t.Fatalf("execute round-trip lost the flag: %v %+v", err, execBack)
	}

	nav := model.RelatedRecordNavigationResponse{Columns: cols, Rows: rows, RowCount: 1, CellTruncated: attached}
	raw, err = json.Marshal(nav)
	if err != nil || !strings.Contains(string(raw), `"cellTruncated":[[false,true]]`) {
		t.Fatalf("navigation envelope must carry the matrix: %s %v", raw, err)
	}

	// Absent matrix serializes as no cellTruncated key (omitempty) —
	// "no information" stays distinguishable from a delivered clean matrix.
	raw, err = json.Marshal(model.QueryExecuteResponse{Columns: cols, Rows: rows, RowCount: 1})
	if err != nil || strings.Contains(string(raw), "cellTruncated") {
		t.Fatalf("nil matrix must omit the field: %s", raw)
	}

	// Misaligned matrix must never attach to either envelope.
	if _, err := attachCellTruncated(cols, rows, [][]bool{{false}}); err == nil {
		t.Fatal("misaligned matrix must not attach")
	}
	// The adapter enforces evidence: a non-empty page cannot attach a nil
	// matrix, while an empty page leaves the field unset (omitempty stays).
	if _, err := attachCellTruncated(cols, rows, nil); err == nil {
		t.Fatal("nil matrix on a non-empty page must be rejected")
	}
	empty, err := attachCellTruncated(cols, nil, nil)
	if err != nil || empty != nil {
		t.Fatalf("empty page attach must stay nil: %v %v", empty, err)
	}
}

// ---------------------------------------------------------------------------
// R1-2: the decision owns its slices — caller/decision mutation isolation.
// ---------------------------------------------------------------------------

func TestDecideResultDeliveryDecisionOwnsSlices(t *testing.T) {
	cols := []model.QueryResultColumn{copyableColumn("a"), copyableColumn("b")}
	inRows := [][]any{
		{"r1c1", "r1c2"},
		{"r2c1", "r2c2"},
	}
	inMatrix := [][]bool{
		{false, true},
		{false, false},
	}
	d, err := DecideResultDelivery(ResultDeliveryInput{
		Columns: cols, Rows: inRows, CellTruncated: inMatrix,
		Capabilities: []string{CapabilityCellTruncated},
	})
	if err != nil {
		t.Fatalf("delivery: %v", err)
	}

	// Mutate every level of the producer input: cell value, row slice, outer
	// rows slice, matrix flag, matrix row.
	inRows[0][0] = "MUTATED"
	inRows[1] = []any{"X", "Y"}
	inRows[0] = nil
	inMatrix[0][1] = false
	inMatrix[1][0] = true
	inMatrix[0] = nil

	if d.Rows[0][0] != "r1c1" || d.Rows[0][1] != "r1c2" ||
		d.Rows[1][0] != "r2c1" || d.Rows[1][1] != "r2c2" {
		t.Fatalf("input mutation reached the decision: %+v", d.Rows)
	}
	if !d.CellTruncated[0][1] || d.CellTruncated[1][0] {
		t.Fatalf("input mutation reached the decision matrix: %+v", d.CellTruncated)
	}
	// The decision's own flag still governs CSV: the page stays denied.
	if err := DecideCSVExport(cols, d.Rows, d.CellTruncated); !errors.Is(err, ErrCSVExportNotPermitted) {
		t.Fatalf("decision must still deny CSV after input mutation: %v", err)
	}

	// Reverse direction: mutating the decision must not reach the producer's
	// surviving structures. Reset inputs to a second pristine page first.
	inRows = [][]any{{"keep1", "keep2"}}
	inMatrix = [][]bool{{true, false}}
	d2, err := DecideResultDelivery(ResultDeliveryInput{
		Columns: cols, Rows: inRows, CellTruncated: inMatrix,
		Capabilities: []string{CapabilityCellTruncated},
	})
	if err != nil {
		t.Fatalf("delivery 2: %v", err)
	}
	d2.Rows[0][0] = "DECISION-MUTATED"
	d2.CellTruncated[0][0] = false
	if inRows[0][0] != "keep1" || !inMatrix[0][0] {
		t.Fatal("decision mutation reached the producer input")
	}

	// Refusal stays a zero decision that shares nothing.
	d3, err := DecideResultDelivery(ResultDeliveryInput{
		Columns: cols, Rows: inRows, CellTruncated: inMatrix, // no capability
	})
	if !errors.Is(err, ErrResultContractUpgradeRequired) || d3.Rows != nil || d3.CellTruncated != nil {
		t.Fatalf("refusal must return zero decision: %v %+v", err, d3)
	}
}

// ---------------------------------------------------------------------------
// R1-3: a truncated result must not retain the giant source string's storage.
// ---------------------------------------------------------------------------

func TestTruncateCellTextReleasesSourceStorage(t *testing.T) {
	// A multi-megabyte source is truncated to the budget; the returned string
	// must be an independent small copy, not a subslice pinning the source's
	// backing array (test-only unsafe observation; production stays unsafe-free).
	big := strings.Repeat("a", 4<<20) + "中"
	got, truncated, err := TruncateCellText(big, ResultCellMaxBytes)
	if err != nil || !truncated || len(got) != ResultCellMaxBytes {
		t.Fatalf("truncation contract changed: len=%d trunc=%v err=%v", len(got), truncated, err)
	}
	if unsafe.StringData(got) == unsafe.StringData(big) {
		t.Fatal("truncated result still aliases the multi-MiB source backing store")
	}

	// The untruncated branch deliberately does NOT copy — same base pointer is
	// fine there (no waste), only the truncated branch must isolate storage.
	small := "small"
	got2, truncated2, err := TruncateCellText(small, ResultCellMaxBytes)
	if err != nil || truncated2 || got2 != small {
		t.Fatalf("untruncated contract changed: %q %v %v", got2, truncated2, err)
	}

	// TruncateRow must route through the same isolating primitive.
	row, flags, err := TruncateRow([]any{big}, nil, ResultCellMaxBytes)
	if err != nil || !flags[0] || len(row[0].(string)) != ResultCellMaxBytes {
		t.Fatalf("row path changed: %v %v", flags, err)
	}
	if unsafe.StringData(row[0].(string)) == unsafe.StringData(big) {
		t.Fatal("TruncateRow result aliases the source backing store")
	}
}
