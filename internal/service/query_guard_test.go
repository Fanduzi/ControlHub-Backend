// Package service provides tests for the read-only query guard.
// input: errors, strings, testing, vitess sqlparser (via query_guard)
// output: TestQueryGuard* (allow/reject/limit/digest cases)
// pos: Unit tests for the AST-backed MySQL/TiDB read-only guard
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"strings"
	"testing"
)

func newTestGuard() *QueryGuard {
	return NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
}

func TestQueryGuardAllowsSimpleSelect(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	got, err := g.Guard("select 1 as value", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	// The SQL LIMIT is effective+1 (101) so the executor can detect truncation;
	// limitApplied reports the real cap (100).
	if !strings.Contains(strings.ToLower(got.ExecutableSQL), "limit 101") {
		t.Fatalf("executable %q must contain the applied limit 101 (cap+1)", got.ExecutableSQL)
	}
	if got.LimitApplied != 100 {
		t.Fatalf("LimitApplied = %d, want 100", got.LimitApplied)
	}
	if !strings.Contains(got.StatementDigest, "?") {
		t.Fatalf("digest %q should mask the literal", got.StatementDigest)
	}
}

func TestQueryGuardRejectsEmptyStatement(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{"", "   ", "\n\t"} {
		if _, err := g.Guard(stmt, 100); !errors.Is(err, ErrQueryStatementEmpty) {
			t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementEmpty", stmt, err)
		}
	}
}

func TestQueryGuardRejectsWriteStatements(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: the sandbox is read-only — any write must be rejected by statement
	// type, never by reaching the database.
	for _, stmt := range []string{
		"insert into users(id) values (1)",
		"update users set name = 'x'",
		"delete from users",
		"replace into users(id) values (1)",
	} {
		_, err := g.Guard(stmt, 100)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

func TestQueryGuardRejectsDDLAndAdminStatements(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{
		"create table t (id int)",
		"drop table t",
		"alter table t add column x int",
		"truncate table t",
		"call proc()",
		"set @x = 1",
		"use db",
		"load data infile '/tmp/x' into table t",
		"begin",
		"commit",
		"grant select on *.* to 'u'@'%'",
	} {
		_, err := g.Guard(stmt, 100)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

func TestQueryGuardRejectsMultiStatements(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: multi-statement input is the classic injection amplifier — the guard
	// must reject any input containing more than one real statement even if each
	// individually would be a SELECT.
	for _, stmt := range []string{
		"select 1; select 2",
		"select 1; drop table t",
	} {
		_, err := g.Guard(stmt, 100)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

func TestQueryGuardAppliesDefaultLimit(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// requestedMaxRows 0 means "unspecified" -> DefaultMaxRows (100).
	got, err := g.Guard("select * from t", 0)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.LimitApplied != 100 {
		t.Fatalf("LimitApplied = %d, want default 100", got.LimitApplied)
	}
	// SQL LIMIT is cap+1 for truncation detection.
	if !strings.Contains(strings.ToLower(got.ExecutableSQL), "limit 101") {
		t.Fatalf("executable %q must contain limit 101 (default cap+1)", got.ExecutableSQL)
	}
}

func TestQueryGuardCapsLargeLimit(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// A requested limit above the hard cap (500) is clamped to 500, and the
	// statement's own larger LIMIT is overwritten by the backend-owned cap.
	got, err := g.Guard("select * from t limit 1000", 1000)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.LimitApplied != 500 {
		t.Fatalf("LimitApplied = %d, want hard cap 500", got.LimitApplied)
	}
	lowered := strings.ToLower(got.ExecutableSQL)
	if !strings.Contains(lowered, "limit 501") {
		t.Fatalf("executable %q must contain clamped limit 501 (hard cap+1)", got.ExecutableSQL)
	}
	if strings.Contains(lowered, "limit 1000") {
		t.Fatalf("executable %q must not retain the oversized limit 1000", got.ExecutableSQL)
	}
}

func TestQueryGuardRejectsNegativeMaxRows(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	if _, err := g.Guard("select 1", -1); !errors.Is(err, ErrQueryLimitInvalid) {
		t.Fatalf("Guard negative maxRows error = %v, want ErrQueryLimitInvalid", err)
	}
}

func TestQueryGuardRejectsSelectIntoOutfile(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: INTO OUTFILE writes query results to a server-side file — a read-only
	// sandbox must never let a SELECT touch the filesystem.
	if _, err := g.Guard("select * from t into outfile '/tmp/x'", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("into outfile error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsSelectIntoDumpfile(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	if _, err := g.Guard("select * from t into dumpfile '/tmp/x'", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("into dumpfile error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsLockingSelect(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: locking clauses hold row/table locks and are not read-only behavior.
	for _, stmt := range []string{
		"select * from t for update",
		"select * from t lock in share mode",
	} {
		if _, err := g.Guard(stmt, 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

func TestQueryGuardRejectsSleepFunction(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SLEEP burns server time / resources and is a DoS primitive.
	if _, err := g.Guard("select sleep(5)", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("sleep error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsBenchmarkFunction(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	if _, err := g.Guard("select benchmark(1000000, md5('x'))", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("benchmark error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsNamedLockFunctions(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: advisory named-lock functions hold cross-connection MySQL locks — a
	// read-only sandbox must never let a query acquire them.
	for _, stmt := range []string{
		"select get_lock('a', 1)",
		"select release_lock('a')",
		"select is_free_lock('a')",
		"select is_used_lock('a')",
	} {
		if _, err := g.Guard(stmt, 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

func TestQueryGuardRejectsLoadFileFunction(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: LOAD_FILE reads arbitrary server-side files.
	if _, err := g.Guard("select load_file('/etc/passwd')", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("load_file error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsUserVariableAssignment(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: user-variable assignment (@var := ...) and SELECT ... INTO @var let a
	// query carry state across the read-only boundary.
	for _, stmt := range []string{
		"select @a := 1",
		"select 1 into @a",
	} {
		if _, err := g.Guard(stmt, 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

func TestQueryGuardDigestMasksLiterals(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	got, err := g.Guard("select id, name from resources where resource_type = 'service' and n = 42", 20)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	// WHY: history must record the query SHAPE, not literal values, so two
	// semantically-identical queries with different parameters collapse and no
	// data leaks into the digest.
	if strings.Contains(got.StatementDigest, "service") {
		t.Fatalf("digest %q leaks string literal 'service'", got.StatementDigest)
	}
	if strings.Contains(got.StatementDigest, "42") {
		t.Fatalf("digest %q leaks numeric literal 42", got.StatementDigest)
	}
	if !strings.Contains(got.StatementDigest, "?") {
		t.Fatalf("digest %q must mask literals as ?", got.StatementDigest)
	}
}

func TestQueryGuardPreviewIsTruncated(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	long := "select 1 -- " + strings.Repeat("x", 700)
	got, err := g.Guard(long, 10)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if len(got.StatementPreview) > 512 {
		t.Fatalf("preview length = %d, want <= 512", len(got.StatementPreview))
	}
}

// --- Phase 38C: read-only metadata statement allow-list ---

func TestQueryGuardAllowsShowTables(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SHOW TABLES is a safe read-only metadata command that users expect
	// in a database query workbench.
	got, err := g.Guard("show tables", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.LimitApplied != 0 {
		t.Fatalf("LimitApplied = %d, want 0 (SHOW statements have no row cap)", got.LimitApplied)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for SHOW TABLES")
	}
}

func TestQueryGuardAllowsShowColumns(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SHOW COLUMNS FROM <table> is a safe metadata introspection command.
	got, err := g.Guard("show columns from query_e2e_items", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for SHOW COLUMNS")
	}
}

func TestQueryGuardAllowsDescribeTable(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: DESCRIBE <table> is equivalent to SHOW COLUMNS and is a standard
	// metadata exploration command.
	got, err := g.Guard("describe query_e2e_items", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for DESCRIBE")
	}
}

func TestQueryGuardAllowsDescTable(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: DESC is a standard shorthand for DESCRIBE.
	got, err := g.Guard("desc query_e2e_items", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for DESC")
	}
}

func TestQueryGuardAllowsExplainSelect(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: EXPLAIN SELECT is a safe read-only command that shows query execution
	// plan without modifying data.
	got, err := g.Guard("explain select * from query_e2e_items", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for EXPLAIN SELECT")
	}
}

// --- Phase 38C: rejection tests for forbidden SHOW/admin statements ---

func TestQueryGuardRejectsShowProcesslist(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SHOW PROCESSLIST exposes all connected sessions and their queries —
	// a read-only sandbox must never leak cross-session visibility.
	if _, err := g.Guard("show processlist", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("show processlist error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsShowDatabases(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SHOW DATABASES exposes all database names on the server — a read-only
	// sandbox must not leak cross-schema visibility.
	if _, err := g.Guard("show databases", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("show databases error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsShowGrants(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SHOW GRANTS exposes privilege information — not appropriate for a
	// read-only query sandbox.
	if _, err := g.Guard("show grants", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("show grants error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsUseDatabase(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: USE changes the session database context — a session mutation that
	// must be rejected in a read-only sandbox.
	if _, err := g.Guard("use mysql", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("use database error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardRejectsSetStatement(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SET modifies session variables — a session mutation that must be
	// rejected in a read-only sandbox.
	if _, err := g.Guard("set sql_safe_updates = 1", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("set statement error = %v, want ErrQueryStatementNotAllowed", err)
	}
}