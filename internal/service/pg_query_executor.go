// Package service runs the governed PostgreSQL execution attempt end to end.
// input: attempt context with deadline, T2-validated *pgx.ConnConfig, resolved database/pinned schema, user statement text, optional private trusted parameter plan, optional requirePinnedSchema (saved/navigation entries prove the pinned schema even for constant/fully-qualified statements), pgsql.PageOptions, timeout
// output: ExecutePGGoverned, ExecutePGGovernedOutcome, PGExecuteInput, PGVerifiedResult (incl. FetchCount, the executed-window scalar the pagination window actually rewrote — T12-R1), PGResultColumn, errPGExecInternal — verified result plus per-attempt PGAttemptEvidence (stage/status/backend identity/ remote_state/sql-sent facts) or the frozen classified error
// pos: T7-S3/S4 internal execution closure (frozen G4 steps 1–10, G10 5b/5c) — guard + plan snapshot → dense $k ParamRef contract → pool → BEGIN READ ONLY + G4 → virtualxid/identity → PGBind → rewrite → pre-wire expected-record validation → T9-B disclosure preflight → R1-1 parameter-provenance preflight (Parse/Describe + fixed probe) → result ExecParams fresh parse → bounded materialize → FD verify → lock audit → ROLLBACK + remote observation → teardown → deliver. UserSQLSent ORs the provenance describe's own transmission fact into the result's send bit
// note: if this file changes, update this header and module README.md.

package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/controlhub/internal/pgsql"
)

// errPGExecInternal is the fixed internal failure for the governed executor.
// Driver text, DSNs, SQL text, and cell values never surface. It stays
// errors.Is-compatible with ErrQueryBackendFailure.
var errPGExecInternal = fmt.Errorf("%w: pg governed executor", ErrQueryBackendFailure)

const (
	// pgExecCleanupWindow bounds the detached cleanup context used for
	// ROLLBACK and resource release — short, independent of the caller's
	// already-spent context.
	pgExecCleanupWindow = 2 * time.Second

	// pgMaxResultColumns and pgMaxResultBytes bound the attempt's private
	// result buffer. They match the shared spec caps the MySQL executor
	// already enforces (100 columns, 1 MiB total payload); the later T8
	// result contract applies its own per-cell truncation downstream.
	pgMaxResultColumns = 100
	pgMaxResultBytes   = 1 << 20

	// The public column cap is a user-output bound — internal witness
	// overhead is not user output. The transport still carries its own
	// explainable bound: public cap plus a fixed witness allowance, so a
	// 100-column public result with witnesses is legal while a pathological
	// record set cannot materialize unbounded transport width.
	pgMaxWitnessColumns   = 128
	pgMaxTransportColumns = pgMaxResultColumns + pgMaxWitnessColumns
)

// PGExecuteInput bundles the caller-resolved governed execution inputs. The
// caller supplies the T2-validated connection config and the resolved
// database/pinned schema — this entry never re-parses DSNs, never reads
// environment for identity, and never derives a default schema.
type PGExecuteInput struct {
	ConnConfig   *pgx.ConnConfig
	Database     string
	PinnedSchema string
	Statement    string
	Page         pgsql.PageOptions
	Timeout      time.Duration // caller-resolved attempt budget (non-prod 5s / prod 3s)
	// parameters is the optional private trusted parameter plan — produced
	// only through the closed-source producers (template compiler and
	// navigation builder). Nil keeps the ordinary unbound path where
	// no ParamRef may exist in the statement at all.
	parameters *pgParameterPlan
	// navigation is the optional prepared related-record FK binding —
	// non-nil only for the navigation entry kind, alongside a
	// navigation-sourced plan whose locator count matches the pair list.
	// The attempt snapshots it independently and the binding gate re-proves
	// the entire mapping under held locks before any user SQL.
	navigation *pgNavigationBinding
	// requirePinnedSchema strengthens the binding gate for the saved/
	// navigation entries: the pinned schema's existence and USAGE are
	// proven inside the real transaction even when every reference is
	// already qualified or the statement touches no relation at all — a
	// saved statement pins its execution context by contract, not by
	// incidental reference. The ordinary entry leaves this unset.
	requirePinnedSchema bool
}

// PGResultColumn is one delivered public column: G7 type identity plus the
// FD-level origin metadata later disclosure/policy stages consume.
type PGResultColumn struct {
	Name         string
	DatabaseType string // pg type name via the connection TypeMap, or a stable unrecognized marker
	DataTypeOID  uint32
	TypeModifier int32
	TableOID     uint32
	AttrNum      int16
}

// PGVerifiedResult is the internal execution output after both verification
// layers pass. Rows carry database-native text: nil is SQL NULL, a non-nil
// empty string is a zero-length value — the two never collapse; each public
// cell is the ≤8192-byte contract prefix and CellTruncated is its
// public-coordinate flag matrix over exactly the delivered rows. Refs,
// PublicProofs, Sources, and BoundRefs preserve the provenance a later
// disclosure stage needs; nothing here can resume a query.
type PGVerifiedResult struct {
	Columns       []PGResultColumn
	Rows          [][]*string
	CellTruncated [][]bool
	Page          pgsql.PageResult
	// FetchCount is the execution-window fact: the LIMIT literal the
	// rewritten statement actually carried (sentinel-inclusive, 0 for a
	// LIMIT 0 window). It is distinct from the page row count and the
	// server-owned row budget.
	FetchCount   int64
	Refs         []pgsql.RangeVarRef
	PublicProofs []pgsql.PublicColumnProof
	Sources      map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence
	BoundRefs    map[pgsql.RefID]*PGBoundRef
}

// pgExecAttempt owns every resource of one governed attempt: the execution
// pool/connection/transaction, the independent observation pool/connection,
// the binding record, the copied FieldDescriptions, and the private row
// buffer. Nothing escapes until both verification layers pass and cleanup
// completes; close() is the single convergence point for every exit path.
type pgExecAttempt struct {
	ctx      context.Context
	budget   pgAttemptBudget
	cfg      *pgx.ConnConfig
	database string
	pinned   string

	execPool *pgxpool.Pool
	execConn *pgxpool.Conn
	tx       pgx.Tx

	backendPID   int32
	backendStart time.Time // this backend session's start — disambiguates pid reuse
	virtualXID   string

	obsPool *pgxpool.Pool
	obsConn *pgxpool.Conn

	bind    *PGBindResult
	derived []PGRelationIdentity // toast-attributed identities beyond the gate's set
	fds     []pgconn.FieldDescription
	rows    [][]*string

	// parameters is the attempt-owned snapshot of the caller's trusted
	// parameter plan; paramValues is its owned transport encoding — a
	// non-nil []byte per bound value, never caller storage. navigation is
	// the attempt-owned snapshot of the prepared FK binding (nil for the
	// ordinary and saved entries).
	parameters  *pgParameterPlan
	paramValues [][]byte
	navigation  *pgNavigationBinding

	// T10-B attempt facts: the furthest stage reached, what the transport
	// knows about the user statement's remote fate, and the scalar evidence
	// record handed to orchestration on every exit path.
	stage    PGAttemptStage
	stmtFate pgStmtFate
	evidence PGAttemptEvidence

	// parameterDescribeSent records that the provenance Parse/Describe of
	// the original statement was dispatched to the driver — a
	// transmission fact separate from the result statement's remote fate,
	// cleared only when the driver positively proves pre-send.
	parameterDescribeSent bool
}

// pgExecAfterBindHook is a test-only seam mirroring the gate's
// afterEntityResolve: it runs inside the attempt transaction after binding
// (and derived-identity collection) so integration tests can interleave a
// controlled fault — an extra lock for the mismatch audit or a blocked-DDL
// barrier — while the approved locks are held. Production leaves it nil.
var pgExecAfterBindHook func(ctx context.Context, tx pgx.Tx) error

// pgExecAfterMaterializeHook is a second test-only seam, invoked after the
// transport reader has closed and before position verification. A probe
// statement run on the same connection here would corrupt any
// FieldDescription/cell slice that still aliased pgx buffers — the attempt's
// copies must survive it for verification to pass.
var pgExecAfterMaterializeHook func(ctx context.Context, tx pgx.Tx) error

// pgExecPostRewriteHook is a third test-only seam between rewrite output and
// the pre-execution expected-layout check. It may mutate the expected
// records to model a fault inside the record set itself — the pre-check must
// then stop ExecParams, or the post-check must catch a consistent lie.
var pgExecPostRewriteHook func(rw *pgsql.RewriteResult) error

// pgExecAfterAuditHook runs after the lock audit verifies and before the
// result is projected — the point a cleanup-stage fault can still prove
// that verified data is never delivered when teardown fails.
var pgExecAfterAuditHook func(ctx context.Context, tx pgx.Tx) error

// pgExecAfterAcquireHook is a test-only seam fired once the execution
// connection is acquired, immediately before BEGIN — it simulates a caller
// cancellation landing between connection acquisition and transaction
// setup. Production leaves it nil.
var pgExecAfterAcquireHook func(ctx context.Context)

// pgExecPreIdentityHook is a test-only seam fired inside
// readTransactionIdentity after the statement budget is armed and before
// the identity SELECT — it simulates a caller cancellation landing during
// session/identity setup. Production leaves it nil.
var pgExecPreIdentityHook func(ctx context.Context)

// pgExecPreSendHook is a test-only seam fired inside executeAndMaterialize
// after the statement budget is armed and immediately before the transport
// dispatch — it simulates a caller cancellation landing in the gap between
// a successful arm and the driver send. Production leaves it nil.
var pgExecPreSendHook func(ctx context.Context)

// pgExecObserverConfig lets a test mutate the validated ConnConfig before
// the independent observation pool is built — the only way to make the
// observer's dial fail on a real server while the execution side stays
// perfectly healthy.
var pgExecObserverConfig func(*pgx.ConnConfig) *pgx.ConnConfig

// pgExecDisableDerived skips the TOAST ownership edge collection in tests so
// a held toast lock proves the explanation branch is load-bearing instead of
// silently passing as "not held".
var pgExecDisableDerived bool

// pgExecObserverSQL wraps the observer's fixed pg_locks statement in tests
// (e.g. a sleep-injected variant) so a real second-connection query can be
// stalled mid-flight while the production path keeps the original SQL.
var pgExecObserverSQL func(orig string) string

// pgExecTransportCalls counts RESULT ExecParams invocations — the seam sits
// immediately before the user result statement is sent, so a test can prove
// malformed expected records stopped the wire rather than merely erroring
// later. The R1-1 parameter-provenance Describe/probe dispatches are not
// result executions and never increment it.
var pgExecTransportCalls atomic.Int64

// ExecutePGGoverned runs one complete governed attempt and returns the
// internal verified result only when position verification and the lock-set
// audit both pass and cleanup finished. Any failure is classified onto the
// frozen vocabulary: *pgsql.RejectError (rejected), ErrQueryTimeout,
// context.Canceled, ErrQueryResultTooLarge, ErrQueryValidationFailed, or a
// redacted internal failure.
func ExecutePGGoverned(ctx context.Context, in PGExecuteInput) (*PGVerifiedResult, error) {
	res, _, err := ExecutePGGovernedOutcome(ctx, in)
	return res, err
}

// ExecutePGGovernedOutcome is ExecutePGGoverned plus the T10-B evidence
// record: the same shared attempt produces a PGAttemptEvidence on every
// exit path — success, rejection, timeout, cancellation, failure — so later
// claim/finalize orchestration can persist backend identity, remote_state,
// and the classified status without re-running anything. The evidence is
// never nil when input validation passed.
func ExecutePGGovernedOutcome(ctx context.Context, in PGExecuteInput) (*PGVerifiedResult, *PGAttemptEvidence, error) {
	return runPGGovernedAttempt(ctx, in, nil)
}

// runPGGovernedAttempt is the shared attempt body — the T9-B seam. A non-nil
// disclosure stage inserts the provenance-bound policy preflight between the
// rewrite records' pre-wire validation and ExecParams (policy reads spend
// the same attempt budget) and carries the plan out for the caller's
// post-cleanup transform. A nil stage preserves the undisclosed T7 path.
func runPGGovernedAttempt(ctx context.Context, in PGExecuteInput, disc *pgDisclosureStage) (res *PGVerifiedResult, ev *PGAttemptEvidence, err error) {
	if in.ConnConfig == nil || in.Database == "" || in.PinnedSchema == "" || in.Timeout <= 0 {
		return nil, &PGAttemptEvidence{Stage: PGStageInput, Status: pgAttemptStatus(ErrQueryValidationFailed)}, ErrQueryValidationFailed
	}
	a := &pgExecAttempt{ctx: ctx, cfg: in.ConnConfig, database: in.Database, pinned: in.PinnedSchema, stage: PGStageInput}
	defer func() {
		// A cleanup failure can never hide behind a successful result, and a
		// known primary failure is never masked by it either. Execution locks
		// are released before the bounded remote-state observation runs, so
		// probing can never hold the transaction hostage.
		if cerr := a.close(); cerr != nil && err == nil {
			res, err = nil, cerr
		}
		// One final conclusion per attempt: the evidence always reflects the
		// error actually returned, whatever stage produced it.
		a.evidence.Stage = a.stage
		a.evidence.Status = pgAttemptStatus(err)
		ev = &a.evidence
	}()

	// Unified attempt deadline: an earlier caller deadline still wins through
	// parent propagation.
	attemptCtx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()
	a.ctx = attemptCtx
	budget, err := pgAttemptBudgetFromContext(attemptCtx)
	if err != nil {
		return nil, nil, err
	}
	a.budget = budget

	// CPU-only stages are bracketed by context checks — no database work
	// continues on a spent or canceled attempt context.
	if err := attemptCtx.Err(); err != nil {
		return nil, nil, a.fail(err)
	}
	// Attempt-owned plan and binding snapshots — before any guard work so a
	// contract fault (untrusted source, plan for different text, malformed
	// mapping) can never reach a connection. The snapshots share no storage
	// with the caller's values.
	plan, err := snapshotPGParameterPlan(in.parameters, in.Statement)
	if err != nil {
		return nil, nil, a.fail(err)
	}
	nav, err := snapshotPGNavigationBinding(in.navigation)
	if err != nil {
		return nil, nil, a.fail(err)
	}
	// The plan source and binding must agree: a navigation plan requires a
	// navigation binding with one locator value per FK column pair, and a
	// binding requires exactly that plan — anything else is an internal
	// wiring defect, never a user error.
	if nav == nil && plan != nil && plan.source == pgParameterSourceNavigation {
		return nil, nil, a.fail(fmt.Errorf("%w: navigation plan without binding", pgsql.ErrEvidenceUnavailable))
	}
	if nav != nil && (plan == nil || plan.source != pgParameterSourceNavigation || len(plan.values) != len(nav.pairs)) {
		return nil, nil, a.fail(fmt.Errorf("%w: navigation binding/plan mismatch", pgsql.ErrEvidenceUnavailable))
	}
	if nav != nil {
		// The statement must be exactly what the trusted builder produces
		// from the owned binding and owned values — a caller or wiring
		// defect that swapped the predicate columns (same target, same
		// parameter count) fails closed before any guard or connection.
		expected, err := newPGNavigationPlan(nav, plan.values)
		if err != nil || expected.statement != in.Statement {
			return nil, nil, a.fail(fmt.Errorf("%w: navigation statement does not match binding", pgsql.ErrEvidenceUnavailable))
		}
	}
	a.parameters = plan
	a.navigation = nav
	if plan != nil {
		a.paramValues = plan.wireValues()
	}

	a.stage = PGStageGuard
	gr, err := pgsql.GuardPG(in.Statement)
	if err != nil {
		return nil, nil, a.fail(err)
	}
	// The parameter contract binds on the guard's immutable tree: a bound
	// plan owes a dense {1..n} reference set, and the ordinary unbound path
	// permits no ParamRef at all — native $k never reaches the wire.
	paramCount := 0
	if a.parameters != nil {
		paramCount = len(a.parameters.values)
	}
	if err := pgsql.ValidateParameterBindings(gr, paramCount); err != nil {
		return nil, nil, a.fail(err)
	}
	if err := attemptCtx.Err(); err != nil {
		return nil, nil, a.fail(err)
	}

	a.stage = PGStageConnect
	if err := a.openExecution(); err != nil {
		return nil, nil, a.fail(err)
	}
	a.stage = PGStageIdentity
	if err := a.readTransactionIdentity(); err != nil {
		return nil, nil, a.fail(err)
	}
	a.stage = PGStageBind
	bind, err := PGBind(attemptCtx, PGBindInput{
		Tx: a.tx, Database: in.Database, PinnedSchema: in.PinnedSchema, Guard: gr,
		requirePinnedSchema: in.requirePinnedSchema,
		navigation:          a.navigation,
	})
	if err != nil {
		return nil, nil, a.fail(err)
	}
	a.bind = bind
	if err := a.collectDerivedIdentities(); err != nil {
		return nil, nil, a.fail(err)
	}
	if pgExecAfterBindHook != nil {
		if err := pgExecAfterBindHook(attemptCtx, a.tx); err != nil {
			return nil, nil, a.fail(err)
		}
	}
	if err := attemptCtx.Err(); err != nil {
		return nil, nil, a.fail(err)
	}
	a.stage = PGStageRewrite
	rw, win, err := pgsql.RewriteFromGuardPaginated(in.Statement, in.PinnedSchema, gr, bind.Resolver, in.Page)
	if err != nil {
		return nil, nil, a.fail(err)
	}
	if err := attemptCtx.Err(); err != nil {
		return nil, nil, a.fail(err)
	}
	if pgExecPostRewriteHook != nil {
		if err := pgExecPostRewriteHook(rw); err != nil {
			return nil, nil, a.fail(err)
		}
	}
	// Expected records are proven against the bound evidence before any user
	// byte reaches the wire — a malformed record set never sends the
	// transport statement.
	if err := verifyPGExpectedLayout(rw, bind); err != nil {
		return nil, nil, a.fail(err)
	}

	// T9-B: the disclosure preflight consumes this attempt's bound and
	// pre-wire-validated records. A policy refusal, evidence fault, or
	// policy-store failure here means the user statement is never sent.
	if disc != nil {
		a.stage = PGStagePreflight
		if err := a.disclosurePreflight(disc, rw, bind); err != nil {
			return nil, nil, a.fail(err)
		}
		if err := attemptCtx.Err(); err != nil {
			return nil, nil, a.fail(err)
		}
	}

	// R1-1: a bound plan's values are proven on their own parameter-only
	// protocol surface before the result statement runs — a conversion
	// failure there has the parameter as its only possible source, so the
	// controlled rejection is attribution, never SQLSTATE coincidence.
	if len(a.paramValues) > 0 {
		a.stage = PGStageParameters
		if err := a.validatePGParameterInputs(rw); err != nil {
			return nil, nil, a.fail(err)
		}
		if err := attemptCtx.Err(); err != nil {
			return nil, nil, a.fail(err)
		}
	}

	a.stage = PGStageExecute
	if err := a.executeAndMaterialize(rw.SQL, win); err != nil {
		return nil, nil, a.fail(err)
	}
	if pgExecAfterMaterializeHook != nil {
		if err := pgExecAfterMaterializeHook(attemptCtx, a.tx); err != nil {
			return nil, nil, a.fail(err)
		}
	}
	a.stage = PGStageVerify
	if err := verifyPGPositions(a.fds, rw, bind); err != nil {
		return nil, nil, a.fail(err)
	}
	if err := attemptCtx.Err(); err != nil {
		return nil, nil, a.fail(err)
	}
	a.stage = PGStageAudit
	if err := a.auditLockSet(); err != nil {
		return nil, nil, a.fail(err)
	}
	if err := attemptCtx.Err(); err != nil {
		return nil, nil, a.fail(err)
	}
	if pgExecAfterAuditHook != nil {
		if err := pgExecAfterAuditHook(attemptCtx, a.tx); err != nil {
			return nil, nil, a.fail(err)
		}
	}

	a.stage = PGStageDeliver
	page, err := win.Result(len(a.rows))
	if err != nil {
		return nil, nil, a.fail(fmt.Errorf("%w: %v", pgsql.ErrEvidenceUnavailable, err))
	}
	out, err := projectPGPublicResult(a.fds, a.rows[:page.RowCount], rw, a.execConn.Conn().TypeMap())
	if err != nil {
		return nil, nil, a.fail(err)
	}
	out.Page = page
	out.FetchCount = win.FetchCount
	out.Refs = rw.Refs
	out.PublicProofs = rw.PublicProofs
	out.Sources = rw.SourceCatalog
	out.BoundRefs = bind.Refs
	// The success verdict must land inside a live attempt: a deadline that
	// passed during the last CPU stages turns this into a timeout, never a
	// delivered result.
	if err := attemptCtx.Err(); err != nil {
		return nil, nil, a.fail(err)
	}
	return out, nil, nil
}

// openExecution builds the execution pool, acquires the single connection,
// and opens the READ ONLY transaction with the frozen G4 session settings:
// pinned search_path as a bound value (the pinned name is one identifier,
// never a spliced search_path fragment), UTC, then the first armed
// statement_timeout from the remaining budget.
func (a *pgExecAttempt) openExecution() error {
	pool, err := OpenPostgresPool(a.ctx, a.cfg)
	if err != nil {
		return err
	}
	a.execPool = pool
	conn, err := pool.Acquire(a.ctx)
	if err != nil {
		return err
	}
	a.execConn = conn
	// The backend pid is known the moment Acquire returns — record it now
	// so a later BEGIN/session/identity failure cannot erase an identity
	// the connection already proved. backend_start and the virtual xid are
	// still filled by the real identity query below; they stay absent if
	// that query never completes, never fabricated.
	a.backendPID = int32(conn.Conn().PgConn().PID())
	if pgExecAfterAcquireHook != nil {
		pgExecAfterAcquireHook(a.ctx)
	}
	tx, err := conn.BeginTx(a.ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	a.tx = tx
	if _, err := tx.Exec(a.ctx,
		`SELECT pg_catalog.set_config('search_path', $1, true)`,
		pgQuoteIdent(a.pinned)+",pg_catalog"); err != nil {
		return err
	}
	if _, err := tx.Exec(a.ctx,
		`SELECT pg_catalog.set_config('TimeZone', 'UTC', true)`); err != nil {
		return err
	}
	return a.budget.arm(a.ctx, tx)
}

// readTransactionIdentity records this backend's pid, this transaction's
// virtualtransaction, and the backend's session start — the held-lock
// evidence key the independent audit later matches, plus the identity fact a
// later remote-state probe uses to reject pid reuse. The probe reads our own
// already-granted virtualxid ExclusiveLock row while the connection is
// confirmed live — never a post-hoc lookup after the statement ends — and
// must yield exactly one non-empty identity; anything else is missing
// internal evidence, never a guessed substitute like a permanent xid. The
// session start comes from pg_stat_get_backend_* functions, NOT the
// pg_stat_activity view: a view reference would hold an AccessShareLock the
// lock audit cannot explain.
func (a *pgExecAttempt) readTransactionIdentity() error {
	if err := a.budget.arm(a.ctx, a.tx); err != nil {
		return err
	}
	if pgExecPreIdentityHook != nil {
		pgExecPreIdentityHook(a.ctx)
	}
	rows, err := a.tx.Query(a.ctx,
		`SELECT pg_catalog.pg_backend_pid(), l.virtualtransaction,
		        pg_catalog.pg_stat_get_backend_start((
		          SELECT s.id FROM pg_catalog.pg_stat_get_backend_idset() s(id)
		           WHERE pg_catalog.pg_stat_get_backend_pid(s.id) = pg_catalog.pg_backend_pid()))
		   FROM pg_catalog.pg_locks l
		  WHERE l.pid = pg_catalog.pg_backend_pid()
		    AND l.locktype = 'virtualxid'
		    AND l.mode = 'ExclusiveLock'
		    AND l.granted`)
	if err != nil {
		return err
	}
	var (
		pid          int32
		vxid         string
		backendStart *time.Time
		n            int
	)
	for rows.Next() {
		n++
		if scanErr := rows.Scan(&pid, &vxid, &backendStart); scanErr != nil {
			rows.Close()
			return scanErr
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if n != 1 || pid == 0 || vxid == "" || backendStart == nil || backendStart.IsZero() {
		return fmt.Errorf("%w: attempt transaction identity absent", pgsql.ErrEvidenceUnavailable)
	}
	a.backendPID = pid
	a.backendStart = *backendStart
	a.virtualXID = vxid
	return nil
}

// executeAndMaterialize arms the remaining budget, runs the transport SQL via
// a fresh ExecParams parse (never the prepared-statement cache), copies the
// FieldDescriptions while they are current, and streams rows into the
// private bounded buffer. The reader is always closed so the protocol
// converges before any later statement or rollback.
func (a *pgExecAttempt) executeAndMaterialize(sql string, win pgsql.PageWindow) error {
	if err := a.budget.arm(a.ctx, a.tx); err != nil {
		return err
	}
	if pgExecPreSendHook != nil {
		pgExecPreSendHook(a.ctx)
	}
	pgExecTransportCalls.Add(1)
	// Dispatched to the transport: from here the statement's remote fate is
	// indeterminate until the reader proves completion or the server
	// delivers an ErrorResponse.
	a.stmtFate = pgStmtIndeterminate
	rr := a.tx.Conn().PgConn().ExecParams(a.ctx, sql, a.paramValues, nil, nil, []int16{0})

	fds := rr.FieldDescriptions()
	a.fds = make([]pgconn.FieldDescription, len(fds))
	copy(a.fds, fds)

	var readErr error
	if len(a.fds) > pgMaxTransportColumns {
		readErr = fmt.Errorf("%w: transport carries %d columns beyond bound %d",
			pgsql.ErrEvidenceUnavailable, len(a.fds), pgMaxTransportColumns)
	}
	totalBytes := 0
	for readErr == nil {
		if err := a.ctx.Err(); err != nil {
			readErr = err
			break
		}
		if !rr.NextRow() {
			break
		}
		if int64(len(a.rows)) >= win.FetchCount {
			// A row beyond the rewritten fetch window is an internal
			// anomaly — stop now, never collect the surplus first.
			readErr = fmt.Errorf("%w: transport row %d exceeds fetch window %d",
				pgsql.ErrEvidenceUnavailable, len(a.rows)+1, win.FetchCount)
			break
		}
		vals := rr.Values()
		row := make([]*string, len(vals))
		rowOK := true
		for i, v := range vals {
			if v == nil {
				continue // SQL NULL stays nil
			}
			// Check the remaining budget before copying — an oversized
			// value is rejected without being duplicated, and the
			// subtraction cannot underflow because totalBytes never
			// exceeds the cap.
			if len(v) > pgMaxResultBytes-totalBytes {
				readErr = ErrQueryResultTooLarge
				rowOK = false
				break
			}
			s := string(v) // native wire text, copied before the next read
			row[i] = &s
			totalBytes += len(s)
		}
		if !rowOK {
			break
		}
		a.rows = append(a.rows, row)
	}
	_, closeErr := rr.Close()
	// The remote-fate fact is folded from both outcomes — a delivered
	// ErrorResponse is the server's own termination proof, a clean close
	// through ReadyForQuery is completion even when a local error already
	// ended our read, and the driver's pre-send mark proves nothing was
	// dispatched. The primary error keeps its own priority below and
	// never carries remote facts.
	a.stmtFate = pgStmtFateAfterRead(readErr, closeErr)
	if readErr != nil {
		return a.classifyTransportError(readErr)
	}
	if closeErr != nil {
		return a.classifyTransportError(closeErr)
	}
	return nil
}

// classifyPGExecTransportError narrows user-transport-stage failures onto
// the frozen relation-access verdict (a bound object that vanished or lost
// privilege between binding and parse). Only the user's own statement goes
// through here — observer probes and internal catalog reads never turn a
// 42501 into a user rejection.
func classifyPGExecTransportError(err error) error {
	if c := classifyPGRelationAccess(err); c != nil {
		return c
	}
	return err
}

// classifyTransportError is classifyPGExecTransportError — R1-1 removed the
// plan-based SQLSTATE mapping entirely: a result-path conversion failure
// keeps the ordinary taxonomy because the parameter provenance seam has
// already isolated the caller's values on their own protocol surface.
// Timeout and cancel classification keep their existing priority in fail().
func (a *pgExecAttempt) classifyTransportError(err error) error {
	return classifyPGExecTransportError(err)
}

// fail maps an executor error onto the frozen terminal vocabulary, mirroring
// the binding gate's classifier: the attempt context is authoritative for
// cancelled-vs-timeout, controlled rejections and known sentinels pass
// through, 57014 is timeout, and everything else is the redacted internal
// failure.
func (a *pgExecAttempt) fail(err error) error {
	if err == nil {
		return nil
	}
	var re *pgsql.RejectError
	if errors.As(err, &re) {
		return re
	}
	if cerr := a.ctx.Err(); cerr != nil {
		if errors.Is(cerr, context.Canceled) {
			return context.Canceled
		}
		return ErrQueryTimeout
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrQueryTimeout
	case errors.Is(err, ErrQueryTimeout), errors.Is(err, ErrQueryBackendFailure),
		errors.Is(err, ErrQueryResultTooLarge), errors.Is(err, ErrQueryValidationFailed),
		errors.Is(err, ErrQueryDisclosureBlocked), errors.Is(err, ErrQueryDisclosureBackendFailure),
		errors.Is(err, pgsql.ErrEvidenceUnavailable),
		errors.Is(err, ErrPGVersionUnsupported), errors.Is(err, ErrPGConnectFailed),
		errors.Is(err, ErrPGVersionReadFailed):
		// Classified sentinels keep their identity — the version gate's
		// verdict must stay recognizable, never flattened into a generic
		// backend failure.
		return err
	case isPGQueryCanceled(err):
		return ErrQueryTimeout
	default:
		return errPGExecInternal
	}
}

// close converges every owned resource on every exit path in a fixed order:
// ROLLBACK and execution-connection release first — under an independent
// bounded context so an already-canceled request still releases the
// transaction's locks — then the bounded remote-state observation, then the
// audit-observation pool teardown. The probe waits on no execution lock and
// no execution lock waits on the probe. A connection is always released
// before its pool is closed (Pool.Close waits on outstanding acquisitions).
// A rollback failure is reported; it never turns cached rows into a
// delivered result.
func (a *pgExecAttempt) close() error {
	first := a.releaseExecution()
	a.observeRemoteState()
	a.releasePools()
	return first
}

// releaseExecution rolls the attempt transaction back, releases the
// execution connection, and closes the execution pool. This is the fast
// half of close(): it must complete before remote observation spends its
// own window, so a canceled or failed attempt frees its locks at once.
func (a *pgExecAttempt) releaseExecution() error {
	base := a.ctx
	if base == nil {
		base = context.Background()
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(base), pgExecCleanupWindow)
	defer cancel()
	var first error
	if a.tx != nil {
		if err := a.tx.Rollback(cleanup); err != nil {
			first = fmt.Errorf("%w: attempt rollback", ErrQueryBackendFailure)
		}
		a.tx = nil
	}
	if a.execConn != nil {
		a.execConn.Release()
		a.execConn = nil
	}
	return first
}

// releasePools tears down both pools after the remote probe. A dead
// execution connection's destruction is asynchronous (puddle runs the
// destructor in a goroutine), so execConn.Release above has already returned
// it — Pool.Close here only waits for that bounded teardown to finish. The
// probe therefore overlaps the dead connection's drain instead of queuing
// behind it.
func (a *pgExecAttempt) releasePools() {
	if a.execPool != nil {
		a.execPool.Close()
		a.execPool = nil
	}
	if a.obsConn != nil {
		a.obsConn.Release()
		a.obsConn = nil
	}
	if a.obsPool != nil {
		a.obsPool.Close()
		a.obsPool = nil
	}
}
