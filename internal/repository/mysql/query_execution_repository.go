// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, context, errors, fmt, internal/model
// output: NewQueryExecutionRepository, QueryExecutionRepository (credential metadata incl. atomic UpsertCredentialMetadataWithAudit / DeleteCredentialMetadataWithAudit + inTx, execution history, audit events)
// pos: MySQL data access for the Phase 37 read-only query sandbox (query_target_credentials, query_executions, audit_events)
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

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
	upsertCredentialMetadataSQL = `insert into query_target_credentials (resource_id, engine, credential_ref, enabled, environment_policy)
		           values (?, ?, ?, ?, ?)
		           on duplicate key update
		             engine = values(engine),
		             credential_ref = values(credential_ref),
		             enabled = values(enabled),
		             environment_policy = values(environment_policy)`
	deleteCredentialMetadataSQL = `delete from query_target_credentials where resource_id = ?`
	insertAuditEventSQL         = `insert into audit_events (actor_user_id, target_resource_id, event_type, result) values (?, ?, ?, ?)`
)

// GetCredentialByResourceID returns the credential metadata for a query target.
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
func (r *QueryExecutionRepository) GetCredentialByResourceID(ctx context.Context, resourceID uint64) (model.QueryCredentialMetadata, error) {
	const q = `select id, resource_id, engine, credential_ref, enabled, environment_policy
	           from query_target_credentials where resource_id = ?`
	var (
		meta      model.QueryCredentialMetadata
		enabled   bool
		policyStr string
	)
	err := r.db.QueryRowContext(ctx, q, resourceID).Scan(
		&meta.ID, &meta.ResourceID, &meta.Engine, &meta.CredentialRef, &enabled, &policyStr,
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
	if _, err := r.db.ExecContext(ctx, upsertCredentialMetadataSQL,
		meta.ResourceID, meta.Engine, meta.CredentialRef, meta.Enabled, string(meta.EnvironmentPolicy),
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
	return r.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, upsertCredentialMetadataSQL,
			meta.ResourceID, meta.Engine, meta.CredentialRef, meta.Enabled, string(meta.EnvironmentPolicy),
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
func (r *QueryExecutionRepository) DeleteCredentialMetadataWithAudit(ctx context.Context, resourceID, actorUserID uint64, eventType, result string) error {
	return r.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, deleteCredentialMetadataSQL, resourceID); err != nil {
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

// DeleteCredentialByResourceID removes a target's credential metadata. It is
// idempotent: deleting a target that has no row is not an error. It never touches
// a DSN (none is stored) and is the Phase 38A product delete path.
func (r *QueryExecutionRepository) DeleteCredentialByResourceID(ctx context.Context, resourceID uint64) error {
	if _, err := r.db.ExecContext(ctx, deleteCredentialMetadataSQL, resourceID); err != nil {
		return fmt.Errorf("delete query credential metadata: %w", err)
	}
	return nil
}

// InsertExecution persists one execution attempt's metadata and returns its id.
// It stores a digest and short preview only — never full result rows.
// When rec.CreatedAt is zero, the database default (CURRENT_TIMESTAMP) is used.
func (r *QueryExecutionRepository) InsertExecution(ctx context.Context, rec model.QueryExecutionRecord) (uint64, error) {
	var createdAt any
	if !rec.CreatedAt.IsZero() {
		createdAt = rec.CreatedAt.UTC()
	}
	const q = `insert into query_executions
	           (target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, row_count, duration_ms, error_code, error_message, created_at)
	           values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(?, CURRENT_TIMESTAMP(6)))`
	res, err := r.db.ExecContext(ctx, q,
		rec.TargetResourceID, rec.ActorUserID, rec.Engine, rec.StatementDigest, rec.StatementPreview,
		string(rec.Status), rec.RowCount, rec.DurationMs, rec.ErrorCode, rec.ErrorMessage,
		createdAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert query execution: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("query execution last insert id: %w", err)
	}
	return uint64(id), nil
}

// ListExecutions returns execution history (newest first) for a target plus the
// total row count, for pagination. It returns metadata only — never result rows.
// When q.ActorUserID is non-nil, only that actor's rows are included (non-admin
// scope). Actor display names come from a parameterized LEFT JOIN on users;
// missing users project as "Unknown user".
//
// Pagination mode is selected by q.Mode (set explicitly by the service layer):
//   - PaginationModeCursor: keyset pagination over (created_at, id). The first
//     page (Cursor == nil) has no boundary predicate and starts from the newest
//     row. Continuation pages (Cursor != nil) add a strictly-older-than
//     predicate on (created_at, id). Cursor mode never runs COUNT and never
//     uses OFFSET. The caller requests PageSize+1 rows and trims the sentinel.
//     The returned total is always 0 in this mode.
//   - PaginationModeOffset: legacy offset pagination with COUNT and OFFSET.
func (r *QueryExecutionRepository) ListExecutions(ctx context.Context, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, int, error) {
	where := `qe.target_resource_id = ?`
	args := []any{q.TargetResourceID}

	if q.ActorUserID != nil {
		where += ` AND qe.actor_user_id = ?`
		args = append(args, *q.ActorUserID)
	}
	if q.Status != nil {
		where += ` AND qe.status = ?`
		args = append(args, string(*q.Status))
	}
	if q.From != nil {
		where += ` AND qe.created_at >= ?`
		args = append(args, q.From.UTC())
	}
	if q.To != nil {
		where += ` AND qe.created_at < ?`
		args = append(args, q.To.UTC())
	}

	cursorMode := q.Mode == model.PaginationModeCursor
	var total int

	if !cursorMode {
		countSQL := `SELECT count(*) FROM query_executions qe WHERE ` + where
		if err := r.db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("count query executions: %w", err)
		}
	} else if q.Cursor != nil {
		payload, err := model.DecodeCursor(*q.Cursor)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid cursor: %w", err)
		}
		cursorID, err := strconv.ParseUint(payload.ID, 10, 64)
		if err != nil {
			return nil, 0, fmt.Errorf("invalid cursor id: %w", err)
		}
		where += ` AND (qe.created_at < ? OR (qe.created_at = ? AND qe.id < ?))`
		args = append(args, payload.CreatedAt, payload.CreatedAt, cursorID)
	}

	listSQL := `SELECT qe.id, qe.target_resource_id, qe.actor_user_id, qe.engine, qe.statement_digest, qe.statement_preview,
		 qe.status, qe.row_count, qe.duration_ms, qe.error_code, qe.error_message, qe.created_at,
		 COALESCE(NULLIF(TRIM(u.display_name), ''), ?) AS actor_display_name
		 FROM query_executions qe
		 LEFT JOIN users u ON u.id = qe.actor_user_id
		 WHERE ` + where + ` ORDER BY qe.created_at DESC, qe.id DESC LIMIT ?`

	listArgs := make([]any, 0, len(args)+2)
	listArgs = append(listArgs, model.UnknownHistoryActorDisplayName)
	listArgs = append(listArgs, args...)

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

	items := make([]model.QueryExecutionRecord, 0)
	for rows.Next() {
		var (
			rec         model.QueryExecutionRecord
			status      string
			displayName string
		)
		if err := rows.Scan(
			&rec.ID, &rec.TargetResourceID, &rec.ActorUserID, &rec.Engine,
			&rec.StatementDigest, &rec.StatementPreview, &status,
			&rec.RowCount, &rec.DurationMs, &rec.ErrorCode, &rec.ErrorMessage, &rec.CreatedAt,
			&displayName,
		); err != nil {
			return nil, 0, fmt.Errorf("scan query execution: %w", err)
		}
		rec.Status = model.QueryExecutionStatus(status)
		rec.Actor = model.QueryExecutionActor{DisplayName: displayName}
		items = append(items, rec)
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
