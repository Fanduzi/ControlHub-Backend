// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model, internal/service
// output: NewResourceRepository, ResourceRepository struct (implements service.ResourceRepository)
// pos: MySQL data access for core resource table with pagination and filtering
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type ResourceRepository struct {
	db *sql.DB
}

func NewResourceRepository(db *sql.DB) *ResourceRepository {
	return &ResourceRepository{db: db}
}

const resourceColumns = `id, resource_type, resource_subtype, name, display_name,
       environment_id, owner_id, lifecycle_status, health_status,
       source, external_id, labels, created_at, updated_at,
       archived_at, archived_by, archive_reason`

func (r *ResourceRepository) ListResources(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, int, error) {
	var conds []string
	var args []any

	if len(q.ResourceTypes) > 0 {
		ph := buildInClause(len(q.ResourceTypes))
		conds = append(conds, "resource_type in ("+ph+")")
		for _, v := range q.ResourceTypes {
			args = append(args, v)
		}
	}
	if len(q.EnvironmentIDs) > 0 {
		ph := buildInClause(len(q.EnvironmentIDs))
		conds = append(conds, "environment_id in ("+ph+")")
		for _, v := range q.EnvironmentIDs {
			args = append(args, v)
		}
	}
	if len(q.LifecycleStatus) > 0 {
		ph := buildInClause(len(q.LifecycleStatus))
		conds = append(conds, "lifecycle_status in ("+ph+")")
		for _, v := range q.LifecycleStatus {
			args = append(args, v)
		}
	}
	if len(q.HealthStatuses) > 0 {
		ph := buildInClause(len(q.HealthStatuses))
		conds = append(conds, "health_status in ("+ph+")")
		for _, v := range q.HealthStatuses {
			args = append(args, v)
		}
	}
	if len(q.ResourceSubtypes) > 0 {
		ph := buildInClause(len(q.ResourceSubtypes))
		conds = append(conds, "resource_subtype in ("+ph+")")
		for _, v := range q.ResourceSubtypes {
			args = append(args, v)
		}
	}
	if q.Query != "" {
		pattern := "%" + q.Query + "%"
		conds = append(conds, "(name like ? or display_name like ? or external_id like ?)")
		args = append(args, pattern, pattern, pattern)
	}

	if q.ArchivedOnly {
		conds = append(conds, "archived_at is not null")
	} else if !q.IncludeArchived {
		conds = append(conds, "archived_at is null")
	}

	where := ""
	if len(conds) > 0 {
		where = "where " + strings.Join(conds, " and ")
	}

	var total int
	countQuery := "select count(*) from resources " + where
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count resources: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	dataQuery := `select r.id, r.resource_type, r.resource_subtype, r.name, r.display_name,
       r.environment_id, r.owner_id, r.lifecycle_status, r.health_status,
       r.source, r.external_id, r.labels, r.created_at, r.updated_at,
       r.archived_at, r.archived_by, r.archive_reason,
       (select rr.to_resource_id from resource_relations rr
        where rr.from_resource_id = r.id and rr.relation_type = 'member_of'
        limit 1) as cluster_id
from resources r ` + where + " order by r.name limit ? offset ?"

	dataArgs := append(args, q.PageSize, offset)
	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]model.Resource, 0)
	for rows.Next() {
		var (
			item          model.Resource
			rawLabels     string
			archivedAt    sql.NullTime
			archivedBy    sql.NullInt64
			archiveReason sql.NullString
			clusterId     sql.NullInt64
		)

		err := rows.Scan(
			&item.ID,
			&item.ResourceType,
			&item.ResourceSubtype,
			&item.Name,
			&item.DisplayName,
			&item.EnvironmentID,
			&item.OwnerID,
			&item.LifecycleStatus,
			&item.HealthStatus,
			&item.Source,
			&item.ExternalID,
			&rawLabels,
			&item.CreatedAt,
			&item.UpdatedAt,
			&archivedAt,
			&archivedBy,
			&archiveReason,
			&clusterId,
		)
		if err != nil {
			return nil, 0, err
		}

		if archivedAt.Valid {
			item.ArchivedAt = &archivedAt.Time
		}
		if archivedBy.Valid {
			v := uint64(archivedBy.Int64)
			item.ArchivedBy = &v
		}
		if archiveReason.Valid {
			item.ArchiveReason = &archiveReason.String
		}
		if clusterId.Valid {
			v := uint64(clusterId.Int64)
			item.ClusterId = &v
		}

		if rawLabels == "" || rawLabels == "null" {
			item.Labels = map[string]string{}
		} else if err := json.Unmarshal([]byte(rawLabels), &item.Labels); err != nil {
			return nil, 0, err
		}

		items = append(items, item)
	}

	return items, total, rows.Err()
}

// buildInClause returns a parameterized placeholder string like "?, ?, ?" for n values.
func buildInClause(n int) string {
	if n <= 0 {
		return ""
	}
	placeholders := make([]string, n)
	for i := range n {
		placeholders[i] = "?"
	}
	return strings.Join(placeholders, ", ")
}

func (r *ResourceRepository) GetResource(id uint64) (*model.Resource, error) {
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

func (r *ResourceRepository) GetResourceProfile(id uint64) (*model.ResourceProfileResponse, error) {
	res, err := r.GetResource(id)
	if err != nil {
		return nil, err
	}

	profile, err := r.fetchProfile(res.ID, res.ResourceType)
	if err != nil {
		return nil, err
	}

	return &model.ResourceProfileResponse{
		ResourceID:      res.ID,
		ResourceType:    res.ResourceType,
		ResourceSubtype: res.ResourceSubtype,
		Profile:         profile,
	}, nil
}

func (r *ResourceRepository) fetchProfile(resourceID uint64, resourceType model.ResourceType) (map[string]any, error) {
	ctx := context.Background()

	switch resourceType {
	case model.ResourceTypeDatabaseInstance:
		return r.fetchDatabaseInstanceProfile(ctx, resourceID)
	case model.ResourceTypeDatabaseCluster:
		return r.fetchDatabaseClusterProfile(ctx, resourceID)
	case model.ResourceTypeService:
		return r.fetchServiceProfile(ctx, resourceID)
	case model.ResourceTypeHost:
		return r.fetchHostProfile(ctx, resourceID)
	default:
		return map[string]any{}, nil
	}
}

func (r *ResourceRepository) fetchDatabaseInstanceProfile(ctx context.Context, id uint64) (map[string]any, error) {
	var engine, version, host, role string
	var port int
	err := r.db.QueryRowContext(ctx,
		`select engine, version, host, port, role from resource_profiles_database_instance where resource_id = ?`,
		id,
	).Scan(&engine, &version, &host, &port, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch database_instance profile: %w", err)
	}
	return map[string]any{
		"engine":  engine,
		"version": version,
		"host":    host,
		"port":    port,
		"role":    role,
	}, nil
}

func (r *ResourceRepository) fetchDatabaseClusterProfile(ctx context.Context, id uint64) (map[string]any, error) {
	var engine, topologyMode, primaryEndpoint string
	err := r.db.QueryRowContext(ctx,
		`select engine, topology_mode, primary_endpoint from resource_profiles_database_cluster where resource_id = ?`,
		id,
	).Scan(&engine, &topologyMode, &primaryEndpoint)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch database_cluster profile: %w", err)
	}
	return map[string]any{
		"engine":          engine,
		"topologyMode":    topologyMode,
		"primaryEndpoint": primaryEndpoint,
	}, nil
}

func (r *ResourceRepository) fetchServiceProfile(ctx context.Context, id uint64) (map[string]any, error) {
	var systemName, repoURL, runtimeEnv string
	err := r.db.QueryRowContext(ctx,
		`select system_name, repository_url, runtime_env from resource_profiles_service where resource_id = ?`,
		id,
	).Scan(&systemName, &repoURL, &runtimeEnv)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch service profile: %w", err)
	}
	return map[string]any{
		"systemName":    systemName,
		"repositoryUrl": repoURL,
		"runtimeEnv":    runtimeEnv,
	}, nil
}

func (r *ResourceRepository) fetchHostProfile(ctx context.Context, id uint64) (map[string]any, error) {
	var hostname, ipAddress, osName string
	err := r.db.QueryRowContext(ctx,
		`select hostname, ip_address, os_name from resource_profiles_host where resource_id = ?`,
		id,
	).Scan(&hostname, &ipAddress, &osName)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fetch host profile: %w", err)
	}
	return map[string]any{
		"hostname":  hostname,
		"ipAddress": ipAddress,
		"osName":    osName,
	}, nil
}

func (r *ResourceRepository) UpsertHostProfile(ctx context.Context, resourceID uint64, hostname, ipAddress, osName string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resource_profiles_host (resource_id, hostname, ip_address, os_name, spec)
		 VALUES (?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE hostname = VALUES(hostname), ip_address = VALUES(ip_address), os_name = VALUES(os_name)`,
		resourceID, hostname, ipAddress, osName,
	)
	return err
}

func (r *ResourceRepository) UpsertDatabaseInstanceProfile(ctx context.Context, resourceID uint64, engine, version, host string, port int, role string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec)
		 VALUES (?, ?, ?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE engine = VALUES(engine), version = VALUES(version), host = VALUES(host), port = VALUES(port), role = VALUES(role)`,
		resourceID, engine, version, host, port, role,
	)
	return err
}

func (r *ResourceRepository) UpsertDatabaseClusterProfile(ctx context.Context, resourceID uint64, engine, topologyMode, primaryEndpoint string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec)
		 VALUES (?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE engine = VALUES(engine), topology_mode = VALUES(topology_mode), primary_endpoint = VALUES(primary_endpoint)`,
		resourceID, engine, topologyMode, primaryEndpoint,
	)
	return err
}

func (r *ResourceRepository) UpsertServiceProfile(ctx context.Context, resourceID uint64, systemName, repositoryUrl, runtimeEnv string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec)
		 VALUES (?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE system_name = VALUES(system_name), repository_url = VALUES(repository_url), runtime_env = VALUES(runtime_env)`,
		resourceID, systemName, repositoryUrl, runtimeEnv,
	)
	return err
}

func (r *ResourceRepository) DeleteProfile(ctx context.Context, resourceID uint64, resourceType string) error {
	tableMap := map[string]string{
		"host":              "resource_profiles_host",
		"database_instance": "resource_profiles_database_instance",
		"database_cluster":  "resource_profiles_database_cluster",
		"service":           "resource_profiles_service",
	}
	table, ok := tableMap[resourceType]
	if !ok {
		return nil
	}
	_, err := r.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE resource_id = ?", resourceID)
	return err
}

func (r *ResourceRepository) CreateResource(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error) {
	labelsJSON, err := json.Marshal(input.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}

	query := `insert into resources
		(resource_type, resource_subtype, name, display_name,
		 environment_id, owner_id, lifecycle_status, health_status,
		 source, external_id, labels, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`

	result, err := r.db.ExecContext(ctx, query,
		input.ResourceType, input.ResourceSubtype,
		input.Name, input.DisplayName,
		input.EnvironmentID, input.OwnerID,
		string(input.LifecycleStatus), string(input.HealthStatus),
		input.Source, input.ExternalID,
		string(labelsJSON),
	)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return nil, service.ErrResourceConflict
		}
		return nil, fmt.Errorf("insert resource: %w", err)
	}

	insertID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("resource last insert id: %w", err)
	}

	return r.GetResource(uint64(insertID))
}

func (r *ResourceRepository) UpdateResource(ctx context.Context, id uint64, input model.ResourceUpdateInput) (*model.Resource, error) {
	existing, err := r.GetResource(id)
	if err != nil {
		return nil, err
	}

	setClauses := []string{}
	args := []any{}

	if input.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *input.Name)
	}
	if input.ResourceSubtype != nil {
		setClauses = append(setClauses, "resource_subtype = ?")
		args = append(args, *input.ResourceSubtype)
	}
	if input.DisplayName != nil {
		setClauses = append(setClauses, "display_name = ?")
		args = append(args, *input.DisplayName)
	}
	if input.EnvironmentID != nil {
		setClauses = append(setClauses, "environment_id = ?")
		args = append(args, *input.EnvironmentID)
	}
	if input.OwnerID != nil {
		setClauses = append(setClauses, "owner_id = ?")
		args = append(args, *input.OwnerID)
	}
	if input.LifecycleStatus != nil {
		setClauses = append(setClauses, "lifecycle_status = ?")
		args = append(args, string(*input.LifecycleStatus))
	}
	if input.HealthStatus != nil {
		setClauses = append(setClauses, "health_status = ?")
		args = append(args, string(*input.HealthStatus))
	}
	if input.Source != nil {
		setClauses = append(setClauses, "source = ?")
		args = append(args, *input.Source)
	}
	if input.ExternalID != nil {
		setClauses = append(setClauses, "external_id = ?")
		args = append(args, *input.ExternalID)
	}
	if input.Labels != nil {
		labelsJSON, _ := json.Marshal(*input.Labels)
		setClauses = append(setClauses, "labels = ?")
		args = append(args, string(labelsJSON))
	}

	if len(setClauses) == 0 {
		return existing, nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := "update resources set " + strings.Join(setClauses, ", ") + " where id = ?"

	if _, err := r.db.ExecContext(ctx, query, args...); err != nil {
		return nil, fmt.Errorf("update resource %d: %w", id, err)
	}

	return r.GetResource(id)
}

func (r *ResourceRepository) ArchiveResource(ctx context.Context, id uint64, reason string) (*model.Resource, error) {
	query := `update resources set archived_at = NOW(6), archived_by = NULL, archive_reason = ? where id = ? and archived_at is null`
	_, err := r.db.ExecContext(ctx, query, reason, id)
	if err != nil {
		return nil, fmt.Errorf("archive resource %d: %w", id, err)
	}

	return r.GetResource(id)
}

func (r *ResourceRepository) UnarchiveResource(ctx context.Context, id uint64) (*model.Resource, error) {
	query := `update resources set archived_at = NULL, archived_by = NULL, archive_reason = NULL where id = ? and archived_at is not null`
	_, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("unarchive resource %d: %w", id, err)
	}

	return r.GetResource(id)
}

type resourceScanner interface {
	Scan(dest ...any) error
}

func scanResource(scanner resourceScanner) (model.Resource, error) {
	var (
		item          model.Resource
		rawLabels     string
		archivedAt    sql.NullTime
		archivedBy    sql.NullInt64
		archiveReason sql.NullString
	)

	err := scanner.Scan(
		&item.ID,
		&item.ResourceType,
		&item.ResourceSubtype,
		&item.Name,
		&item.DisplayName,
		&item.EnvironmentID,
		&item.OwnerID,
		&item.LifecycleStatus,
		&item.HealthStatus,
		&item.Source,
		&item.ExternalID,
		&rawLabels,
		&item.CreatedAt,
		&item.UpdatedAt,
		&archivedAt,
		&archivedBy,
		&archiveReason,
	)
	if err != nil {
		return model.Resource{}, err
	}

	if archivedAt.Valid {
		item.ArchivedAt = &archivedAt.Time
	}
	if archivedBy.Valid {
		archivedByValue := uint64(archivedBy.Int64)
		item.ArchivedBy = &archivedByValue
	}
	if archiveReason.Valid {
		item.ArchiveReason = &archiveReason.String
	}

	if rawLabels == "" || rawLabels == "null" {
		item.Labels = map[string]string{}
		return item, nil
	}

	if err := json.Unmarshal([]byte(rawLabels), &item.Labels); err != nil {
		return model.Resource{}, err
	}

	return item, nil
}
