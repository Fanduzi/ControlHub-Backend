// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, context, internal/model, internal/service
// output: NewQueryTargetRepository, QueryTargetRepository (implements service.QueryTargetRepository)
// pos: Read-only query target read model — joins database_instance resources with profiles, environments, owners, and cluster membership
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// QueryTargetRepository reads the query target read model from MySQL. It
// satisfies service.QueryTargetRepository. It is strictly read-only: it never
// mutates resources, profiles, or topology.
type QueryTargetRepository struct {
	db *sql.DB
}

func NewQueryTargetRepository(db *sql.DB) *QueryTargetRepository {
	return &QueryTargetRepository{db: db}
}

// ListQueryTargets returns database_instance resources as raw query targets
// (identity + connection context only). Engine comes from the profile when
// present and falls back to resource_subtype otherwise, so an instance with a
// missing profile still reports an identifiable engine and surfaces as
// missing_connection (host/port gap) instead of disappearing. Host/port still
// come only from the profile. Cluster membership is resolved through the
// member_of relation the same way the resource list resolves cluster_id.
func (r *QueryTargetRepository) ListQueryTargets(ctx context.Context, q model.QueryTargetListQuery) ([]model.QueryTarget, error) {
	// resolvedEngine = profile engine, falling back to resource_subtype when the
	// profile row is missing. Used for both the returned engine and the engine
	// filter so a no-profile mysql instance stays visible under ?engine=mysql.
	const resolvedEngine = "coalesce(nullif(p.engine, ''), r.resource_subtype)"

	where := "where r.resource_type = 'database_instance' and r.archived_at is null"
	args := []any{}
	if q.Engine != "" {
		where += " and lower(" + resolvedEngine + ") = ?"
		args = append(args, q.Engine)
	}
	if q.EnvironmentID != 0 {
		where += " and r.environment_id = ?"
		args = append(args, q.EnvironmentID)
	}

	query := `select
		r.id, r.name, r.display_name,
		e.name, o.name,
		` + resolvedEngine + `, p.host, p.port,
		(select rr.to_resource_id from resource_relations rr
		 where rr.from_resource_id = r.id and rr.relation_type = 'member_of' limit 1) as cluster_id,
		(select c.display_name from resource_relations rr
		 join resources c on c.id = rr.to_resource_id
		 where rr.from_resource_id = r.id and rr.relation_type = 'member_of' limit 1) as cluster_name
	from resources r
	left join resource_profiles_database_instance p on p.resource_id = r.id
	left join environments e on e.id = r.environment_id
	left join owners o on o.id = r.owner_id
	` + where + `
	order by r.name`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list query targets: %w", err)
	}
	defer rows.Close()

	items := make([]model.QueryTarget, 0)
	for rows.Next() {
		var (
			id                uint64
			name              string
			displayName       string
			environmentName   sql.NullString
			ownerName         sql.NullString
			engine            sql.NullString
			host              sql.NullString
			port              sql.NullInt64
			clusterID         sql.NullInt64
			clusterName       sql.NullString
		)
		if err := rows.Scan(
			&id, &name, &displayName,
			&environmentName, &ownerName,
			&engine, &host, &port,
			&clusterID, &clusterName,
		); err != nil {
			return nil, fmt.Errorf("scan query target: %w", err)
		}

		target := model.QueryTarget{
			ResourceID:   id,
			ResourceName: name,
			DisplayName:  displayName,
			ResourceType: model.ResourceTypeDatabaseInstance,
			ConnectionContext: model.QueryTargetConnectionContext{
				Environment: environmentName.String,
				Owner:       ownerName.String,
				Engine:      engine.String,
				Host:        host.String,
				Port:        int(port.Int64),
			},
		}
		if clusterID.Valid {
			target.ConnectionContext.ClusterID = uint64(clusterID.Int64)
			target.ConnectionContext.ClusterName = clusterName.String
		}
		items = append(items, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate query targets: %w", err)
	}
	return items, nil
}
