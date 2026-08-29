// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewRelationRepository, RelationRepository struct
// pos: MySQL data access for resource_relations table
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type RelationRepository struct {
	db *sql.DB
}

func NewRelationRepository(db *sql.DB) *RelationRepository {
	return &RelationRepository{db: db}
}

func (r *RelationRepository) ListByResourceID(resourceID uint64) ([]model.ResourceRelation, error) {
	query := `
	select id, from_resource_id, to_resource_id, relation_type, created_at
	from resource_relations
	where from_resource_id = ? or to_resource_id = ?
	order by created_at desc`

	rows, err := r.db.QueryContext(context.Background(), query, resourceID, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ResourceRelation, 0)
	for rows.Next() {
		var item model.ResourceRelation
		if err := rows.Scan(
			&item.ID,
			&item.FromResourceID,
			&item.ToResourceID,
			&item.RelationType,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *RelationRepository) ListRelationViewsByResourceID(resourceID uint64) ([]model.ResourceRelationView, error) {
	query := `
	select rr.id, rr.from_resource_id, rr.to_resource_id, rr.relation_type, rr.created_at,
	       rel.id, rel.name, rel.display_name, rel.resource_type, rel.resource_subtype,
	       rel.health_status, rel.lifecycle_status
	from resource_relations rr
	join resources rel on (
	    case when rr.from_resource_id = ? then rel.id = rr.to_resource_id
	         else rel.id = rr.from_resource_id end
	)
	where rr.from_resource_id = ? or rr.to_resource_id = ?
	order by rr.created_at desc`

	rows, err := r.db.QueryContext(context.Background(), query, resourceID, resourceID, resourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ResourceRelationView, 0)
	for rows.Next() {
		var item model.ResourceRelationView
		if err := rows.Scan(
			&item.ID,
			&item.FromResourceID,
			&item.ToResourceID,
			&item.RelationType,
			&item.CreatedAt,
			&item.RelatedResourceID,
			&item.RelatedResourceName,
			&item.RelatedResourceDisplayName,
			&item.RelatedResourceType,
			&item.RelatedResourceSubtype,
			&item.RelatedResourceHealthStatus,
			&item.RelatedResourceLifecycleStat,
		); err != nil {
			return nil, err
		}
		if item.FromResourceID == resourceID {
			item.Direction = "outgoing"
		} else {
			item.Direction = "incoming"
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *RelationRepository) ListClusterMembers(clusterID uint64) ([]model.ClusterMemberView, error) {
	query := `
	select m.id, m.name, m.display_name, m.resource_type, m.resource_subtype,
	       m.lifecycle_status, m.health_status
	from resource_relations rr
	join resources m on m.id = rr.from_resource_id
	where rr.to_resource_id = ? and rr.relation_type = 'member_of'
	order by m.name`

	rows, err := r.db.QueryContext(context.Background(), query, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ClusterMemberView, 0)
	for rows.Next() {
		var item model.ClusterMemberView
		if err := rows.Scan(
			&item.ResourceID,
			&item.Name,
			&item.DisplayName,
			&item.ResourceType,
			&item.ResourceSubtype,
			&item.LifecycleStatus,
			&item.HealthStatus,
		); err != nil {
			return nil, err
		}
		item.ProfileSummary = r.fetchMemberProfileSummary(item.ResourceID, item.ResourceType)
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *RelationRepository) fetchMemberProfileSummary(resourceID uint64, resourceType string) *model.ProfileSummary {
	ctx := context.Background()
	switch resourceType {
	case "database_instance":
		return r.fetchInstanceProfileSummary(ctx, resourceID)
	case "host":
		return r.fetchHostProfileSummary(ctx, resourceID)
	default:
		return nil
	}
}

func (r *RelationRepository) fetchInstanceProfileSummary(ctx context.Context, id uint64) *model.ProfileSummary {
	var engine, version, host, role string
	var port int
	err := r.db.QueryRowContext(ctx,
		`select engine, version, host, port, role from resource_profiles_database_instance where resource_id = ?`,
		id,
	).Scan(&engine, &version, &host, &port, &role)
	if err != nil {
		return nil
	}
	return &model.ProfileSummary{
		Engine:   engine,
		Version:  version,
		Hostname: host,
		Port:     port,
		Role:     role,
	}
}

func (r *RelationRepository) fetchHostProfileSummary(ctx context.Context, id uint64) *model.ProfileSummary {
	var hostname, ipAddress string
	err := r.db.QueryRowContext(ctx,
		`select hostname, ip_address from resource_profiles_host where resource_id = ?`,
		id,
	).Scan(&hostname, &ipAddress)
	if err != nil {
		return nil
	}
	return &model.ProfileSummary{
		Hostname: hostname,
		IP:       ipAddress,
	}
}

func (r *RelationRepository) GetResource(id uint64) (*model.Resource, error) {
	query := "select " + resourceColumns + " from resources where id = ?"

	row := r.db.QueryRowContext(context.Background(), query, id)

	item, err := scanResource(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrResourceNotFound
		}
		return nil, err
	}
	return &item, nil
}

func (r *RelationRepository) CreateRelation(ctx context.Context, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	return insertRelation(ctx, r.db, input)
}

func insertRelation(ctx context.Context, execer sqlExecer, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	query := `insert into resource_relations (from_resource_id, to_resource_id, relation_type, created_at)
	values (?, ?, ?, NOW())`

	result, err := execer.ExecContext(ctx, query, input.FromResourceID, input.ToResourceID, input.RelationType)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return nil, service.ErrRelationConflict
		}
		return nil, fmt.Errorf("insert relation: %w", err)
	}

	insertID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("relation last insert id: %w", err)
	}

	return &model.ResourceRelation{
		ID:             uint64(insertID),
		FromResourceID: input.FromResourceID,
		ToResourceID:   input.ToResourceID,
		RelationType:   input.RelationType,
		CreatedAt:      time.Now(),
	}, nil
}

func (r *RelationRepository) CreateRelationWithAudit(ctx context.Context, input model.RelationCreateInput, actorUserID uint64, eventType string) (*model.ResourceRelation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin inventory relationship transaction: %w", err)
	}
	defer tx.Rollback()
	created, err := insertRelation(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	field := "relationships." + string(input.RelationType)
	if err := insertInventoryAuditTx(ctx, tx, actorUserID, input.FromResourceID, eventType, []model.AuditChange{{
		Field: field, Operation: model.AuditChangeAdd,
		After: map[string]any{"relatedResourceId": input.ToResourceID, "direction": "outgoing"},
	}}); err != nil {
		return nil, err
	}
	if err := insertInventoryAuditTx(ctx, tx, actorUserID, input.ToResourceID, eventType, []model.AuditChange{{
		Field: field, Operation: model.AuditChangeAdd,
		After: map[string]any{"relatedResourceId": input.FromResourceID, "direction": "incoming"},
	}}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit inventory relationship transaction: %w", err)
	}
	return created, nil
}

func (r *RelationRepository) DeleteRelation(ctx context.Context, relationID uint64) error {
	result, err := r.db.ExecContext(ctx, `delete from resource_relations where id = ?`, relationID)
	if err != nil {
		return fmt.Errorf("delete relation: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return service.ErrRelationNotFound
	}
	return nil
}

func (r *RelationRepository) DeleteRelationWithAudit(ctx context.Context, relationID, actorUserID uint64, eventType string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin inventory relationship transaction: %w", err)
	}
	defer tx.Rollback()
	var relation model.ResourceRelation
	if err := tx.QueryRowContext(ctx, `select id, from_resource_id, to_resource_id, relation_type, created_at
		from resource_relations where id = ? for update`, relationID).Scan(
		&relation.ID, &relation.FromResourceID, &relation.ToResourceID, &relation.RelationType, &relation.CreatedAt,
	); errors.Is(err, sql.ErrNoRows) {
		return service.ErrRelationNotFound
	} else if err != nil {
		return fmt.Errorf("lock relation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `delete from resource_relations where id = ?`, relationID); err != nil {
		return fmt.Errorf("delete relation: %w", err)
	}
	field := "relationships." + string(relation.RelationType)
	if err := insertInventoryAuditTx(ctx, tx, actorUserID, relation.FromResourceID, eventType, []model.AuditChange{{
		Field: field, Operation: model.AuditChangeRemove,
		Before: map[string]any{"relatedResourceId": relation.ToResourceID, "direction": "outgoing"},
	}}); err != nil {
		return err
	}
	if err := insertInventoryAuditTx(ctx, tx, actorUserID, relation.ToResourceID, eventType, []model.AuditChange{{
		Field: field, Operation: model.AuditChangeRemove,
		Before: map[string]any{"relatedResourceId": relation.FromResourceID, "direction": "incoming"},
	}}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit inventory relationship transaction: %w", err)
	}
	return nil
}

func (r *RelationRepository) ListRelationsByResourceIDs(ids []uint64) ([]model.ResourceRelation, error) {
	if len(ids) == 0 {
		return []model.ResourceRelation{}, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids)*2)
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
		args[len(ids)+i] = id
	}

	query := fmt.Sprintf(`
	select id, from_resource_id, to_resource_id, relation_type, created_at
	from resource_relations
	where from_resource_id in (%s) or to_resource_id in (%s)
	order by created_at desc`,
		strings.Join(placeholders, ", "),
		strings.Join(placeholders, ", "),
	)

	rows, err := r.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ResourceRelation, 0)
	for rows.Next() {
		var item model.ResourceRelation
		if err := rows.Scan(
			&item.ID,
			&item.FromResourceID,
			&item.ToResourceID,
			&item.RelationType,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
