// Package service provides business logic for the Phase 37/38S read-only query sandbox.
// input: errors, fmt, time, unicode/utf8, internal/model
// output: TruncateCellText, TruncateCellValue, TruncateRow, CellTruncatedMatrix helpers (CloneCellTruncated, AnyCellTruncated), ResultDeliveryInput/Decision + DecideResultDelivery + ErrResultContractUpgradeRequired, DecideCSVExport + ErrCSVExportNotPermitted, CapabilityCellTruncated, ResultCellMaxBytes
// pos: Post-finalize result-delivery contract for the governed query workbench (T8-A, spec G2/G7): rune-safe per-cell truncation, cellTruncated matrix alignment, capability gate, and CSV export eligibility — shared by all three governed result paths without touching production scanning, evidence, or routing
// note: if this file changes, update this header and module README.md.
package service

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/model"
)

// ResultCellMaxBytes is the fixed per-cell text budget: 8192 BYTES, not
// characters (spec G7). Callers pass it to the truncation primitives; it is
// not a configurable system — wiring keeps the executor's existing cell cap.
const ResultCellMaxBytes = 8192

// CapabilityCellTruncated is the exact result-contract capability token a
// client must declare before a page containing truncated cells may be
// delivered to it (spec G2). Comparison is byte-exact; unknown tokens and
// duplicates never satisfy the requirement.
const CapabilityCellTruncated = "cellTruncated"

// ErrResultContractUpgradeRequired is the controlled refusal when a page
// contains at least one truncated cell but the client did not declare the
// cellTruncated capability (maps to HTTP code
// "result_contract_upgrade_required" at the shared result boundary). It is a
// delivery refusal only — the underlying attempt already recorded success;
// it never carries cell values, changes recorded status, or writes evidence.
var ErrResultContractUpgradeRequired = errors.New("result contract upgrade required")

// ErrCSVExportNotPermitted is the controlled refusal for CSV export of a
// governed result page: any truncated cell, missing/corrupt truncation
// evidence, or a column without copy permission denies export.
var ErrCSVExportNotPermitted = errors.New("csv export not permitted")

// errResultContractInvalid is the fixed internal error for inputs outside
// the declared materialized value shapes or a malformed truncation matrix.
// It never embeds cell contents.
var errResultContractInvalid = errors.New("result contract invalid")

// ---------------------------------------------------------------------------
// 1. Per-cell truncation primitives
// ---------------------------------------------------------------------------

// TruncateCellText enforces the per-cell byte budget on one valid-UTF-8 text
// value. len(s) <= maxBytes returns the input untouched with flag false. Over
// budget, it returns the longest complete UTF-8 prefix within maxBytes — the
// multi-byte tail is never split — with flag true. No ellipsis, padding, or
// reformatting is applied; the flag is produced in the same pass so it always
// describes the original input, never the truncated output.
//
// Invalid UTF-8 input and non-positive budgets are rejected with the fixed
// internal error rather than masked by byte replacement.
func TruncateCellText(s string, maxBytes int) (string, bool, error) {
	if maxBytes <= 0 {
		return "", false, errResultContractInvalid
	}
	if !utf8.ValidString(s) {
		return "", false, errResultContractInvalid
	}
	if len(s) <= maxBytes {
		return s, false, nil
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	// Clone isolates the ≤maxBytes prefix from the source string's backing
	// array — without it a truncated cell would pin a potentially multi-MiB
	// scan buffer for the life of the response. The untruncated branch above
	// deliberately stays copy-free.
	return strings.Clone(s[:end]), true, nil
}

// TruncateCellValue applies the cell contract to one materialized value.
// Declared shapes: nil (SQL NULL), string (text incl. bytea-hex/JSON rendered
// text), bool, int64, float64, and time.Time. Only strings can truncate;
// non-text shapes pass through unchanged with flag false so MySQL numeric,
// boolean, and temporal values keep their native JSON type — they are never
// restringified. Any other shape is rejected with the fixed internal error.
func TruncateCellValue(v any, maxBytes int) (any, bool, error) {
	switch x := v.(type) {
	case nil:
		return nil, false, nil
	case string:
		return TruncateCellText(x, maxBytes)
	case bool, int64, float64, time.Time:
		return x, false, nil
	default:
		return nil, false, errResultContractInvalid
	}
}

// TruncateRow applies TruncateCellValue to every value and merges the fresh
// flag with the caller-supplied prior flag per cell: prior[i]=true can never
// be cleared to false by reprocessing an already-truncated value. prior may
// be nil (fresh flags) but must otherwise be exactly len(values). Returns new
// slices; the inputs are never mutated.
func TruncateRow(values []any, prior []bool, maxBytes int) ([]any, []bool, error) {
	if prior != nil && len(prior) != len(values) {
		return nil, nil, errResultContractInvalid
	}
	out := make([]any, len(values))
	flags := make([]bool, len(values))
	for i, v := range values {
		tv, truncated, err := TruncateCellValue(v, maxBytes)
		if err != nil {
			return nil, nil, err
		}
		out[i] = tv
		flags[i] = truncated || (prior != nil && prior[i])
	}
	return out, flags, nil
}

// ---------------------------------------------------------------------------
// 2. Truncation matrix — per-cell flags aligned with the delivered page
// ---------------------------------------------------------------------------

// CloneCellTruncated deep-copies a truncation matrix so adapters never alias
// or mutate the producer's underlying data. nil stays nil (no information).
func CloneCellTruncated(matrix [][]bool) [][]bool {
	if matrix == nil {
		return nil
	}
	out := make([][]bool, len(matrix))
	for i, row := range matrix {
		out[i] = append([]bool(nil), row...)
	}
	return out
}

// AnyCellTruncated reports whether any delivered cell carries the flag.
func AnyCellTruncated(matrix [][]bool) bool {
	for _, row := range matrix {
		for _, t := range row {
			if t {
				return true
			}
		}
	}
	return false
}

// validateCellTruncatedMatrix enforces strict alignment between the public
// page and its truncation evidence. Every public row width is always checked
// against the column count — the matrix being nil never skips that check.
// Evidence rules: a non-empty page must carry a complete aligned matrix (its
// absence is the fixed internal evidence error, never a known-clean page and
// never a client-capability issue); a zero-row page may carry nil or an
// empty matrix, while a residual non-empty matrix is corrupt. There is no
// fabricated all-false fill-in and no trust/skip switch.
func validateCellTruncatedMatrix(columnCount int, rows [][]any, matrix [][]bool) error {
	for i := range rows {
		if len(rows[i]) != columnCount {
			return errResultContractInvalid
		}
	}
	if matrix == nil {
		if len(rows) == 0 {
			return nil
		}
		return errResultContractInvalid
	}
	if len(matrix) != len(rows) {
		return errResultContractInvalid
	}
	for i := range matrix {
		if len(matrix[i]) != columnCount {
			return errResultContractInvalid
		}
	}
	return nil
}

// attachCellTruncated validates the matrix against the page and returns a
// defensive clone ready to assign to a response's CellTruncated field. It is
// the shared seam for QueryExecuteResponse and RelatedRecordNavigationResponse
// so the three governed paths cannot drift on alignment.
func attachCellTruncated(columns []model.QueryResultColumn, rows [][]any, matrix [][]bool) ([][]bool, error) {
	if err := validateCellTruncatedMatrix(len(columns), rows, matrix); err != nil {
		return nil, err
	}
	return CloneCellTruncated(matrix), nil
}

// ---------------------------------------------------------------------------
// 3. Delivery capability gate (post-finalize, shared result boundary)
// ---------------------------------------------------------------------------

// ResultDeliveryInput is the trusted-internal input to the delivery gate. It
// is assembled by the orchestrator after execution, disclosure, and evidence
// persistence — never from request data. UpstreamErr carries the upstream
// execution/governance/evidence outcome; there is no caller-supplied
// "already finalized" flag that could bypass evidence persistence.
type ResultDeliveryInput struct {
	// UpstreamErr is the upstream execution, governance, or evidence-
	// persistence outcome. Non-nil always wins over the capability check so a
	// delivery refusal can never mask a real failure (e.g. the 502 evidence
	// path). The gate does not write history, finalize claims, or retry.
	UpstreamErr error
	// Columns/Rows/CellTruncated describe the final public page actually
	// being delivered: pagination sentinels never delivered must not be
	// counted here — the caller supplies exactly the released rows and their
	// aligned matrix (nil matrix = no per-cell truncation information).
	Columns       []model.QueryResultColumn
	Rows          [][]any
	CellTruncated [][]bool
	// Capabilities is the client's declared result-contract capability set.
	Capabilities []string
}

// ResultDeliveryDecision is the gate's deliverable payload: the page rows and
// their aligned matrix. Refusal returns the zero decision — no usable rows.
type ResultDeliveryDecision struct {
	Rows          [][]any
	CellTruncated [][]bool
}

// DecideResultDelivery applies the G2 capability gate to a finalized public
// page. Ordering contract: execution/disclosure passed and the evidence pair
// is already written — this call happens AFTER finalize. An upstream failure
// (execution, governance, or evidence persistence) is returned unchanged
// before any capability reasoning. A page with at least one truncated cell
// and no exact "cellTruncated" capability is refused with
// ErrResultContractUpgradeRequired and carries no rows; clean pages stay
// deliverable to capability-less clients.
func DecideResultDelivery(in ResultDeliveryInput) (ResultDeliveryDecision, error) {
	if in.UpstreamErr != nil {
		return ResultDeliveryDecision{}, in.UpstreamErr
	}
	if err := validateCellTruncatedMatrix(len(in.Columns), in.Rows, in.CellTruncated); err != nil {
		return ResultDeliveryDecision{}, err
	}
	if AnyCellTruncated(in.CellTruncated) && !hasCellTruncatedCapability(in.Capabilities) {
		return ResultDeliveryDecision{}, ErrResultContractUpgradeRequired
	}
	// The decision owns its payload: outer + per-row slices and both matrix
	// levels are copied so later producer/consumer mutation cannot reach
	// across the boundary. Cell values are declared immutable scalar shapes,
	// so no deeper recursion is needed (or wanted).
	return ResultDeliveryDecision{
		Rows:          cloneRows(in.Rows),
		CellTruncated: CloneCellTruncated(in.CellTruncated),
	}, nil
}

// cloneRows copies the outer slice and each row slice. Elements themselves
// are the declared immutable scalar shapes and are shared by design.
func cloneRows(rows [][]any) [][]any {
	if rows == nil {
		return nil
	}
	out := make([][]any, len(rows))
	for i, r := range rows {
		out[i] = append([]any(nil), r...)
	}
	return out
}

// hasCellTruncatedCapability reports whether the client declared the exact
// cellTruncated token. Unknown tokens never satisfy it; duplicates do not
// change the result.
func hasCellTruncatedCapability(capabilities []string) bool {
	for _, c := range capabilities {
		if c == CapabilityCellTruncated {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 4. CSV export eligibility (no endpoint — shared decision only)
// ---------------------------------------------------------------------------

// DecideCSVExport decides whether the delivered public page may be exported
// as CSV. Refusal is fail-closed:
//   - any actually delivered cell with cellTruncated=true → deny, even when
//     the client declared the capability (declaring it means the client can
//     understand truncated results, not that a truncated page may be
//     exported as if complete);
//   - the truncation matrix is required evidence for a non-empty page —
//     missing or malformed → deny;
//   - no truncation does NOT grant copy permission: every column must carry
//     raw_copy_allowed + copyAllowed; masked_no_copy or blocked columns deny.
func DecideCSVExport(columns []model.QueryResultColumn, rows [][]any, cellTruncated [][]bool) error {
	if len(columns) == 0 {
		return fmt.Errorf("%w: no columns", ErrCSVExportNotPermitted)
	}
	for i, col := range columns {
		if col.DisplayMode != model.ResultDisclosureRawCopyAllowed || !col.CopyAllowed {
			return fmt.Errorf("%w: column %d not copyable", ErrCSVExportNotPermitted, i)
		}
	}
	// The matrix relationship is validated whenever it is provided — a
	// residual matrix on a zero-row page, a nil matrix on a non-empty page,
	// or any misalignment is corrupt evidence, never authorized-clean.
	if err := validateCellTruncatedMatrix(len(columns), rows, cellTruncated); err != nil {
		return fmt.Errorf("%w: missing or corrupt truncation evidence", ErrCSVExportNotPermitted)
	}
	if AnyCellTruncated(cellTruncated) {
		return fmt.Errorf("%w: page contains truncated cells", ErrCSVExportNotPermitted)
	}
	return nil
}
