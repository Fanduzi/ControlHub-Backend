package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type ResourceRepository struct {
	db *sql.DB
}

func NewResourceRepository(db *sql.DB) *ResourceRepository {
	return &ResourceRepository{db: db}
}

func (r *ResourceRepository) ListResources(resourceType string, environmentID string) ([]model.Resource, error) {
	query := `
	select id, resource_type, resource_subtype, name, display_name,
	       environment_id, owner_id, lifecycle_status, health_status,
	       source, external_id, labels, created_at, updated_at
	from resources
	where (? = '' or resource_type = ?)
	  and (? = '' or environment_id = ?)
	order by name`

	rows, err := r.db.QueryContext(context.Background(), query,
		resourceType, resourceType,
		environmentID, environmentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Resource, 0)
	for rows.Next() {
		item, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *ResourceRepository) GetResource(id string) (*model.Resource, error) {
	query := `
	select id, resource_type, resource_subtype, name, display_name,
	       environment_id, owner_id, lifecycle_status, health_status,
	       source, external_id, labels, created_at, updated_at
	from resources
	where id = ?`

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

func (r *ResourceRepository) GetResourceProfile(id string) (*model.ResourceProfileResponse, error) {
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

func (r *ResourceRepository) fetchProfile(resourceID string, resourceType model.ResourceType) (map[string]any, error) {
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

func (r *ResourceRepository) fetchDatabaseInstanceProfile(ctx context.Context, id string) (map[string]any, error) {
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
		"engine": engine,
		"version": version,
		"host":   host,
		"port":   port,
		"role":   role,
	}, nil
}

func (r *ResourceRepository) fetchDatabaseClusterProfile(ctx context.Context, id string) (map[string]any, error) {
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
		"engine":           engine,
		"topologyMode":     topologyMode,
		"primaryEndpoint":  primaryEndpoint,
	}, nil
}

func (r *ResourceRepository) fetchServiceProfile(ctx context.Context, id string) (map[string]any, error) {
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

func (r *ResourceRepository) fetchHostProfile(ctx context.Context, id string) (map[string]any, error) {
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

type resourceScanner interface {
	Scan(dest ...any) error
}

func scanResource(scanner resourceScanner) (model.Resource, error) {
	var (
		item      model.Resource
		rawLabels string
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
	)
	if err != nil {
		return model.Resource{}, err
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
