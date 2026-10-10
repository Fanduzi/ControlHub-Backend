// Package service runs the governed PostgreSQL execution attempt end to end.
// input: attempt context with deadline, T2-validated *pgx.ConnConfig, resolved
// database/pinned schema, user statement text, pgsql.PageOptions and the
// caller-resolved attempt timeout
// output: ExecutePGGoverned, PGExecuteInput, PGVerifiedResult, PGResultColumn,
// errPGExecInternal — internal verified result (native-text public rows, G7
// column types, pagination verdict, provenance/binding records) or the frozen
// classified error; no live connection, transaction, cursor, or resolver ever
// leaves the attempt
// pos: T7-S3/S4 internal execution closure (frozen G4 steps 1–10 plus G10
// mechanism 5b/5c) — guard → OpenPostgresPool → BEGIN READ ONLY + G4 session
// settings → in-transaction virtualxid identity → PGBind →
// RewriteFromGuardPaginated → ExecParams fresh parse with text result format →
// bounded private materialization → per-position FD verification →
// second-connection lock-set audit → ROLLBACK → witness strip → deliver;
// every exit path converges resources under an independent bounded cleanup
// context and every failure maps onto timeout/cancelled/rejected/failed
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
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
// empty string is a zero-length value — the two never collapse. Refs,
// PublicProofs, Sources, and BoundRefs preserve the provenance a later
// disclosure stage needs; nothing here can resume a query.
type PGVerifiedResult struct {
	Columns      []PGResultColumn
	Rows         [][]*string
	Page         pgsql.PageResult
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

	backendPID int32
	virtualXID string

	obsPool *pgxpool.Pool
	obsConn *pgxpool.Conn

	bind    *PGBindResult
	derived []PGRelationIdentity // toast-attributed identities beyond the gate's set
	fds     []pgconn.FieldDescription
	rows    [][]*string
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

// ExecutePGGoverned runs one complete governed attempt and returns the
// internal verified result only when position verification and the lock-set
// audit both pass and cleanup finished. Any failure is classified onto the
// frozen vocabulary: *pgsql.RejectError (rejected), ErrQueryTimeout,
// context.Canceled, ErrQueryResultTooLarge, ErrQueryValidationFailed, or a
// redacted internal failure.
func ExecutePGGoverned(ctx context.Context, in PGExecuteInput) (res *PGVerifiedResult, err error) {
	if in.ConnConfig == nil || in.Database == "" || in.PinnedSchema == "" || in.Timeout <= 0 {
		return nil, ErrQueryValidationFailed
	}
	a := &pgExecAttempt{ctx: ctx, cfg: in.ConnConfig, database: in.Database, pinned: in.PinnedSchema}
	defer func() {
		// A cleanup failure can never hide behind a successful result, and a
		// known primary failure is never masked by it either.
		if cerr := a.close(); cerr != nil && err == nil {
			res, err = nil, cerr
		}
	}()

	// Unified attempt deadline: an earlier caller deadline still wins through
	// parent propagation.
	attemptCtx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()
	a.ctx = attemptCtx
	budget, err := pgAttemptBudgetFromContext(attemptCtx)
	if err != nil {
		return nil, err
	}
	a.budget = budget

	// CPU-only stages are bracketed by context checks — no database work
	// continues on a spent or canceled attempt context.
	if err := attemptCtx.Err(); err != nil {
		return nil, a.fail(err)
	}
	gr, err := pgsql.GuardPG(in.Statement)
	if err != nil {
		return nil, a.fail(err)
	}
	if err := attemptCtx.Err(); err != nil {
		return nil, a.fail(err)
	}

	if err := a.openExecution(); err != nil {
		return nil, a.fail(err)
	}
	if err := a.readTransactionIdentity(); err != nil {
		return nil, a.fail(err)
	}
	bind, err := PGBind(attemptCtx, PGBindInput{
		Tx: a.tx, Database: in.Database, PinnedSchema: in.PinnedSchema, Guard: gr,
	})
	if err != nil {
		return nil, a.fail(err)
	}
	a.bind = bind
	if err := a.collectDerivedIdentities(); err != nil {
		return nil, a.fail(err)
	}
	if pgExecAfterBindHook != nil {
		if err := pgExecAfterBindHook(attemptCtx, a.tx); err != nil {
			return nil, a.fail(err)
		}
	}
	if err := attemptCtx.Err(); err != nil {
		return nil, a.fail(err)
	}
	rw, win, err := pgsql.RewriteFromGuardPaginated(in.Statement, in.PinnedSchema, gr, bind.Resolver, in.Page)
	if err != nil {
		return nil, a.fail(err)
	}
	if err := attemptCtx.Err(); err != nil {
		return nil, a.fail(err)
	}

	if err := a.executeAndMaterialize(rw.SQL, win); err != nil {
		return nil, a.fail(err)
	}
	if pgExecAfterMaterializeHook != nil {
		if err := pgExecAfterMaterializeHook(attemptCtx, a.tx); err != nil {
			return nil, a.fail(err)
		}
	}
	if err := verifyPGPositions(a.fds, rw, bind); err != nil {
		return nil, a.fail(err)
	}
	if err := a.auditLockSet(); err != nil {
		return nil, a.fail(err)
	}

	page, err := win.Result(len(a.rows))
	if err != nil {
		return nil, a.fail(fmt.Errorf("%w: %v", pgsql.ErrEvidenceUnavailable, err))
	}
	out := projectPGPublicResult(a.fds, a.rows[:page.RowCount], rw, a.execConn.Conn().TypeMap())
	out.Page = page
	out.Refs = rw.Refs
	out.PublicProofs = rw.PublicProofs
	out.Sources = rw.SourceCatalog
	out.BoundRefs = bind.Refs
	return out, nil
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

// readTransactionIdentity records this backend's pid and this transaction's
// virtualtransaction — the held-lock evidence key the independent audit
// later matches. The probe reads our own already-granted virtualxid
// ExclusiveLock row and must yield exactly one non-empty identity; anything
// else is missing internal evidence, never a guessed substitute like a
// permanent xid.
func (a *pgExecAttempt) readTransactionIdentity() error {
	if err := a.budget.arm(a.ctx, a.tx); err != nil {
		return err
	}
	rows, err := a.tx.Query(a.ctx,
		`SELECT pg_catalog.pg_backend_pid(), l.virtualtransaction
		   FROM pg_catalog.pg_locks l
		  WHERE l.pid = pg_catalog.pg_backend_pid()
		    AND l.locktype = 'virtualxid'
		    AND l.mode = 'ExclusiveLock'
		    AND l.granted`)
	if err != nil {
		return err
	}
	var (
		pid  int32
		vxid string
		n    int
	)
	for rows.Next() {
		n++
		if scanErr := rows.Scan(&pid, &vxid); scanErr != nil {
			rows.Close()
			return scanErr
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if n != 1 || pid == 0 || vxid == "" {
		return fmt.Errorf("%w: attempt transaction identity absent", pgsql.ErrEvidenceUnavailable)
	}
	a.backendPID = pid
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
	rr := a.tx.Conn().PgConn().ExecParams(a.ctx, sql, nil, nil, nil, []int16{0})

	fds := rr.FieldDescriptions()
	a.fds = make([]pgconn.FieldDescription, len(fds))
	copy(a.fds, fds)

	var readErr error
	if len(a.fds) > pgMaxResultColumns {
		readErr = ErrQueryResultTooLarge
	}
	totalBytes := 0
	for readErr == nil && rr.NextRow() {
		vals := rr.Values()
		row := make([]*string, len(vals))
		for i, v := range vals {
			if v == nil {
				continue // SQL NULL stays nil
			}
			s := string(v) // native wire text, copied before the next read
			row[i] = &s
			totalBytes += len(s)
		}
		if totalBytes > pgMaxResultBytes {
			readErr = ErrQueryResultTooLarge
			break
		}
		a.rows = append(a.rows, row)
	}
	_, closeErr := rr.Close()
	switch {
	case readErr != nil:
		return readErr
	case closeErr != nil:
		return closeErr
	case int64(len(a.rows)) > win.FetchCount:
		// More rows than the rewritten window permits is an internal
		// defect — the buffer is dropped, never delivered.
		return fmt.Errorf("%w: transport returned %d rows beyond window %d",
			pgsql.ErrEvidenceUnavailable, len(a.rows), win.FetchCount)
	}
	return nil
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
		errors.Is(err, pgsql.ErrEvidenceUnavailable):
		return err
	case isPGQueryCanceled(err):
		return ErrQueryTimeout
	default:
		return errPGExecInternal
	}
}

// close converges every owned resource on every exit path. ROLLBACK runs
// first — under an independent bounded context so an already-canceled request
// still releases the transaction's locks — and before the observation pool is
// torn down, so a failed audit can never hold execution locks hostage. A
// connection is always released before its pool is closed (Pool.Close waits
// on outstanding acquisitions). A rollback failure is reported; it never
// turns cached rows into a delivered result.
func (a *pgExecAttempt) close() error {
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
	return first
}
