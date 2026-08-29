// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewAuditRepository, user/machine-aware AuditRepository list and resource-environment projections
// pos: MySQL audit_events pagination, filtering, scan, and user/machine/resource search
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/fan/controlhub/internal/model"
)

type AuditRepository struct {
	db *sql.DB
}

func NewAuditRepository(db *sql.DB) *AuditRepository {
	return &AuditRepository{db: db}
}

func (r *AuditRepository) ListAuditEvents(ctx context.Context, q model.AuditListQuery) ([]model.AuditEvent, int, error) {
	var conds []string
	var args []any
	from := `audit_events
	left join users u on u.id = audit_events.actor_user_id
	left join machine_principals mp on mp.id = audit_events.actor_machine_principal_id
	left join resources r on r.id = audit_events.target_resource_id`

	if q.TargetResourceID != nil {
		conds = append(conds, "target_resource_id = ?")
		args = append(args, *q.TargetResourceID)
	}
	if q.EnvironmentID != nil {
		conds = append(conds, "r.environment_id = ?")
		args = append(args, *q.EnvironmentID)
	}
	if len(q.EventTypes) > 0 {
		ph := buildInClause(len(q.EventTypes))
		conds = append(conds, "event_type in ("+ph+")")
		for _, v := range q.EventTypes {
			args = append(args, v)
		}
	}
	if len(q.Results) > 0 {
		ph := buildInClause(len(q.Results))
		conds = append(conds, "result in ("+ph+")")
		for _, v := range q.Results {
			args = append(args, v)
		}
	}
	query := strings.TrimSpace(q.Query)
	if query != "" {
		pattern := "%" + query + "%"
		conds = append(conds, "(u.display_name like ? or u.email like ? or mp.name like ? or r.name like ? or r.display_name like ?)")
		args = append(args, pattern, pattern, pattern, pattern, pattern)
	}

	where := ""
	if len(conds) > 0 {
		where = "where " + strings.Join(conds, " and ")
	}

	var total int
	countQuery := "select count(*) from " + from + " " + where
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	dataQuery := `select audit_events.id, audit_events.actor_user_id, audit_events.actor_machine_principal_id, audit_events.target_resource_id,
		audit_events.event_type, audit_events.result, audit_events.changes, audit_events.created_at
	from ` + from + " " + where + ` order by audit_events.created_at desc limit ? offset ?`

	dataArgs := append(args, q.PageSize, offset)
	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanAuditEventsRows(rows, total)
}

func (r *AuditRepository) ListByResourceID(resourceID uint64) ([]model.AuditEvent, error) {
	query := `
	select id, actor_user_id, actor_machine_principal_id, target_resource_id, event_type, result, changes, created_at
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

type nullableUint64 struct {
	Uint64 uint64
	Valid  bool
}

func (n *nullableUint64) Scan(value any) error {
	if value == nil {
		n.Uint64 = 0
		n.Valid = false
		return nil
	}

	switch v := value.(type) {
	case uint64:
		n.Uint64 = v
	case int64:
		if v < 0 {
			return fmt.Errorf("scan nullable uint64: negative value %d", v)
		}
		n.Uint64 = uint64(v)
	case []byte:
		parsed, err := strconv.ParseUint(string(v), 10, 64)
		if err != nil {
			return fmt.Errorf("scan nullable uint64 from bytes: %w", err)
		}
		n.Uint64 = parsed
	case string:
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return fmt.Errorf("scan nullable uint64 from string: %w", err)
		}
		n.Uint64 = parsed
	default:
		return fmt.Errorf("scan nullable uint64: unsupported type %T", value)
	}

	n.Valid = true
	return nil
}

func scanAuditEventsRows(rows *sql.Rows, total int) ([]model.AuditEvent, int, error) {
	items := make([]model.AuditEvent, 0)
	for rows.Next() {
		var item model.AuditEvent
		var targetResourceID nullableUint64
		var actorUserID nullableUint64
		var actorMachinePrincipalID nullableUint64
		var rawChanges sql.NullString
		if err := rows.Scan(
			&item.ID,
			&actorUserID,
			&actorMachinePrincipalID,
			&targetResourceID,
			&item.EventType,
			&item.Result,
			&rawChanges,
			&item.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if actorUserID.Valid {
			aid := actorUserID.Uint64
			item.ActorUserID = &aid
		}
		if actorMachinePrincipalID.Valid {
			id := actorMachinePrincipalID.Uint64
			item.ActorMachinePrincipalID = &id
		}
		if item.ActorUserID != nil && item.ActorMachinePrincipalID != nil {
			return nil, 0, fmt.Errorf("scan audit event: multiple actor identities")
		}
		if targetResourceID.Valid {
			targetID := targetResourceID.Uint64
			item.TargetResourceID = &targetID
		}
		if rawChanges.Valid {
			if err := json.Unmarshal([]byte(rawChanges.String), &item.Changes); err != nil {
				return nil, 0, fmt.Errorf("decode audit changes: %w", err)
			}
		}
		items = append(items, item)
	}

	return items, total, rows.Err()
}
