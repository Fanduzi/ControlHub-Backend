// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: user SQL text via github.com/wasilibs/go-pgquery; pg_query_go/v6 typed AST
// output: RejectError, ErrNotReadOnly/ErrNotWitnessable/ErrAmbiguousScope/ErrLayoutUnfreezable, GuardResult, GuardPG
// pos: G5 read-only dialect guard — single SelectStmt, no INTO/locking/data-modifying CTE, recursive forbidden-function denylist; emits classified RangeVar refs with witnessable-position verdicts; controlled error codes map verbatim to the API layer
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"fmt"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// RejectError is a controlled rejection: safe to surface Code/Message to the
// API layer. It never carries user SQL text beyond a short structural label.
type RejectError struct {
	Code    string // OpenAPI error code, e.g. "query_not_allowed"
	Message string // human-readable reason; never embeds DSNs or secrets
}

func (e *RejectError) Error() string { return e.Message }

// Sentinel categories for errors.Is-style checks in tests and service mapping.
var (
	// ErrNotReadOnly — the guard rejected a statement outside the read-only
	// SELECT contract (wrong statement type, multi-statement, SELECT INTO,
	// locking clause, data-modifying CTE, forbidden function).
	ErrNotReadOnly = errors.New("pgsql: statement is not a read-only SELECT")
	// ErrNotWitnessable — an entity RangeVar sits in a position that cannot
	// propagate an identity witness (inside WHERE/EXISTS/scalar subqueries,
	// set-operation leaves, or as a whole-row composite of a witnessed source).
	ErrNotWitnessable = errors.New("pgsql: entity reference is not in a witnessable position")
	// ErrAmbiguousScope — a name's ownership cannot be determined
	// positionally; the guard refuses to guess (spec: 归属无法确定 → 受控拒绝).
	ErrAmbiguousScope = errors.New("pgsql: reference ownership cannot be determined")
	// ErrLayoutUnfreezable — the visible-column layout cannot be frozen before
	// witness injection (e.g. a form the freezer cannot rewrite exactly).
	ErrLayoutUnfreezable = errors.New("pgsql: visible column layout cannot be frozen")
)

// forbiddenFunctions is the curated denylist (spec G5 plus the lab-vetted
// superset): functions callable inside a SELECT that sleep, mutate
// session/catalog state, touch the filesystem, terminate other backends,
// advance sequences, allocate xids, or open remote connections. Compared on
// the unqualified final name, lowercased.
var forbiddenFunctions = map[string]struct{}{
	"pg_sleep":   {},
	"set_config": {}, // session mutation equivalent of SET
	"nextval":    {}, "setval": {}, "currval": {},
	"pg_terminate_backend": {}, "pg_cancel_backend": {},
	"pg_reload_conf": {}, "pg_rotate_logfile": {}, "pg_create_restore_point": {},
	"pg_switch_wal": {}, "pg_backup_start": {}, "pg_backup_stop": {}, "pg_promote": {},
	"lo_import": {}, "lo_export": {}, "lo_from_bytea": {}, "lo_put": {}, "lo_unlink": {},
	"pg_read_file": {}, "pg_read_binary_file": {}, "pg_stat_file": {},
	"pg_ls_dir": {}, "pg_ls_logdir": {}, "pg_ls_waldir": {}, "pg_logdir_ls": {},
	"pg_ls_archive_statusdir": {}, "pg_ls_tmpdir": {},
	"dblink": {}, "dblink_connect": {}, "dblink_exec": {}, "dblink_send_query": {},
	"pg_advisory_lock": {}, "pg_advisory_lock_shared": {},
	"pg_advisory_xact_lock": {}, "pg_advisory_xact_lock_shared": {},
	"pg_advisory_unlock": {}, "pg_advisory_unlock_all": {},
	"pg_notify":               {}, // LISTEN/NOTIFY channel traffic — side effect
	"setseed":                 {}, // session RNG state mutation
	"pg_stat_get_backend_pid": {}, // PID discovery feeding pg_cancel/terminate
	"pg_column_size":          {}, // large object-ish size oracle — conservative
	"query_to_xml":            {}, "database_to_xml": {}, "schema_to_xml": {},
	"table_to_xml": {}, // serialize arbitrary results incl. other schemas
	"txid_current": {}, // allocates a real xid — not read-only in WAL terms
}

// GuardResult is the guard's output: the parsed tree plus everything the
// downstream stages need — classified RangeVar references (with byte offsets
// and witnessable-position verdicts) in source order.
type GuardResult struct {
	Tree *pg.ParseResult
	Stmt *pg.SelectStmt // the single top-level SELECT
	Refs []RangeVarRef  // entity + CTE references in statement order
}

// GuardPG parses and validates one user statement under the read-only
// contract, then classifies every RangeVar. It is pure analysis — no database
// access, no mutation.
func GuardPG(statement string) (*GuardResult, error) {
	if strings.TrimSpace(statement) == "" {
		return nil, &RejectError{Code: "query_not_allowed", Message: "statement is empty"}
	}
	// Parse the statement verbatim — recorded RangeVar offsets index into the
	// caller's exact bytes for the canonical rewrite; trimming here would
	// shift every offset.
	tree, err := pgquery.Parse(statement)
	if err != nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v", ErrNotReadOnly)}
	}
	if len(tree.Stmts) != 1 {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: multi-statement input", ErrNotReadOnly)}
	}
	sel := tree.Stmts[0].Stmt.GetSelectStmt()
	if sel == nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: statement type is not SELECT", ErrNotReadOnly)}
	}
	if err := checkForbidden(tree); err != nil {
		return nil, err
	}
	refs, err := collectRangeVarRefs(tree)
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		// RefQualified is still an entity reference — a schema prefix must
		// not bypass the witnessable-position rule (it only changes which
		// name the binding gate resolves).
		if r.Kind != RefCTE && !r.Witnessable {
			return nil, &RejectError{Code: "query_reference_not_witnessable", Message: fmt.Sprintf("%v", ErrNotWitnessable)}
		}
	}
	return &GuardResult{Tree: tree, Stmt: sel, Refs: refs}, nil
}

// checkForbidden walks the whole tree for forbidden constructs.
func checkForbidden(tree *pg.ParseResult) error {
	var found *RejectError
	walkTree(tree.ProtoReflect(), func(ctx *walkCtx, m protoreflect.Message) bool {
		if found != nil {
			return false
		}
		switch n := m.Interface().(type) {
		case *pg.SelectStmt:
			if n.GetIntoClause() != nil {
				found = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: SELECT INTO", ErrNotReadOnly)}
				return false
			}
			if len(n.GetLockingClause()) > 0 {
				found = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: locking clause", ErrNotReadOnly)}
				return false
			}
		case *pg.CommonTableExpr:
			if n.GetCtequery().GetSelectStmt() == nil {
				found = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: data-modifying CTE", ErrNotReadOnly)}
				return false
			}
		case *pg.FuncCall:
			name := funcName(n)
			if _, bad := forbiddenFunctions[name]; bad {
				found = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: forbidden function", ErrNotReadOnly)}
				return false
			}
		}
		return true
	})
	if found == nil {
		return nil
	}
	return found
}

// funcName extracts the unqualified final segment of a possibly
// schema-qualified function name, lowercased — denylist matching is
// name-based, so a schema prefix cannot smuggle a forbidden call.
func funcName(fc *pg.FuncCall) string {
	parts := fc.GetFuncname()
	if len(parts) == 0 {
		return ""
	}
	if s := parts[len(parts)-1].GetString_(); s != nil {
		return strings.ToLower(s.GetSval())
	}
	return ""
}
