// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewAuditRepository, AuditRepository struct
// pos: MySQL data access for audit_events table with pagination and filtering
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

type AuditRepository struct {
	db *sql.DB
}

func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) ListAuditEvents(ctx context.Context, q model.AuditListQuery) ([]model.AuditEvent, int, error) {
	where := `where (? = '' or target_resource_id = ?)
	  and (? = '' or event_type = ?)
	  and (? = '' or result = ?)`

	filterArgs := []any{
		q.TargetResourceID, q.TargetResourceID,
		q.EventType, q.EventType,
		q.Result, q.Result,
	}

	// Count query
	var total int
	countQuery := "select count(*) from audit_events " + where
	if err := r.db.QueryRowContext(ctx, countQuery, filterArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}

	// Data query
	offset := (q.Page - 1) * q.PageSize
	dataQuery := `select id, actor_user_id, coalesce(target_resource_id, ''), event_type, result, created_at
	from audit_events ` + where + ` order by created_at desc limit ? offset ?`

	dataArgs := append(filterArgs, q.PageSize, offset)
	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanAuditEventsRows(rows, total)
}

func (r *AuditRepository) ListByResourceID(resourceID string) ([]model.AuditEvent, error) {
	query := `
	select id, actor_user_id, coalesce(target_resource_id, ''), event_type, result, created_at
	from audit_events
	where target_resource_id = ?
	order by created_at desc`

	rows, err := r.db.QueryContext(context.Background(), query, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items, _, err := scanAuditEventsRows(rows, 0)
	return items, err
}

func scanAuditEventsRows(rows *sql.Rows, total int) ([]model.AuditEvent, int, error) {
	items := make([]model.AuditEvent, 0)
	for rows.Next() {
		var item model.AuditEvent
		if err := rows.Scan(
			&item.ID,
			&item.ActorUserID,
			&item.TargetResourceID,
			&item.EventType,
			&item.Result,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
