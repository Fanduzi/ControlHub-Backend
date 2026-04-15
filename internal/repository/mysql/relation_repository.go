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
	var id string
	if err := r.db.QueryRowContext(ctx, "SELECT UUID()").Scan(&id); err != nil {
		return nil, fmt.Errorf("generate id: %w", err)
	}

	query := `insert into resource_relations (id, from_resource_id, to_resource_id, relation_type, created_at)
	values (?, ?, ?, ?, NOW())`

	_, err := r.db.ExecContext(ctx, query, id, input.FromResourceID, input.ToResourceID, input.RelationType)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return nil, service.ErrRelationConflict
		}
		return nil, fmt.Errorf("insert relation: %w", err)
	}

	return &model.ResourceRelation{
		ID:             id,
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

func (r *RelationRepository) ListRelationsByResourceIDs(ids []string) ([]model.ResourceRelation, error) {
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
