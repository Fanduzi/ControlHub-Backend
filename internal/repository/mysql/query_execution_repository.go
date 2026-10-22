// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, context, errors, expvar, fmt, log, strconv, internal/model
// output: NewQueryExecutionRepository, identity-aware atomic keyless-only InsertExecutionWithAudit with successful-user-only full SQL, owner-only statement lookup, user/machine history with private-statement availability, credential metadata/audit operations, QueryEvidencePersistenceFailures accessor
// pos: MySQL persistence boundary for exactly-one-actor query history, non-disclosing reusable-statement availability, owner-only private statements, and at-most-one-actor audit evidence without credential data
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"expvar"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// QueryExecutionRepository reads credential metadata and writes/lists query
// execution history and audit events for the read-only query sandbox. It never
// stores or returns a DSN/password — only the opaque, validated credential_ref.
type QueryExecutionRepository struct {
	db *sql.DB
}

func NewQueryExecutionRepository(db *sql.DB) *QueryExecutionRepository {
	return &QueryExecutionRepository{db: db}
}

// Shared SQL for credential metadata + audit. The standalone and transactional
// (WithAudit) methods use the SAME statements so their behavior cannot drift;
// only the transaction boundary differs.
const (
	upsertCredentialMetadataSQL = `insert into query_target_credentials (resource_id, database_name, default_schema, engine, credential_ref, enabled, environment_policy)
		           values (?, ?, ?, ?, ?, ?, ?)
		           on duplicate key update
		             default_schema = values(default_schema),
		             engine = values(engine),
		             credential_ref = values(credential_ref),
		             enabled = values(enabled),
		             environment_policy = values(environment_policy)`
	deleteCredentialMetadataSQL = `delete from query_target_credentials where resource_id = ? and database_name = ?`
	insertAuditEventSQL         = `insert into audit_events (actor_user_id, target_resource_id, event_type, result) values (?, ?, ?, ?)`
	insertExecutionAuditSQL     = `insert into audit_events (actor_user_id, actor_machine_principal_id, target_resource_id, event_type, result) values (?, ?, ?, ?, ?)`
	insertExecutionSQL          = `insert into query_executions
	           (target_resource_id, actor_user_id, actor_machine_principal_id, engine, database_name, schema_name, statement_digest, statement_preview, full_statement, status, row_count, duration_ms, error_code, error_message, backend_pid, remote_state, client_execution_id, created_at)
	           values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(?, CURRENT_TIMESTAMP(6)))`
)

// QueryEvidencePersistenceFailures is the dimensionless operational counter for
// atomic Execution Evidence Pair persistence failures (Issue #34). It carries
// no dimensions and is published under a stable key so operational tooling can
// scrape it without touching the admin-only metrics endpoint.
var QueryEvidencePersistenceFailures = expvar.NewInt("query_evidence_persistence_failures")

// recordQueryEvidencePersistenceFailure increments the dimensionless counter
// exactly once and emits the ONE fixed safe log category. Neither surface may
// contain actor, target, statement, template value, credential, DSN, request
// data, or raw driver/database error details.
func recordQueryEvidencePersistenceFailure() {
	QueryEvidencePersistenceFailures.Add(1)
	log.Printf("query_evidence_persistence_failed error_class=query_evidence_pair_persistence_failure")
}

// errQueryEvidencePairFailed is the internal pair-failure sentinel returned to
// callers. It is a fixed string with no driver/database/statement details.
var errQueryEvidencePairFailed = errors.New("query evidence pair persistence failed")

// GetCredential returns the credential metadata for one query CONNECTION —
// the composite row keyed by (resource_id, database_name). databaseName is the
// connection's database segment: ” addresses the legacy single-connection
// MySQL/TiDB row; a non-empty name addresses a PostgreSQL connection row.
// It distinguishes three outcomes so callers never mask a failure as "no row":
//   - no row -> sql.ErrNoRows;
//   - a row whose credential_ref OR environment_policy fails validation ->
//     model.ErrInvalidCredentialMetadata (fail closed so the resolver never
//     performs an env lookup with an unvalidated key). The scanned row is
//     returned ALONGSIDE the sentinel so status callers can report
//     configured=true; every caller checks the error before trusting the row;
//   - any other read error -> a wrapped error (propagated as a backend error).
//
// The environment_policy is returned as the typed enum.
func (r *QueryExecutionRepository) GetCredential(ctx context.Context, resourceID uint64, databaseName string) (model.QueryCredentialMetadata, error) {
	const q = `select id, resource_id, database_name, default_schema, engine, credential_ref, enabled, environment_policy
	           from query_target_credentials where resource_id = ? and database_name = ?`
	var (
		meta      model.QueryCredentialMetadata
		enabled   bool
		policyStr string
	)
	err := r.db.QueryRowContext(ctx, q, resourceID, databaseName).Scan(
		&meta.ID, &meta.ResourceID, &meta.DatabaseName, &meta.DefaultSchema,
		&meta.Engine, &meta.CredentialRef, &enabled, &policyStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.QueryCredentialMetadata{}, sql.ErrNoRows
		}
		return model.QueryCredentialMetadata{}, fmt.Errorf("get query credential: %w", err)
	}
	meta.Enabled = enabled
	meta.EnvironmentPolicy = model.QueryEnvironmentPolicy(policyStr)
	// Validate the stored metadata defense-in-depth: a row that bypassed the
	// write path (legacy/manual data) must fail closed. An invalid ref or policy
	// is a distinct condition from "no row" — surface the sentinel, not nil.
	if vErr := model.ValidateCredentialRef(meta.CredentialRef); vErr != nil {
		return meta, fmt.Errorf("credential_ref for resource %d is invalid: %w", resourceID, model.ErrInvalidCredentialMetadata)
	}
	if pErr := meta.EnvironmentPolicy.Validate(); pErr != nil {
		return meta, fmt.Errorf("environment_policy for resource %d is invalid: %w", resourceID, model.ErrInvalidCredentialMetadata)
	}
	return meta, nil
}

// ListCredentials returns every credential row for one resource, ordered by
// database_name (the composite connection key, ” first). It is the
// connections[] enumeration read for the query target list: rows are scanned
// and re-validated with the same fail-closed rules as GetCredential — a row
// with an invalid credential_ref or environment_policy fails the whole list
// (ErrInvalidCredentialMetadata), never degrades into a silently dropped
// connection. It never returns a DSN or password — only metadata.
func (r *QueryExecutionRepository) ListCredentials(ctx context.Context, resourceID uint64) ([]model.QueryCredentialMetadata, error) {
	const q = `select id, resource_id, database_name, default_schema, engine, credential_ref, enabled, environment_policy
	           from query_target_credentials where resource_id = ? order by database_name`
	rows, err := r.db.QueryContext(ctx, q, resourceID)
	if err != nil {
		return nil, fmt.Errorf("list query credentials: %w", err)
	}
	defer rows.Close()

	var out []model.QueryCredentialMetadata
	for rows.Next() {
		var (
			meta      model.QueryCredentialMetadata
			enabled   bool
			policyStr string
		)
		if err := rows.Scan(
			&meta.ID, &meta.ResourceID, &meta.DatabaseName, &meta.DefaultSchema,
			&meta.Engine, &meta.CredentialRef, &enabled, &policyStr,
		); err != nil {
			return nil, fmt.Errorf("list query credentials scan: %w", err)
		}
		meta.Enabled = enabled
		meta.EnvironmentPolicy = model.QueryEnvironmentPolicy(policyStr)
		if vErr := model.ValidateCredentialRef(meta.CredentialRef); vErr != nil {
			return nil, fmt.Errorf("credential_ref for resource %d is invalid: %w", resourceID, model.ErrInvalidCredentialMetadata)
		}
		if pErr := meta.EnvironmentPolicy.Validate(); pErr != nil {
			return nil, fmt.Errorf("environment_policy for resource %d is invalid: %w", resourceID, model.ErrInvalidCredentialMetadata)
		}
		out = append(out, meta)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list query credentials rows: %w", err)
	}
	return out, nil
}

// UpsertCredentialMetadata writes or replaces a target's credential metadata. It
// is shared by the local/dev seed path (cmd/querydev) and the Phase 38A product
// write path, so it validates the credential_ref and environment_policy in-method
// (defense in depth — it never trusts the caller) and stores resource_id, engine,
// credential_ref, enabled, and environment_policy ONLY — never a DSN or password.
// The upsert keys on the existing unique (resource_id) index so repeated writes
// against the same target refresh its metadata instead of failing on the
// duplicate. Reads re-validate the ref fail-closed via model.ValidateCredentialRef.
func (r *QueryExecutionRepository) UpsertCredentialMetadata(ctx context.Context, meta model.QueryCredentialMetadata) error {
	if err := model.ValidateCredentialRef(meta.CredentialRef); err != nil {
		return fmt.Errorf("upsert query credential metadata: %w", err)
	}
	if err := meta.EnvironmentPolicy.Validate(); err != nil {
		return fmt.Errorf("upsert query credential metadata: %w", err)
	}
	if err := meta.ValidateConnectionIdentity(); err != nil {
		return fmt.Errorf("upsert query credential metadata: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, upsertCredentialMetadataSQL,
		meta.ResourceID, meta.DatabaseName, meta.DefaultSchema, meta.Engine, meta.CredentialRef, meta.Enabled, string(meta.EnvironmentPolicy),
	); err != nil {
		return fmt.Errorf("upsert query credential metadata: %w", err)
	}
	return nil
}

// UpsertCredentialMetadataWithAudit writes a target's credential metadata AND its
// audit event in a single transaction so the two can never diverge: if the audit
// insert fails, the metadata upsert is rolled back (no "configured but no audit"
// state). It validates the same way as UpsertCredentialMetadata (defense in
// depth) and stores metadata + audit only — never a DSN or password. The audit
// row records actor, target, event type, and result — never a DSN.
func (r *QueryExecutionRepository) UpsertCredentialMetadataWithAudit(ctx context.Context, meta model.QueryCredentialMetadata, actorUserID uint64, eventType, result string) error {
	if err := model.ValidateCredentialRef(meta.CredentialRef); err != nil {
		return fmt.Errorf("upsert query credential metadata: %w", err)
	}
	if err := meta.EnvironmentPolicy.Validate(); err != nil {
		return fmt.Errorf("upsert query credential metadata: %w", err)
	}
	if err := meta.ValidateConnectionIdentity(); err != nil {
		return fmt.Errorf("upsert query credential metadata: %w", err)
	}
	return r.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, upsertCredentialMetadataSQL,
			meta.ResourceID, meta.DatabaseName, meta.DefaultSchema, meta.Engine, meta.CredentialRef, meta.Enabled, string(meta.EnvironmentPolicy),
		); err != nil {
			return fmt.Errorf("upsert query credential metadata: %w", err)
		}
		if _, err := tx.ExecContext(ctx, insertAuditEventSQL, actorUserID, meta.ResourceID, eventType, result); err != nil {
			return fmt.Errorf("insert audit event: %w", err)
		}
		return nil
	})
}

// DeleteCredentialMetadataWithAudit removes a target's credential metadata AND
// writes its audit event in a single transaction: if the audit insert fails, the
// delete is rolled back so the original metadata remains (no unattributed
// removal). The audit row records actor, target, event type, and result only.
func (r *QueryExecutionRepository) DeleteCredentialMetadataWithAudit(ctx context.Context, resourceID uint64, databaseName string, actorUserID uint64, eventType, result string) error {
	return r.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, deleteCredentialMetadataSQL, resourceID, databaseName); err != nil {
			return fmt.Errorf("delete query credential metadata: %w", err)
		}
		if _, err := tx.ExecContext(ctx, insertAuditEventSQL, actorUserID, resourceID, eventType, result); err != nil {
			return fmt.Errorf("insert audit event: %w", err)
		}
		return nil
	})
}

// inTx runs fn inside a database transaction, rolling back on any error and
// committing only when fn returns nil. The deferred Rollback is a safe no-op
// after Commit. This is the atomic primitive for "metadata change + audit": the
// repository owns the transaction boundary so the service never stitches partial
// writes into a half-applied state.
func (r *QueryExecutionRepository) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin credential metadata transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; safe on error
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit credential metadata transaction: %w", err)
	}
	return nil
}

// DeleteCredential removes one connection's credential metadata, addressed by
// the composite key (resource_id, database_name). It is idempotent: deleting a
// connection that has no row is not an error. It never touches a DSN (none is
// stored) and is the Phase 38A product delete path.
func (r *QueryExecutionRepository) DeleteCredential(ctx context.Context, resourceID uint64, databaseName string) error {
	if _, err := r.db.ExecContext(ctx, deleteCredentialMetadataSQL, resourceID, databaseName); err != nil {
		return fmt.Errorf("delete query credential metadata: %w", err)
	}
	return nil
}

// executionRecordArgs converts a record into the insertExecutionSQL parameters.
// It is the single field mapping shared by the keyless and keyed evidence
// paths — the keyed path differs only by a non-NULL client_execution_id.
// Full SQL is bound only for successful user executions; every machine or
// non-success outcome binds NULL without rejecting its metadata evidence.
// Unset optional evidence binds NULL: BackendPID and ClientExecutionID stay
// NULL, remote_state binds its stored zero value ”. When rec.CreatedAt is
// zero a nil placeholder lets the database default apply.
func executionRecordArgs(rec model.QueryExecutionRecord) ([]any, error) {
	actorUserID, actorMachinePrincipalID, err := executionActorArgs(rec)
	if err != nil {
		return nil, err
	}
	var createdAt any
	if !rec.CreatedAt.IsZero() {
		createdAt = rec.CreatedAt.UTC()
	}
	var fullStatement any
	if rec.ActorUserID != 0 && rec.ActorMachinePrincipalID == 0 && rec.Status == model.QueryExecutionSuccess && rec.FullStatement != "" {
		fullStatement = rec.FullStatement
	}
	var backendPID any
	if rec.BackendPID != nil {
		backendPID = *rec.BackendPID
	}
	var clientExecutionID any
	if rec.ClientExecutionID != nil && *rec.ClientExecutionID != "" {
		clientExecutionID = *rec.ClientExecutionID
	}
	return []any{
		rec.TargetResourceID, actorUserID, actorMachinePrincipalID, rec.Engine,
		rec.DatabaseName, rec.SchemaName,
		rec.StatementDigest, rec.StatementPreview, fullStatement, string(rec.Status),
		rec.RowCount, rec.DurationMs, rec.ErrorCode, rec.ErrorMessage,
		backendPID, string(rec.RemoteState), clientExecutionID, createdAt,
	}, nil
}

func executionActorArgs(rec model.QueryExecutionRecord) (any, any, error) {
	if (rec.ActorUserID == 0) == (rec.ActorMachinePrincipalID == 0) {
		return nil, nil, errors.New("query execution evidence requires exactly one actor identity")
	}
	if rec.ActorUserID != 0 {
		return rec.ActorUserID, nil, nil
	}
	return nil, rec.ActorMachinePrincipalID, nil
}

// InsertExecutionWithAudit is the repository-owned atomic Execution Evidence
// Pair primitive (Issue #34): it commits one query-execution history row and
// the corresponding fixed audit event in ONE database transaction and returns
// the committed execution id on success. The audit event type is a fixed
// per-caller constant — query.executed for ordinary/paged/template execution,
// related_record_navigation for related-record navigation (Issue #36) — never
// request-controlled. On any failure — including audit insertion failure — the
// whole transaction rolls back so no partial evidence can commit, the
// dimensionless QueryEvidencePersistenceFailures counter is incremented
// exactly once, and one fixed safe log line is emitted. The returned sentinel
// carries no driver/database/statement details; callers surface it as the
// existing controlled backend error. A non-empty ClientExecutionID must take
// the keyed path — FinalizeClaimWithAudit links the claim in the same
// transaction; writing a keyed row here would split that invariant.
func (r *QueryExecutionRepository) InsertExecutionWithAudit(ctx context.Context, rec model.QueryExecutionRecord, eventType, result string) (uint64, error) {
	if rec.ClientExecutionID != nil && *rec.ClientExecutionID != "" {
		return 0, fmt.Errorf("%w: keyed executions must finalize through the claim link", ErrQueryExecutionClaimInvalid)
	}
	args, err := executionRecordArgs(rec)
	if err != nil {
		return 0, err
	}
	actorUserID, actorMachinePrincipalID := args[1], args[2]
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit; safe on error

	res, err := tx.ExecContext(ctx, insertExecutionSQL, args...)
	if err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	id, err := res.LastInsertId()
	if err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	if _, err := tx.ExecContext(ctx, insertExecutionAuditSQL, actorUserID, actorMachinePrincipalID, rec.TargetResourceID, eventType, result); err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	if err := tx.Commit(); err != nil {
		recordQueryEvidencePersistenceFailure()
		return 0, errQueryEvidencePairFailed
	}
	return uint64(id), nil
}

func (r *QueryExecutionRepository) QueryEvidencePersistenceFailures() int64 {
	return QueryEvidencePersistenceFailures.Value()
}

// GetSuccessfulExecutionStatement returns full SQL only when every ownership
// and execution predicate matches. All mismatches deliberately collapse to
// sql.ErrNoRows at the database boundary.
func (r *QueryExecutionRepository) GetSuccessfulExecutionStatement(ctx context.Context, executionID, targetResourceID, actorUserID uint64) (model.QueryExecutionStatementResponse, error) {
	const q = `SELECT full_statement FROM query_executions WHERE id = ? AND target_resource_id = ? AND actor_user_id = ? AND actor_machine_principal_id IS NULL AND status = 'success' AND full_statement IS NOT NULL`
	var response model.QueryExecutionStatementResponse
	err := r.db.QueryRowContext(ctx, q, executionID, targetResourceID, actorUserID).Scan(&response.Statement)
	if errors.Is(err, sql.ErrNoRows) {
		return model.QueryExecutionStatementResponse{}, sql.ErrNoRows
	}
	if err != nil {
		return model.QueryExecutionStatementResponse{}, fmt.Errorf("get successful execution statement: %w", err)
	}
	return response, nil
}

// ListExecutions returns the mixed execution history for a target — persisted
// terminal execution rows unioned with entries derived from claim occupancy
// rows — ordered by real activity time (executions by created_at, claims by
// claimed_at), newest first, plus the total row count for offset pagination.
// Both arms come from ONE consistent union query, so a finalize racing the
// read can never produce a torn claim/execution pair, a duplicated entry, or
// a linked claim surfacing beside its own terminal row.
//
// The claim arm yields unlinked claims plus claims whose stored execution_id
// link is dangling or points at identity-mismatched evidence — the latter
// rows come back flagged (ClaimLinkBroken) for the service to fail closed on,
// never as fabricated running entries. A validly linked claim is excluded
// here: its terminal execution row already represents it.
//
// When q.Actor is non-nil, only that typed identity's rows are included
// (non-admin scope): a user identity binds actor_user_id with the machine
// column NULL, and vice versa — user:7 can never read machine:7. When
// q.Status is set (a stored terminal value), claim-derived entries cannot
// match and the claim arm is omitted entirely.
//
// Pagination mode is selected by q.Mode (set explicitly by the service layer):
//   - PaginationModeCursor: keyset pagination over the mixed ordering
//     (activity_at, kind, ref). Cursor mode never runs COUNT and never uses
//     OFFSET. The caller requests PageSize+1 rows and trims the sentinel.
//     The returned total is always 0 in this mode.
//   - PaginationModeOffset: legacy offset pagination with COUNT and OFFSET.
func (r *QueryExecutionRepository) ListExecutions(ctx context.Context, q model.QueryExecutionListQuery) ([]model.QueryExecutionListItem, int, error) {
	execWhere := `qe.target_resource_id = ?`
	execArgs := []any{q.TargetResourceID}
	claimWhere := `c.target_resource_id = ?`
	claimArgs := []any{q.TargetResourceID}

	if q.Actor != nil {
		if q.Actor.Kind == model.QueryExecutionActorUser {
			execWhere += ` AND qe.actor_user_id = ? AND qe.actor_machine_principal_id IS NULL`
			claimWhere += ` AND c.actor_user_id = ? AND c.actor_machine_principal_id IS NULL`
		} else {
			execWhere += ` AND qe.actor_machine_principal_id = ? AND qe.actor_user_id IS NULL`
			claimWhere += ` AND c.actor_machine_principal_id = ? AND c.actor_user_id IS NULL`
		}
		execArgs = append(execArgs, q.Actor.ID)
		claimArgs = append(claimArgs, q.Actor.ID)
	}
	if q.Status != nil {
		execWhere += ` AND qe.status = ?`
		execArgs = append(execArgs, string(*q.Status))
	}
	if q.From != nil {
		execWhere += ` AND qe.created_at >= ?`
		claimWhere += ` AND c.claimed_at >= ?`
		execArgs = append(execArgs, q.From.UTC())
		claimArgs = append(claimArgs, q.From.UTC())
	}
	if q.To != nil {
		execWhere += ` AND qe.created_at < ?`
		claimWhere += ` AND c.claimed_at < ?`
		execArgs = append(execArgs, q.To.UTC())
		claimArgs = append(claimArgs, q.To.UTC())
	}
	if q.Database != "" {
		// Connection-identity scope is exact on both arms: the claims columns
		// are utf8mb4_0900_bin by DDL, while query_executions.database_name
		// inherits the case/accent-insensitive, pad-folding default — without
		// the explicit collation 'AppDB' rows would match a 'appdb' filter
		// here and not in the claim arm. client_execution_id carries 0900_bin
		// on both tables already; the explicit COLLATE keeps the filter
		// byte-exact at the query level rather than relying on column DDL.
		execWhere += ` AND qe.database_name COLLATE utf8mb4_0900_bin = ?`
		claimWhere += ` AND c.database_name COLLATE utf8mb4_0900_bin = ?`
		execArgs = append(execArgs, q.Database)
		claimArgs = append(claimArgs, q.Database)
	}
	if q.ClientExecutionID != "" {
		execWhere += ` AND qe.client_execution_id COLLATE utf8mb4_0900_bin = ?`
		claimWhere += ` AND c.client_execution_id COLLATE utf8mb4_0900_bin = ?`
		execArgs = append(execArgs, q.ClientExecutionID)
		claimArgs = append(claimArgs, q.ClientExecutionID)
	}

	const execArm = `SELECT 0 AS kind, qe.created_at AS activity_at, qe.id AS ref_num, NULL AS ref_str,
		qe.target_resource_id, qe.actor_user_id, qe.actor_machine_principal_id, qe.engine,
		qe.database_name, qe.schema_name, qe.statement_digest, qe.statement_preview,
		qe.full_statement IS NOT NULL,
		qe.status, qe.row_count, qe.duration_ms, qe.error_code, qe.error_message,
		qe.backend_pid, qe.remote_state, qe.client_execution_id,
		NULL AS attempt_timeout_ms, NULL AS claim_execution_id,
		u.display_name AS user_display_name, mp.name AS machine_name
		FROM query_executions qe
		LEFT JOIN users u ON u.id = qe.actor_user_id
		LEFT JOIN machine_principals mp ON mp.id = qe.actor_machine_principal_id
		WHERE `

	// The claim arm returns only unlinked claims and claims whose link is
	// provably broken: a claim whose execution row exists AND matches every
	// occupancy identity stays out — its terminal row is already listed.
	claimArm := `SELECT 1 AS kind, c.claimed_at AS activity_at, NULL AS ref_num, c.client_execution_id AS ref_str,
		c.target_resource_id, c.actor_user_id, c.actor_machine_principal_id, NULL AS engine,
		c.database_name, c.schema_name, NULL AS statement_digest, NULL AS statement_preview,
		NULL AS has_full,
		NULL AS status, NULL AS row_count, NULL AS duration_ms, NULL AS error_code, NULL AS error_message,
		NULL AS backend_pid, NULL AS remote_state, c.client_execution_id,
		c.attempt_timeout_ms, c.execution_id AS claim_execution_id,
		u.display_name AS user_display_name, mp.name AS machine_name
		FROM query_execution_claims c
		LEFT JOIN query_executions le ON le.id = c.execution_id
		LEFT JOIN users u ON u.id = c.actor_user_id
		LEFT JOIN machine_principals mp ON mp.id = c.actor_machine_principal_id
		WHERE ` + claimWhere + `
		  AND (c.execution_id IS NULL
		       OR le.id IS NULL
		       OR NOT (le.target_resource_id <=> c.target_resource_id)
		       OR NOT (le.actor_user_id <=> c.actor_user_id)
		       OR NOT (le.actor_machine_principal_id <=> c.actor_machine_principal_id)
		       OR le.database_name <> c.database_name
		       OR le.schema_name <> c.schema_name
		       OR NOT (le.client_execution_id <=> c.client_execution_id))`

	union := execArm + execWhere
	args := execArgs
	if q.Status == nil {
		union += ` UNION ALL ` + claimArm
		args = append(args, claimArgs...)
	}

	cursorMode := q.Mode == model.PaginationModeCursor
	var total int

	if !cursorMode {
		countSQL := `SELECT count(*) FROM (` + union + `) u`
		if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count query executions: %w", err)
		}
	}

	listSQL := `SELECT * FROM (` + union + `) u`
	if cursorMode && q.Cursor != nil {
		payload, err := model.DecodeCursor(*q.Cursor)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid cursor: %w", err)
		}
		switch payload.Kind {
		case "e":
			listSQL += ` WHERE (u.activity_at < ? OR (u.activity_at = ? AND u.kind > 0)
				OR (u.activity_at = ? AND u.kind = 0 AND u.ref_num < ?))`
			cursorID, err := strconv.ParseUint(payload.Ref, 10, 64)
			if err != nil {
				return nil, 0, fmt.Errorf("invalid cursor ref: %w", err)
			}
			args = append(args, payload.CreatedAt, payload.CreatedAt, payload.CreatedAt, cursorID)
		case "c":
			listSQL += ` WHERE (u.activity_at < ?
				OR (u.activity_at = ? AND u.kind = 1 AND u.ref_str < ?))`
			args = append(args, payload.CreatedAt, payload.CreatedAt, payload.Ref)
		}
	}
	listSQL += ` ORDER BY u.activity_at DESC, u.kind ASC, u.ref_num DESC, u.ref_str DESC LIMIT ?`

	listArgs := append([]any{}, args...)
	if cursorMode {
		listArgs = append(listArgs, q.PageSize)
	} else {
		offset := (q.Page - 1) * q.PageSize
		listArgs = append(listArgs, q.PageSize, offset)
		listSQL += ` OFFSET ?`
	}

	rows, err := r.db.QueryContext(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list query executions: %w", err)
	}
	defer rows.Close()

	items := make([]model.QueryExecutionListItem, 0)
	for rows.Next() {
		var (
			item            model.QueryExecutionListItem
			kind            int
			activityAt      time.Time
			refNum          nullableUint64
			refStr          sql.NullString
			actorUserID     nullableUint64
			actorMachineID  nullableUint64
			engine          sql.NullString
			digest          sql.NullString
			preview         sql.NullString
			hasFull         sql.NullBool
			status          sql.NullString
			rowCount        sql.NullInt64
			duration        sql.NullInt64
			errCode         sql.NullString
			errMsg          sql.NullString
			remote          sql.NullString
			timeoutMs       nullableUint64
			claimExecID     nullableUint64
			userDisplayName sql.NullString
			machineName     sql.NullString
		)
		if err := rows.Scan(
			&kind, &activityAt, &refNum, &refStr,
			&item.TargetResourceID, &actorUserID, &actorMachineID, &engine,
			&item.DatabaseName, &item.SchemaName, &digest, &preview, &hasFull,
			&status, &rowCount, &duration, &errCode, &errMsg,
			&item.BackendPID, &remote, &item.ClientExecutionID,
			&timeoutMs, &claimExecID,
			&userDisplayName, &machineName,
		); err != nil {
			return nil, 0, fmt.Errorf("scan query execution: %w", err)
		}
		if actorUserID.Valid == actorMachineID.Valid {
			return nil, 0, errors.New("scan query execution: expected exactly one actor identity")
		}
		if actorUserID.Valid {
			item.ActorUserID = actorUserID.Uint64
			item.Actor = model.QueryExecutionActor{Kind: model.QueryExecutionActorUser, DisplayName: model.UnknownHistoryActorDisplayName}
			if userDisplayName.Valid && strings.TrimSpace(userDisplayName.String) != "" {
				item.Actor.DisplayName = userDisplayName.String
			}
		} else {
			item.ActorMachinePrincipalID = actorMachineID.Uint64
			item.Actor = model.QueryExecutionActor{Kind: model.QueryExecutionActorMachine, DisplayName: model.UnknownHistoryMachineActorDisplayName}
			if machineName.Valid && strings.TrimSpace(machineName.String) != "" {
				item.Actor.DisplayName = machineName.String
			}
		}
		if kind == 0 {
			if !refNum.Valid || !status.Valid {
				return nil, 0, errors.New("scan query execution: terminal row lacks identity or status")
			}
			if err := model.ValidateStatus(status.String); err != nil {
				return nil, 0, errors.New("scan query execution: stored status is invalid")
			}
			if err := model.QueryExecutionRemoteState(remote.String).Validate(); err != nil {
				return nil, 0, errors.New("scan query execution: stored remote state is invalid")
			}
			item.Kind = model.QueryExecutionListItemExecution
			id := refNum.Uint64
			item.ID = &id
			item.Engine = engine.String
			item.StatementDigest = digest.String
			item.StatementPreview = preview.String
			item.HasFullStatement = hasFull.Valid && hasFull.Bool
			item.Status = model.QueryExecutionViewStatus(status.String)
			item.RowCount = int(rowCount.Int64)
			item.DurationMs = duration.Int64
			item.ErrorCode = errCode.String
			item.ErrorMessage = errMsg.String
			item.RemoteState = model.QueryExecutionRemoteState(remote.String)
			t := activityAt
			item.CreatedAt = &t
		} else {
			item.Kind = model.QueryExecutionListItemClaim
			if claimExecID.Valid {
				item.ClaimLinkBroken = true
			}
			if timeoutMs.Valid {
				item.ClaimAttemptTimeoutMs = int64(timeoutMs.Uint64)
			}
			t := activityAt
			item.ClaimedAt = &t
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate query executions: %w", err)
	}
	return items, total, nil
}

// InsertAuditEvent records a query.executed audit event for the general event
// stream. Detailed metadata lives in query_executions; this is the cross-cutting
// event record.
func (r *QueryExecutionRepository) InsertAuditEvent(ctx context.Context, actorUserID uint64, targetResourceID uint64, eventType string, result string) error {
	_, err := r.db.ExecContext(ctx, insertAuditEventSQL, actorUserID, targetResourceID, eventType, result)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}
