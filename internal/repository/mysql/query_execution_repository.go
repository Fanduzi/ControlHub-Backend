// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, context, errors, fmt, internal/model
// output: NewQueryExecutionRepository, QueryExecutionRepository (credential metadata + execution history + audit events)
// pos: MySQL data access for the Phase 37 read-only query sandbox (query_target_credentials, query_executions, audit_events)
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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

// GetCredentialByResourceID returns the credential metadata for a query target.
// It returns sql.ErrNoRows when no row exists. It runs the row's credential_ref
// through model.ValidateCredentialRef on read: an invalid ref fails closed
// (returns an error) so the resolver never performs an env lookup with an
// unvalidated key. The environment_policy is returned as the typed enum.
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
	if vErr := model.ValidateCredentialRef(meta.CredentialRef); vErr != nil {
		// Fail closed: never surface an invalid ref to the resolver/env lookup.
		return model.QueryCredentialMetadata{}, fmt.Errorf("credential_ref for resource %d is invalid: %w", resourceID, vErr)
	}
	meta.Enabled = enabled
	meta.EnvironmentPolicy = model.QueryEnvironmentPolicy(policyStr)
	return meta, nil
}

// UpsertCredentialMetadata writes or replaces a target's credential metadata.
// It is the local/dev seed write path (cmd/querydev). It stores
// resource_id, engine, credential_ref, enabled, and environment_policy ONLY —
// never a DSN or password. The upsert keys on the existing unique
// (resource_id) index so repeated local/dev seeding is idempotent: re-running
// the seed against the same target refreshes its metadata instead of failing
// on the duplicate. The credential_ref is validated upstream by the seed
// service; reads re-validate it fail-closed via model.ValidateCredentialRef.
func (r *QueryExecutionRepository) UpsertCredentialMetadata(ctx context.Context, meta model.QueryCredentialMetadata) error {
	const q = `insert into query_target_credentials (resource_id, engine, credential_ref, enabled, environment_policy)
	           values (?, ?, ?, ?, ?)
	           on duplicate key update
	             engine = values(engine),
	             credential_ref = values(credential_ref),
	             enabled = values(enabled),
	             environment_policy = values(environment_policy)`
	if _, err := r.db.ExecContext(ctx, q,
		meta.ResourceID, meta.Engine, meta.CredentialRef, meta.Enabled, string(meta.EnvironmentPolicy),
	); err != nil {
		return fmt.Errorf("upsert query credential metadata: %w", err)
	}
	return nil
}

// InsertExecution persists one execution attempt's metadata and returns its id.
// It stores a digest and short preview only — never full result rows.
func (r *QueryExecutionRepository) InsertExecution(ctx context.Context, rec model.QueryExecutionRecord) (uint64, error) {
	const q = `insert into query_executions
	           (target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, row_count, duration_ms, error_code, error_message)
	           values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := r.db.ExecContext(ctx, q,
		rec.TargetResourceID, rec.ActorUserID, rec.Engine, rec.StatementDigest, rec.StatementPreview,
		string(rec.Status), rec.RowCount, rec.DurationMs, rec.ErrorCode, rec.ErrorMessage,
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
func (r *QueryExecutionRepository) ListExecutions(ctx context.Context, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx,
		`select count(*) from query_executions where target_resource_id = ?`,
		q.TargetResourceID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count query executions: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	rows, err := r.db.QueryContext(ctx,
		`select id, target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, row_count, duration_ms, error_code, error_message, created_at
		 from query_executions where target_resource_id = ? order by created_at desc limit ? offset ?`,
		q.TargetResourceID, q.PageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list query executions: %w", err)
	}
	defer rows.Close()

	items := make([]model.QueryExecutionRecord, 0)
	for rows.Next() {
		var (
			rec    model.QueryExecutionRecord
			status string
		)
		if err := rows.Scan(
			&rec.ID, &rec.TargetResourceID, &rec.ActorUserID, &rec.Engine,
			&rec.StatementDigest, &rec.StatementPreview, &status,
			&rec.RowCount, &rec.DurationMs, &rec.ErrorCode, &rec.ErrorMessage, &rec.CreatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan query execution: %w", err)
		}
		rec.Status = model.QueryExecutionStatus(status)
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
	_, err := r.db.ExecContext(ctx,
		`insert into audit_events (actor_user_id, target_resource_id, event_type, result) values (?, ?, ?, ?)`,
		actorUserID, targetResourceID, eventType, result,
	)
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}