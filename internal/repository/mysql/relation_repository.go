// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewRelationRepository, RelationRepository struct
// pos: MySQL data access for resource_relations table
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type RelationRepository struct {
	db *sql.DB
}

func NewRelationRepository(db *sql.DB) *RelationRepository {
	return &RelationRepository{db: db}
}

func (r *RelationRepository) ListByResourceID(resourceID string) ([]model.ResourceRelation, error) {
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

func (r *RelationRepository) GetResource(id string) (*model.Resource, error) {
	query := `
	select id, resource_type, resource_subtype, name, display_name,
	       environment_id, owner_id, lifecycle_status, health_status,
	       source, external_id, labels, created_at, updated_at
	from resources
	where id = ?`

	row := r.db.QueryRowContext(context.Background(), query, id)

	var (
		item      model.Resource
		rawLabels string
	)
	err := row.Scan(
		&item.ID, &item.ResourceType, &item.ResourceSubtype,
		&item.Name, &item.DisplayName,
		&item.EnvironmentID, &item.OwnerID,
		&item.LifecycleStatus, &item.HealthStatus,
		&item.Source, &item.ExternalID,
		&rawLabels, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrResourceNotFound
	}
	if err != nil {
		return nil, err
	}
	if rawLabels != "" && rawLabels != "null" {
		_ = json.Unmarshal([]byte(rawLabels), &item.Labels)
	}
	if item.Labels == nil {
		item.Labels = map[string]string{}
	}
	return &item, nil
}

func (r *RelationRepository) CreateRelation(ctx context.Context, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	query := `insert into resource_relations (id, from_resource_id, to_resource_id, relation_type, created_at)
	values (UUID(), ?, ?, ?, NOW())`

	result, err := r.db.ExecContext(ctx, query, input.FromResourceID, input.ToResourceID, input.RelationType)
	if err != nil {
		return nil, fmt.Errorf("insert relation: %w", err)
	}

	id, _ := result.LastInsertId()
	return &model.ResourceRelation{
		ID:             fmt.Sprintf("%d", id),
		FromResourceID: input.FromResourceID,
		ToResourceID:   input.ToResourceID,
		RelationType:   input.RelationType,
		CreatedAt:      time.Now(),
	}, nil
}

func (r *RelationRepository) DeleteRelation(ctx context.Context, relationID string) error {
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
