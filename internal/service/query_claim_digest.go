// Package service computes the canonical request digests for the G9
// execution-claim protocol.
// input: crypto/sha256, encoding/binary, sort, strconv, string values already
// resolved by the typed service entry (never raw client JSON)
// output: DigestOrdinaryExecute, DigestSavedStatement, DigestRelatedRecords,
// and the typed digest input structs
// pos: T10-C claim admission digest (issue #118, frozen G9 request_digest
// contract) — one deterministic v1 length-prefixed encoding shared by the
// three typed entries so the same semantic request always occupies the same
// claim digest and different requests never collide; clientExecutionId and
// capabilities never reach the byte stream
// note: if this file changes, update this header and module README.md.
package service

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
)

// Claim-digest wire encoding v1. Every element is framed so byte streams can
// never collide at field or list boundaries:
//   - entry tag:   one uvarint length + tag bytes, always element 0;
//   - string:      uvarint(byte length) + verbatim UTF-8 bytes — statements
//     keep original bytes (no trim, no case fold);
//   - integer:     uvarint(unsigned) — page, pageSize, maxRows, statementId
//     are resolved non-negative ints;
//   - typed value: one kind byte + the canonical payload below;
//   - list:        uvarint(element count) + elements in wire order.
//
// Typed-value kind bytes make "5" the string distinct from 5 the integer,
// "" distinct from absent, and the text "null" distinct from a typed NULL.
// 'n' carries no payload; 'i' re-encodes int64 as its canonical decimal
// text; 'd' carries the validated decimal literal (never float64); 'b' is
// "0"/"1"; 's' is verbatim UTF-8.
const (
	claimDigestTagExecute    = "execute"
	claimDigestTagSavedStmt  = "saved-statement"
	claimDigestTagNavigation = "related-records"

	claimDigestKindNull    = 'n'
	claimDigestKindString  = 's'
	claimDigestKindDecimal = 'd'
	claimDigestKindInteger = 'i'
	claimDigestKindBoolean = 'b'
)

// ClaimDigestNamedValue is one saved-statement parameter after typed
// validation: the declared name plus its typed canonical text. Sorting by
// name happens inside the digest function — callers pass the map iteration
// order.
type ClaimDigestNamedValue struct {
	Name string
	Kind byte   // one of the claimDigestKind* tags
	Text string // canonical text payload; ignored for Kind 'n'
}

// ClaimDigestValue constructors keep the kind vocabulary closed.
func ClaimDigestNull() ClaimDigestNamedValue { return ClaimDigestNamedValue{Kind: claimDigestKindNull} }
func ClaimDigestString(v string) ClaimDigestNamedValue {
	return ClaimDigestNamedValue{Kind: claimDigestKindString, Text: v}
}
func ClaimDigestDecimal(v string) ClaimDigestNamedValue {
	return ClaimDigestNamedValue{Kind: claimDigestKindDecimal, Text: v}
}
func ClaimDigestInteger(v int64) ClaimDigestNamedValue {
	return ClaimDigestNamedValue{Kind: claimDigestKindInteger, Text: fmt.Sprintf("%d", v)}
}
func ClaimDigestBoolean(v bool) ClaimDigestNamedValue {
	if v {
		return ClaimDigestNamedValue{Kind: claimDigestKindBoolean, Text: "1"}
	}
	return ClaimDigestNamedValue{Kind: claimDigestKindBoolean, Text: "0"}
}

// OrdinaryExecuteDigestInput is the resolved admission view of one ordinary
// execute request — fields are the server-resolved values (defaults already
// applied), not the wire request. Statement bytes are preserved verbatim.
type OrdinaryExecuteDigestInput struct {
	Database  string
	Schema    string
	Statement string
	Page      int
	PageSize  int
	MaxRows   int
}

// SavedStatementDigestInput is the resolved admission view of one
// saved-statement execution. Values are the typed parameter name/value pairs
// after validation; they are sorted by name inside the digest so map or call
// order cannot affect the fingerprint. Parameter values influence only the
// digest — they are never persisted, logged, audited, or returned.
type SavedStatementDigestInput struct {
	Database    string
	Schema      string
	StatementID uint64
	Values      []ClaimDigestNamedValue
	Page        int
	PageSize    int
	MaxRows     int
}

// RelatedRecordsDigestInput is the resolved admission view of one
// related-record navigation: the trusted source identity, the
// relation/direction marker, and the ordered locator values. Values order is
// significant — [a,b] and [b,a] are different requests.
type RelatedRecordsDigestInput struct {
	Database      string
	Schema        string
	SourceSchema  string
	SourceObject  string
	SourceColumns []string // ordered local column group of the relation
	ForeignKey    string   // relation/direction identity
	LocalValues   []string // ordered locator values
	Page          int
	PageSize      int
	MaxRows       int
}

// claimDigestWriter appends framed elements to the digest byte stream.
type claimDigestWriter struct{ buf []byte }

func (w *claimDigestWriter) str(s string) {
	var ln [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(ln[:], uint64(len(s)))
	w.buf = append(w.buf, ln[:n]...)
	w.buf = append(w.buf, s...)
}

func (w *claimDigestWriter) num(v uint64) {
	var ln [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(ln[:], uint64(v))
	w.buf = append(w.buf, ln[:n]...)
}

func (w *claimDigestWriter) list(n int) { w.num(uint64(n)) }

func (w *claimDigestWriter) value(v ClaimDigestNamedValue) {
	w.buf = append(w.buf, v.Kind)
	if v.Kind != claimDigestKindNull {
		w.str(v.Text)
	}
}

func (w *claimDigestWriter) sum() string {
	h := sha256.Sum256(w.buf)
	return fmt.Sprintf("%x", h)
}

// DigestOrdinaryExecute encodes the frozen ordinary-execute field order:
// database, schema, statement (original bytes), page, pageSize, maxRows.
func DigestOrdinaryExecute(in OrdinaryExecuteDigestInput) string {
	w := &claimDigestWriter{}
	w.str(claimDigestTagExecute)
	w.str(in.Database)
	w.str(in.Schema)
	w.str(in.Statement)
	w.num(uint64(in.Page))
	w.num(uint64(in.PageSize))
	w.num(uint64(in.MaxRows))
	return w.sum()
}

// DigestSavedStatement encodes the frozen saved-statement field order:
// database, schema, statementId, name/value pairs sorted by name, page,
// pageSize, maxRows. Sorting is by name bytes; duplicate names keep their
// relative value order so (a=1,a=2) and (a=2,a=1) remain distinct.
func DigestSavedStatement(in SavedStatementDigestInput) string {
	w := &claimDigestWriter{}
	w.str(claimDigestTagSavedStmt)
	w.str(in.Database)
	w.str(in.Schema)
	w.num(in.StatementID)
	vals := append([]ClaimDigestNamedValue(nil), in.Values...)
	sort.SliceStable(vals, func(i, j int) bool { return vals[i].Name < vals[j].Name })
	w.list(len(vals))
	for _, v := range vals {
		w.str(v.Name)
		w.value(v)
	}
	w.num(uint64(in.Page))
	w.num(uint64(in.PageSize))
	w.num(uint64(in.MaxRows))
	return w.sum()
}

// DigestRelatedRecords encodes the frozen related-records field order:
// database, schema, source{schema, object, ordered column group},
// relation/direction marker, ordered locator values, page, pageSize, maxRows.
func DigestRelatedRecords(in RelatedRecordsDigestInput) string {
	w := &claimDigestWriter{}
	w.str(claimDigestTagNavigation)
	w.str(in.Database)
	w.str(in.Schema)
	w.str(in.SourceSchema)
	w.str(in.SourceObject)
	w.list(len(in.SourceColumns))
	for _, c := range in.SourceColumns {
		w.str(c)
	}
	w.str(in.ForeignKey)
	w.list(len(in.LocalValues))
	for _, v := range in.LocalValues {
		w.str(v)
	}
	w.num(uint64(in.Page))
	w.num(uint64(in.PageSize))
	w.num(uint64(in.MaxRows))
	return w.sum()
}
