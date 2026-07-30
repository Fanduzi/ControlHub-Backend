// Package service provides tests for the read-only query guard.
// input: errors, strings, testing, vitess sqlparser (via query_guard)
// output: TestQueryGuard* (allow/reject/limit/digest cases)
// pos: Unit tests for the AST-backed MySQL/TiDB read-only guard
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"math"
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
	// CRITICAL: ExecutableSQL must remain an EXPLAIN statement, not degrade to
	// a bare SELECT that returns business data.
	if !strings.HasPrefix(strings.ToLower(got.ExecutableSQL), "explain") {
		t.Fatalf("ExecutableSQL %q must start with EXPLAIN, not bare SELECT", got.ExecutableSQL)
	}
}

func TestQueryGuardExplainRejectsLockingInnerSelect(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: the inner SELECT of an EXPLAIN must still pass the full side-effect
	// guard — locking clauses are not read-only.
	if _, err := g.Guard("explain select * from t for update", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("explain+for update error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardExplainRejectsSleepInnerSelect(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: side-effect functions in the inner SELECT must be rejected even under EXPLAIN.
	if _, err := g.Guard("explain select sleep(1)", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("explain+sleep error = %v, want ErrQueryStatementNotAllowed", err)
	}
}

func TestQueryGuardExplainRejectsIntoOutfileInnerSelect(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: INTO OUTFILE in the inner SELECT must be rejected even under EXPLAIN.
	if _, err := g.Guard("explain select * from t into outfile '/tmp/x'", 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
		t.Fatalf("explain+into outfile error = %v, want ErrQueryStatementNotAllowed", err)
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

func TestQueryGuardAllowsShowDatabases(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SHOW DATABASES is a safe read-only metadata command. The read-only
	// credential controls what databases are actually accessible; the guard's
	// job is to reject writes, session mutation, and privilege commands.
	got, err := g.Guard("show databases", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.LimitApplied != 0 {
		t.Fatalf("LimitApplied = %d, want 0 (SHOW statements have no row cap)", got.LimitApplied)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for SHOW DATABASES")
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

// --- Phase 38D: cross-schema metadata exploration is now allowed ---

func TestQueryGuardAllowsShowTablesFromSchema(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SHOW TABLES FROM <db> is a safe read-only metadata command. The
	// read-only credential controls what schemas are actually accessible.
	got, err := g.Guard("show tables from query_e2e", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for SHOW TABLES FROM")
	}
}

func TestQueryGuardAllowsShowColumnsFromQualifiedTable(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: SHOW COLUMNS FROM <db>.<table> is a safe metadata introspection
	// command. The credential controls actual schema access.
	got, err := g.Guard("show columns from query_e2e.items", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for SHOW COLUMNS FROM <db>.<table>")
	}
}

func TestQueryGuardAllowsDescribeQualifiedTable(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: DESCRIBE <db>.<table> is a safe metadata introspection command.
	// The credential controls actual schema access.
	got, err := g.Guard("describe query_e2e.items", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for DESCRIBE <db>.<table>")
	}
}

func TestQueryGuardAllowsDescQualifiedTable(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	// WHY: DESC <db>.<table> is shorthand for DESCRIBE — same safe metadata
	// introspection.
	got, err := g.Guard("desc query_e2e.items", 100)
	if err != nil {
		t.Fatalf("Guard error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty for DESC <db>.<table>")
	}
}

// TestGuardExplainSelectAcceptsSimpleSelect proves the narrow Explain guard
// entry accepts a bare parser-approved SELECT. WHY: the Explain route must
// accept only a bare SELECT — never user-typed EXPLAIN/SHOW/DESCRIBE.
func TestGuardExplainSelectAcceptsSimpleSelect(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	got, err := g.GuardExplainSelect("select 1 as value")
	if err != nil {
		t.Fatalf("GuardExplainSelect error: %v", err)
	}
	if got.ExecutableSQL == "" {
		t.Fatal("ExecutableSQL must not be empty")
	}
	if !strings.Contains(strings.ToLower(got.ExecutableSQL), "select") {
		t.Fatalf("ExecutableSQL must contain the select: %q", got.ExecutableSQL)
	}
	// WHY: Explain must reflect the user's actual plan shape. LIMIT injection
	// would mask high_estimated_rows. The narrow entry must NOT inject LIMIT.
	if strings.Contains(strings.ToLower(got.ExecutableSQL), "limit") {
		t.Fatalf("Explain guard must NOT inject LIMIT (would mask plan shape): %q", got.ExecutableSQL)
	}
	if got.LimitApplied != 0 {
		t.Fatalf("LimitApplied = %d, want 0 (Explain never injects LIMIT)", got.LimitApplied)
	}
}

// TestGuardExplainSelectRejectsEmptyStatement mirrors the execute guard's
// empty-statement rejection.
func TestGuardExplainSelectRejectsEmptyStatement(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{"", "   ", "\n\t"} {
		_, err := g.GuardExplainSelect(stmt)
		if !errors.Is(err, ErrQueryStatementEmpty) {
			t.Errorf("GuardExplainSelect(%q) error = %v, want ErrQueryStatementEmpty", stmt, err)
		}
	}
}

// TestGuardExplainSelectRejectsTypedExplain proves user-typed EXPLAIN is
// rejected on the Explain route. WHY: the spec forbids the browser from
// constructing EXPLAIN; the backend owns the wrapper. The execute route
// accepts typed EXPLAIN for historical reasons, but that must NOT authorize
// the same syntax on the Explain route.
func TestGuardExplainSelectRejectsTypedExplain(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{
		"explain select 1",
		"explain format = json select 1",
		"EXPLAIN SELECT * FROM t",
	} {
		_, err := g.GuardExplainSelect(stmt)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Errorf("GuardExplainSelect(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

// TestGuardExplainSelectRejectsShowAndDescribe proves SHOW and DESCRIBE are
// rejected on the Explain route. WHY: the execute route accepts these as
// metadata commands, but Explain is SELECT-only by spec.
func TestGuardExplainSelectRejectsShowAndDescribe(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{
		"show databases",
		"show tables",
		"show columns from t",
		"describe t",
		"desc t",
	} {
		_, err := g.GuardExplainSelect(stmt)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Errorf("GuardExplainSelect(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

// TestGuardExplainSelectRejectsWriteStatements proves DML/DDL/admin statements
// are rejected.
func TestGuardExplainSelectRejectsWriteStatements(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{
		"insert into t values (1)",
		"update t set a = 1",
		"delete from t",
		"create table t (id int)",
		"drop table t",
		"alter table t add column c int",
		"truncate table t",
		"rename table t to u",
		"grant select on *.* to 'x'@'%'",
		"revoke all from 'x'@'%'",
		"set @x = 1",
		"call proc()",
	} {
		_, err := g.GuardExplainSelect(stmt)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Errorf("GuardExplainSelect(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

// TestGuardExplainSelectRejectsMultiStatement proves multi-statement input is
// rejected before parsing. WHY: the classic "; drop table" amplifier must
// never reach the executor.
func TestGuardExplainSelectRejectsMultiStatement(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{
		"select 1; drop table t",
		"select 1; select 2",
		"select 1 -- comment\ndrop table t",
	} {
		_, err := g.GuardExplainSelect(stmt)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Errorf("GuardExplainSelect(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

// TestGuardExplainSelectRejectsUnsafeFunctions proves the AST walker catches
// SLEEP, BENCHMARK, LOAD_FILE, named-lock functions, and user-variable
// assignment. WHY: these are resource/file/lock side-effects that break the
// read-only contract even inside a SELECT.
func TestGuardExplainSelectRejectsUnsafeFunctions(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{
		"select sleep(1)",
		"select benchmark(1000, md5('x'))",
		"select load_file('/etc/passwd')",
		"select get_lock('x', 1)",
		"select @x := 1",
	} {
		_, err := g.GuardExplainSelect(stmt)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Errorf("GuardExplainSelect(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

// TestGuardExplainSelectRejectsIntoAndLocking proves INTO OUTFILE/DUMPFILE and
// FOR UPDATE / LOCK IN SHARE MODE are rejected. WHY: these write server-side
// files or acquire locks, breaking read-only.
func TestGuardExplainSelectRejectsIntoAndLocking(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{
		"select * from t into outfile '/tmp/x'",
		"select * from t into dumpfile '/tmp/x'",
		"select * from t for update",
		"select * from t lock in share mode",
	} {
		_, err := g.GuardExplainSelect(stmt)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Errorf("GuardExplainSelect(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

// TestGuardExplainSelectRejectsMalformedSQL proves a parse error maps to
// ErrQueryStatementNotAllowed, not a 500.
func TestGuardExplainSelectRejectsMalformedSQL(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	for _, stmt := range []string{
		"select from",
		"select * from",
		"select )(",
		"select 1 where",
	} {
		_, err := g.GuardExplainSelect(stmt)
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Errorf("GuardExplainSelect(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
		}
	}
}

// TestGuardExplainSelectPreservesUserPlan proves the guarded ExecutableSQL is
// the bare SELECT (not prefixed with EXPLAIN). WHY: the executor owns
// prepending EXPLAIN FORMAT=JSON; the guard returns only the parser-approved
// SELECT. This is the structural seam that prevents the executor from ever
// seeing arbitrary SQL.
func TestGuardExplainSelectPreservesUserPlan(t *testing.T) {
	t.Parallel()
	g := newTestGuard()
	got, err := g.GuardExplainSelect("select * from t where id = 1")
	if err != nil {
		t.Fatalf("GuardExplainSelect error: %v", err)
	}
	lower := strings.ToLower(got.ExecutableSQL)
	if !strings.HasPrefix(lower, "select") {
		t.Fatalf("ExecutableSQL must start with select (no EXPLAIN prefix): %q", got.ExecutableSQL)
	}
	if strings.Contains(lower, "explain") {
		t.Fatalf("ExecutableSQL must NOT contain explain (executor owns the prefix): %q", got.ExecutableSQL)
	}
	// The digest masks the literal 1.
	if !strings.Contains(got.StatementDigest, "?") {
		t.Fatalf("digest %q should mask the literal 1", got.StatementDigest)
	}
}

// --- Phase 38R: GuardSavedStatement for the save-route entry point ---

func TestGuardSavedStatement(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	tests := []struct {
		name      string
		statement string
		wantErr   bool
	}{
		// Allowed
		{"simple select", "SELECT 1", false},
		{"select with from", "SELECT id FROM orders", false},
		{"select with where", "SELECT id FROM orders WHERE id > 10", false},
		{"select with join", "SELECT o.id FROM orders o JOIN customers c ON o.customer_id = c.id", false},
		{"select with subquery", "SELECT id FROM orders WHERE customer_id IN (SELECT id FROM customers)", false},

		// Rejected: empty
		{"empty string", "", true},
		{"whitespace only", "   ", true},

		// Rejected: non-SELECT
		{"show databases", "SHOW DATABASES", true},
		{"show tables", "SHOW TABLES", true},
		{"describe table", "DESCRIBE orders", true},
		{"desc table", "DESC orders", true},
		{"explain select", "EXPLAIN SELECT 1", true},
		{"insert", "INSERT INTO orders (id) VALUES (1)", true},
		{"update", "UPDATE orders SET id = 1", true},
		{"delete", "DELETE FROM orders", true},
		{"create table", "CREATE TABLE test (id INT)", true},
		{"drop table", "DROP TABLE orders", true},
		{"alter table", "ALTER TABLE orders ADD COLUMN test INT", true},

		// Rejected: multi-statement
		{"two selects", "SELECT 1; SELECT 2", true},
		{"select with trailing semicolon", "SELECT 1;", false},

		// Rejected: unsafe functions
		{"sleep function", "SELECT SLEEP(5)", true},
		{"benchmark function", "SELECT BENCHMARK(1000000, SHA2('test', 256))", true},
		{"load_file function", "SELECT LOAD_FILE('/etc/passwd')", true},

		// Rejected: locking
		{"for update", "SELECT id FROM orders FOR UPDATE", true},
		{"lock in share mode", "SELECT id FROM orders LOCK IN SHARE MODE", true},

		// Rejected: into
		{"into outfile", "SELECT id INTO OUTFILE '/tmp/test.txt' FROM orders", true},
		{"into dumpfile", "SELECT id INTO DUMPFILE '/tmp/test.txt' FROM orders", true},

		// Rejected: user variable assignment
		{"user variable", "SELECT @var := 1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := g.GuardSavedStatement(tt.statement)
			if (err != nil) != tt.wantErr {
				t.Errorf("GuardSavedStatement(%q) error = %v, wantErr %v", tt.statement, err, tt.wantErr)
				return
			}
			if !tt.wantErr && result == "" {
				t.Errorf("GuardSavedStatement(%q) returned empty result", tt.statement)
			}
		})
	}
}

func TestGuardSavedStatementPreservesOriginalText(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	input := "SELECT id FROM orders"
	result, err := g.GuardSavedStatement(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != input {
		t.Errorf("expected %q, got %q", input, result)
	}
}

func TestGuardUnchanged(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	// Guard should still accept SHOW, DESCRIBE, EXPLAIN
	tests := []struct {
		name      string
		statement string
		wantErr   bool
	}{
		{"select", "SELECT 1", false},
		{"show databases", "SHOW DATABASES", false},
		{"show tables", "SHOW TABLES", false},
		{"describe", "DESCRIBE orders", false},
		{"explain select", "EXPLAIN SELECT 1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := g.Guard(tt.statement, 100)
			if (err != nil) != tt.wantErr {
				t.Errorf("Guard(%q) error = %v, wantErr %v", tt.statement, err, tt.wantErr)
			}
		})
	}
}

// --- Phase 38S: governed query-result paging contract ---

func TestQueryGuard_GuardPaginatedSelect_serializesBackendOwnedWindow(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	// Given a SELECT whose second page has 25 rows and a 100-row governed cap.
	// When the page window is guarded.
	got, err := g.GuardPaginatedSelect("select * from t", 2, 25, 100)

	// Then Vitess serializes the AST-owned OFFSET as MySQL's LIMIT offset, count
	// form. The SQL reads one extra row so the executor can detect a next page.
	if err != nil {
		t.Fatalf("GuardPaginatedSelect error: %v", err)
	}
	if got.ExecutableSQL != "select * from t limit 25, 26" {
		t.Fatalf("ExecutableSQL = %q, want exact AST serialization", got.ExecutableSQL)
	}
	if got.LimitApplied != 100 {
		t.Fatalf("LimitApplied = %d, want overall cap 100", got.LimitApplied)
	}
	if got.ResultLimit != 25 {
		t.Fatalf("ResultLimit = %d, want page scan limit 25", got.ResultLimit)
	}
}

func TestQueryGuard_GuardPaginatedSelect_overridesClientLimitAndOffset(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	// Given client-controlled pagination clauses.
	// When the server guards page three with a 10-row page size.
	got, err := g.GuardPaginatedSelect("select id from t limit 1000 offset 500", 3, 10, 100)

	// Then neither client value survives the AST rewrite.
	if err != nil {
		t.Fatalf("GuardPaginatedSelect error: %v", err)
	}
	if got.ExecutableSQL != "select id from t limit 20, 11" {
		t.Fatalf("ExecutableSQL = %q, want server-owned limit and offset", got.ExecutableSQL)
	}
}

func TestQueryGuard_GuardPaginatedSelect_returnsFallbackForMetadata(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	for _, statement := range []string{
		"show tables",
		"describe t",
		"explain select * from t",
	} {
		// Given a metadata statement.
		// When the paging guard receives it.
		_, err := g.GuardPaginatedSelect(statement, 1, 10, 100)

		// Then the service can fall back to the normal guard without injecting a page window.
		if !errors.Is(err, ErrQueryPaginationNotApplicable) {
			t.Errorf("GuardPaginatedSelect(%q) error = %v, want ErrQueryPaginationNotApplicable", statement, err)
		}
	}
}

func TestQueryGuard_GuardPaginatedSelect_rejectsPageOutsideGovernedCap(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	// Given a page starting at the governed cap boundary.
	// When the request is guarded.
	_, err := g.GuardPaginatedSelect("select * from t", 11, 10, 100)

	// Then it is rejected before SQL generation.
	if !errors.Is(err, ErrQueryPaginationInvalid) {
		t.Fatalf("GuardPaginatedSelect error = %v, want ErrQueryPaginationInvalid", err)
	}
}

func TestQueryGuard_GuardPaginatedSelect_rejectsOffsetOverflow(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	// Given page and page size whose offset multiplication overflows int.
	// When the request is guarded.
	_, err := g.GuardPaginatedSelect("select * from t", math.MaxInt, 2, math.MaxInt)

	// Then the request is rejected rather than wrapping its OFFSET.
	if !errors.Is(err, ErrQueryPaginationInvalid) {
		t.Fatalf("GuardPaginatedSelect overflow error = %v, want ErrQueryPaginationInvalid", err)
	}
}

func TestQueryGuard_GuardPaginatedSelect_clampsRequestedMaxRowsToHardCap(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	// Given a paged request whose overall cap far exceeds HardMaxRows (500).
	// When page one is guarded.
	got, err := g.GuardPaginatedSelect("select * from t", 1, 10, 2_000_000_000)

	// Then the governed cap is the guard's hard cap, exactly as for non-paged
	// execution — paging must never widen the absolute row boundary.
	if err != nil {
		t.Fatalf("GuardPaginatedSelect error: %v", err)
	}
	if got.LimitApplied != 500 {
		t.Fatalf("LimitApplied = %d, want hard cap 500", got.LimitApplied)
	}

	// And a page whose offset reaches the hard cap is rejected before SQL
	// generation, so deep paging cannot walk past the governed boundary.
	if _, err := g.GuardPaginatedSelect("select * from t", 51, 10, 2_000_000_000); !errors.Is(err, ErrQueryPaginationInvalid) {
		t.Fatalf("beyond-hard-cap page error = %v, want ErrQueryPaginationInvalid", err)
	}
}

func TestQueryGuard_GuardPaginatedSelect_zeroMaxRowsUsesGuardDefault(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	// Given a paged request that omits an overall cap (requestedMaxRows 0).
	// When page two is guarded.
	got, err := g.GuardPaginatedSelect("select * from t", 2, 10, 0)

	// Then the guard applies the same DefaultMaxRows as non-paged execution,
	// so a default worksheet can actually page within the default cap.
	if err != nil {
		t.Fatalf("GuardPaginatedSelect error: %v", err)
	}
	if got.LimitApplied != 100 {
		t.Fatalf("LimitApplied = %d, want DefaultMaxRows 100", got.LimitApplied)
	}
	if got.ExecutableSQL != "select * from t limit 10, 11" {
		t.Fatalf("ExecutableSQL = %q, want second default page window", got.ExecutableSQL)
	}
}

func TestQueryGuard_GuardPaginatedSelect_negativeMaxRowsIsLimitError(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	// Given a paged request with a negative overall cap.
	// When the paginated guard receives it.
	_, err := g.GuardPaginatedSelect("select 1", 1, 10, -1)

	// Then the negative cap is classified by the same limit validation as
	// non-paged execution, never as an invalid pagination window.
	if !errors.Is(err, ErrQueryLimitInvalid) {
		t.Fatalf("GuardPaginatedSelect negative maxRows error = %v, want ErrQueryLimitInvalid", err)
	}
	if errors.Is(err, ErrQueryPaginationInvalid) {
		t.Fatalf("negative maxRows must not be reported as pagination error: %v", err)
	}
}

func TestQueryGuard_GuardPaginatedSelect_preservesSelectSafetyChecks(t *testing.T) {
	t.Parallel()
	g := newTestGuard()

	for _, statement := range []string{
		"select * from t; select * from u",
		"select * from t into outfile '/tmp/x'",
		"select * from t for update",
		"select sleep(1)",
		"select @value := 1",
	} {
		// Given a SELECT that violates the read-only contract.
		// When it is sent through the paginated entry point.
		_, err := g.GuardPaginatedSelect(statement, 1, 10, 100)

		// Then the same AST safety policy rejects it.
		if !errors.Is(err, ErrQueryStatementNotAllowed) {
			t.Errorf("GuardPaginatedSelect(%q) error = %v, want ErrQueryStatementNotAllowed", statement, err)
		}
	}
}
