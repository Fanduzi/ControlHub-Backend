// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model, internal/service
// output: NewResourceRepository, ResourceRepository struct (implements service.ResourceRepository)
// pos: MySQL data access for core resource table, typed profiles including domain_name and virtual_ip, pagination and filtering
// pos: MySQL data access for core resource table, typed profiles including database_proxy and control_plane_component, pagination and filtering
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
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
       origin, labels, created_at, updated_at,
       archived_at, archived_by, archive_reason`

func (r *ResourceRepository) ListResources(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, int, error) {
	var conds []string
	var args []any

	if len(q.ResourceTypes) > 0 {
		ph := buildInClause(len(q.ResourceTypes))
		conds = append(conds, "r.resource_type in ("+ph+")")
		for _, v := range q.ResourceTypes {
			args = append(args, v)
		}
	}
	if len(q.EnvironmentIDs) > 0 {
		ph := buildInClause(len(q.EnvironmentIDs))
		conds = append(conds, "r.environment_id in ("+ph+")")
		for _, v := range q.EnvironmentIDs {
			args = append(args, v)
		}
	}
	if len(q.LifecycleStatus) > 0 {
		ph := buildInClause(len(q.LifecycleStatus))
		conds = append(conds, "r.lifecycle_status in ("+ph+")")
		for _, v := range q.LifecycleStatus {
			args = append(args, v)
		}
	}
	if len(q.HealthStatuses) > 0 {
		ph := buildInClause(len(q.HealthStatuses))
		conds = append(conds, "r.health_status in ("+ph+")")
		for _, v := range q.HealthStatuses {
			args = append(args, v)
		}
	}
	if len(q.ResourceSubtypes) > 0 {
		ph := buildInClause(len(q.ResourceSubtypes))
		conds = append(conds, "r.resource_subtype in ("+ph+")")
		for _, v := range q.ResourceSubtypes {
			args = append(args, v)
		}
	}
	if q.Query != "" {
		pattern := "%" + q.Query + "%"
		conds = append(conds, `(r.name like ? or r.display_name like ?
			or exists (select 1 from resource_aliases ra where ra.resource_id = r.id and ra.alias like ?)
			or exists (select 1 from resource_external_identifiers rei where rei.resource_id = r.id and (rei.external_system like ? or rei.external_value like ?)))`)
		args = append(args, pattern, pattern, pattern, pattern, pattern)
	}

	if q.ArchivedOnly {
		conds = append(conds, "r.archived_at is not null")
	} else if !q.IncludeArchived {
		conds = append(conds, "r.archived_at is null")
	}

	where := ""
	if len(conds) > 0 {
		where = "where " + strings.Join(conds, " and ")
	}

	var total int
	countQuery := "select count(*) from resources r " + where
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count resources: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	dataQuery := `select r.id, r.resource_type, r.resource_subtype, r.name, r.display_name,
       r.environment_id, r.owner_id, r.lifecycle_status, r.health_status,
       r.origin, r.labels, r.created_at, r.updated_at,
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
			&item.Origin,
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
		if err := loadResourceIdentity(ctx, r.db, &item); err != nil {
			return nil, 0, err
		}

		item.ProfileSummary = r.buildProfileSummary(ctx, item.ID, item.ResourceType)

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	r.attachDatabaseOperationalSummaries(ctx, items)

	return items, total, nil
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
	query := `select r.id, r.resource_type, r.resource_subtype, r.name, r.display_name,
	       r.environment_id, r.owner_id, r.lifecycle_status, r.health_status,
	       r.origin, r.labels, r.created_at, r.updated_at,
	       r.archived_at, r.archived_by, r.archive_reason,
	       (select rr.to_resource_id from resource_relations rr
	        where rr.from_resource_id = r.id and rr.relation_type = 'member_of'
	        limit 1) as cluster_id
	from resources r where r.id = ?`

	row := r.db.QueryRowContext(context.Background(), query, id)

	var (
		item          model.Resource
		rawLabels     string
		archivedAt    sql.NullTime
		archivedBy    sql.NullInt64
		archiveReason sql.NullString
		clusterId     sql.NullInt64
	)

	err := row.Scan(
		&item.ID,
		&item.ResourceType,
		&item.ResourceSubtype,
		&item.Name,
		&item.DisplayName,
		&item.EnvironmentID,
		&item.OwnerID,
		&item.LifecycleStatus,
		&item.HealthStatus,
		&item.Origin,
		&rawLabels,
		&item.CreatedAt,
		&item.UpdatedAt,
		&archivedAt,
		&archivedBy,
		&archiveReason,
		&clusterId,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrResourceNotFound
		}
		return nil, err
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
		return nil, err
	}
	if err := loadResourceIdentity(context.Background(), r.db, &item); err != nil {
		return nil, err
	}

	item.ProfileSummary = r.buildProfileSummary(context.Background(), item.ID, item.ResourceType)

	if item.ResourceType == model.ResourceTypeDatabaseCluster {
		item.DatabaseOperationalSummary = r.buildDatabaseOperationalSummary(context.Background(), item.ID)
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
	return fetchProfile(context.Background(), r.db, resourceID, resourceType)
}

type profileQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func fetchProfile(ctx context.Context, q profileQueryer, id uint64, resourceType model.ResourceType) (map[string]any, error) {
	var err error
	switch resourceType {
	case model.ResourceTypeDatabaseInstance:
		var engine, version, host, role string
		var port int
		err = q.QueryRowContext(ctx, `select engine, version, host, port, role from resource_profiles_database_instance where resource_id = ?`, id).Scan(&engine, &version, &host, &port, &role)
		if err == nil {
			return map[string]any{"engine": engine, "version": version, "host": host, "port": port, "role": role}, nil
		}
	case model.ResourceTypeDatabaseCluster:
		var engine, topologyMode, primaryEndpoint string
		err = q.QueryRowContext(ctx, `select engine, topology_mode, primary_endpoint from resource_profiles_database_cluster where resource_id = ?`, id).Scan(&engine, &topologyMode, &primaryEndpoint)
		if err == nil {
			return map[string]any{"engine": engine, "topologyMode": topologyMode, "primaryEndpoint": primaryEndpoint}, nil
		}
	case model.ResourceTypeService:
		var systemName, repositoryURL, runtimeEnv string
		err = q.QueryRowContext(ctx, `select system_name, repository_url, runtime_env from resource_profiles_service where resource_id = ?`, id).Scan(&systemName, &repositoryURL, &runtimeEnv)
		if err == nil {
			return map[string]any{"systemName": systemName, "repositoryUrl": repositoryURL, "runtimeEnv": runtimeEnv}, nil
		}
	case model.ResourceTypeHost:
		var hostname, ipAddress, osName string
		err = q.QueryRowContext(ctx, `select hostname, ip_address, os_name from resource_profiles_host where resource_id = ?`, id).Scan(&hostname, &ipAddress, &osName)
		if err == nil {
			return map[string]any{"hostname": hostname, "ipAddress": ipAddress, "osName": osName}, nil
		}
	case model.ResourceTypeDomainName:
		var fqdn string
		err = q.QueryRowContext(ctx, `select fqdn from resource_profiles_domain_name where resource_id = ?`, id).Scan(&fqdn)
		if err == nil {
			return map[string]any{"fqdn": fqdn}, nil
		}
	case model.ResourceTypeDatabaseProxy:
		var technologySubtype, host, role, version string
		var port int
		err = q.QueryRowContext(ctx, `select technology_subtype, host, port, role, version from resource_profiles_database_proxy where resource_id = ?`, id).Scan(&technologySubtype, &host, &port, &role, &version)
		if err == nil {
			return map[string]any{"technologySubtype": technologySubtype, "host": host, "port": port, "role": role, "version": version}, nil
		}
	case model.ResourceTypeVirtualIP:
		var ipAddress string
		err = q.QueryRowContext(ctx, `select ip_address from resource_profiles_virtual_ip where resource_id = ?`, id).Scan(&ipAddress)
		if err == nil {
			return map[string]any{"ipAddress": ipAddress}, nil
		}
	case model.ResourceTypeControlPlaneComponent:
		var componentSubtype, endpoint, version, role string
		err = q.QueryRowContext(ctx, `select component_subtype, endpoint, version, role from resource_profiles_control_plane_component where resource_id = ?`, id).Scan(&componentSubtype, &endpoint, &version, &role)
		if err == nil {
			return map[string]any{"componentSubtype": componentSubtype, "endpoint": endpoint, "version": version, "role": role}, nil
		}
	default:
		return map[string]any{}, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	return nil, fmt.Errorf("fetch %s profile: %w", resourceType, err)
}

const (
	upsertHostProfileSQL = `INSERT INTO resource_profiles_host (resource_id, hostname, ip_address, os_name, spec)
		 VALUES (?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE hostname = VALUES(hostname), ip_address = VALUES(ip_address), os_name = VALUES(os_name)`

	upsertDatabaseInstanceProfileSQL = `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec)
		 VALUES (?, ?, ?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE engine = VALUES(engine), version = VALUES(version), host = VALUES(host), port = VALUES(port), role = VALUES(role)`

	upsertDatabaseClusterProfileSQL = `INSERT INTO resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec)
		 VALUES (?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE engine = VALUES(engine), topology_mode = VALUES(topology_mode), primary_endpoint = VALUES(primary_endpoint)`

	upsertServiceProfileSQL = `INSERT INTO resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec)
		 VALUES (?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE system_name = VALUES(system_name), repository_url = VALUES(repository_url), runtime_env = VALUES(runtime_env)`

	upsertDomainNameProfileSQL = `INSERT INTO resource_profiles_domain_name (resource_id, fqdn, spec)
		 VALUES (?, ?, '{}')
		 ON DUPLICATE KEY UPDATE fqdn = VALUES(fqdn)`

	upsertVirtualIPProfileSQL = `INSERT INTO resource_profiles_virtual_ip (resource_id, ip_address, spec)
		 VALUES (?, ?, '{}')
		 ON DUPLICATE KEY UPDATE ip_address = VALUES(ip_address)`

	// patch statements merge submitted fields (COALESCE) and create the row
	// when absent; omitted fields keep their current values. Each pointer
	// argument appears twice: once for the INSERT branch, once for UPDATE.
	patchHostProfileSQL = `INSERT INTO resource_profiles_host (resource_id, hostname, ip_address, os_name, spec)
		 VALUES (?, COALESCE(?, ''), COALESCE(?, ''), COALESCE(?, ''), '{}')
		 ON DUPLICATE KEY UPDATE hostname = COALESCE(?, hostname), ip_address = COALESCE(?, ip_address), os_name = COALESCE(?, os_name)`

	patchDatabaseInstanceProfileSQL = `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec)
		 VALUES (?, COALESCE(?, ''), COALESCE(?, ''), COALESCE(?, ''), COALESCE(?, 0), COALESCE(?, ''), '{}')
		 ON DUPLICATE KEY UPDATE engine = COALESCE(?, engine), version = COALESCE(?, version), host = COALESCE(?, host), port = COALESCE(?, port), role = COALESCE(?, role)`

	patchDatabaseClusterProfileSQL = `INSERT INTO resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec)
		 VALUES (?, COALESCE(?, ''), COALESCE(?, ''), COALESCE(?, ''), '{}')
		 ON DUPLICATE KEY UPDATE engine = COALESCE(?, engine), topology_mode = COALESCE(?, topology_mode), primary_endpoint = COALESCE(?, primary_endpoint)`

	patchServiceProfileSQL = `INSERT INTO resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec)
		 VALUES (?, COALESCE(?, ''), COALESCE(?, ''), COALESCE(?, ''), '{}')
		 ON DUPLICATE KEY UPDATE system_name = COALESCE(?, system_name), repository_url = COALESCE(?, repository_url), runtime_env = COALESCE(?, runtime_env)`

	patchDomainNameProfileSQL = `INSERT INTO resource_profiles_domain_name (resource_id, fqdn, spec)
		 VALUES (?, COALESCE(?, ''), '{}')
		 ON DUPLICATE KEY UPDATE fqdn = COALESCE(?, fqdn)`
	upsertDatabaseProxyProfileSQL = `INSERT INTO resource_profiles_database_proxy (resource_id, technology_subtype, host, port, role, version, spec)
		 VALUES (?, ?, ?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE technology_subtype = VALUES(technology_subtype), host = VALUES(host), port = VALUES(port), role = VALUES(role), version = VALUES(version)`

	patchVirtualIPProfileSQL = `INSERT INTO resource_profiles_virtual_ip (resource_id, ip_address, spec)
		 VALUES (?, COALESCE(?, ''), '{}')
		 ON DUPLICATE KEY UPDATE ip_address = COALESCE(?, ip_address)`
	upsertControlPlaneComponentProfileSQL = `INSERT INTO resource_profiles_control_plane_component (resource_id, component_subtype, endpoint, version, role, spec)
		 VALUES (?, ?, ?, ?, ?, '{}')
		 ON DUPLICATE KEY UPDATE component_subtype = VALUES(component_subtype), endpoint = VALUES(endpoint), version = VALUES(version), role = VALUES(role)`

	patchDatabaseProxyProfileSQL = `INSERT INTO resource_profiles_database_proxy (resource_id, technology_subtype, host, port, role, version, spec)
		 VALUES (?, COALESCE(?, ''), COALESCE(?, ''), COALESCE(?, 0), COALESCE(?, ''), COALESCE(?, ''), '{}')
		 ON DUPLICATE KEY UPDATE technology_subtype = COALESCE(?, technology_subtype), host = COALESCE(?, host), port = COALESCE(?, port), role = COALESCE(?, role), version = COALESCE(?, version)`

	patchControlPlaneComponentProfileSQL = `INSERT INTO resource_profiles_control_plane_component (resource_id, component_subtype, endpoint, version, role, spec)
		 VALUES (?, COALESCE(?, ''), COALESCE(?, ''), COALESCE(?, ''), COALESCE(?, ''), '{}')
		 ON DUPLICATE KEY UPDATE component_subtype = COALESCE(?, component_subtype), endpoint = COALESCE(?, endpoint), version = COALESCE(?, version), role = COALESCE(?, role)`
)

// upsertProfileTx writes the typed profile row inside the create-with-profile
// transaction. The per-type field dispatch mirrors ProfileService.writeProfile
// in internal/service/profile_service.go; keep the two mappings in sync.
func upsertProfileTx(ctx context.Context, tx *sql.Tx, resourceID uint64, resourceType model.ResourceType, fields map[string]any) error {
	switch resourceType {
	case model.ResourceTypeHost:
		_, err := tx.ExecContext(ctx, upsertHostProfileSQL, resourceID,
			profileFieldString(fields, "hostname"), profileFieldString(fields, "ipAddress"), profileFieldString(fields, "osName"))
		return err
	case model.ResourceTypeDatabaseInstance:
		_, err := tx.ExecContext(ctx, upsertDatabaseInstanceProfileSQL, resourceID,
			profileFieldString(fields, "engine"), profileFieldString(fields, "version"),
			profileFieldString(fields, "host"), profileFieldInt(fields, "port"), profileFieldString(fields, "role"))
		return err
	case model.ResourceTypeDatabaseCluster:
		_, err := tx.ExecContext(ctx, upsertDatabaseClusterProfileSQL, resourceID,
			profileFieldString(fields, "engine"), profileFieldString(fields, "topologyMode"), profileFieldString(fields, "primaryEndpoint"))
		return err
	case model.ResourceTypeService:
		_, err := tx.ExecContext(ctx, upsertServiceProfileSQL, resourceID,
			profileFieldString(fields, "systemName"), profileFieldString(fields, "repositoryUrl"), profileFieldString(fields, "runtimeEnv"))
		return err
	case model.ResourceTypeDomainName:
		_, err := tx.ExecContext(ctx, upsertDomainNameProfileSQL, resourceID, profileFieldString(fields, "fqdn"))
		return err
	case model.ResourceTypeDatabaseProxy:
		_, err := tx.ExecContext(ctx, upsertDatabaseProxyProfileSQL, resourceID,
			profileFieldString(fields, "technologySubtype"), profileFieldString(fields, "host"),
			profileFieldInt(fields, "port"), profileFieldString(fields, "role"), profileFieldString(fields, "version"))
		return err
	case model.ResourceTypeVirtualIP:
		_, err := tx.ExecContext(ctx, upsertVirtualIPProfileSQL, resourceID, profileFieldString(fields, "ipAddress"))
		return err
	case model.ResourceTypeControlPlaneComponent:
		_, err := tx.ExecContext(ctx, upsertControlPlaneComponentProfileSQL, resourceID,
			profileFieldString(fields, "componentSubtype"), profileFieldString(fields, "endpoint"),
			profileFieldString(fields, "version"), profileFieldString(fields, "role"))
		return err
	default:
		return service.ErrProfileNotSupported
	}
}

func profileFieldString(fields map[string]any, key string) string {
	v, ok := fields[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func profileFieldInt(fields map[string]any, key string) int {
	v, ok := fields[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// profileFieldStringPtr returns nil when the key is absent (field omitted)
// and a pointer to the value otherwise, so a single COALESCE statement can
// distinguish "omit" from "set to empty".
func profileFieldStringPtr(fields map[string]any, key string) *string {
	if _, ok := fields[key]; !ok {
		return nil
	}
	s := profileFieldString(fields, key)
	return &s
}

// profileFieldIntPtr is the integer counterpart of profileFieldStringPtr.
func profileFieldIntPtr(fields map[string]any, key string) *int {
	if _, ok := fields[key]; !ok {
		return nil
	}
	n := profileFieldInt(fields, key)
	return &n
}

// PatchProfile merges the submitted profile fields in a single atomic
// statement: nil pointers leave the stored value unchanged, non-nil values
// are written (explicit empty/zero values honored), and the row is created
// when absent. The per-type field dispatch mirrors ProfileService.writeProfile
// in internal/service/profile_service.go; keep the two mappings in sync.
type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (r *ResourceRepository) PatchProfile(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]any) error {
	return patchProfile(ctx, r.db, resourceID, resourceType, fields)
}

func patchProfile(ctx context.Context, execer sqlExecer, resourceID uint64, resourceType model.ResourceType, fields map[string]any) error {
	switch resourceType {
	case model.ResourceTypeHost:
		hostname := profileFieldStringPtr(fields, "hostname")
		ipAddress := profileFieldStringPtr(fields, "ipAddress")
		osName := profileFieldStringPtr(fields, "osName")
		_, err := execer.ExecContext(ctx, patchHostProfileSQL, resourceID, hostname, ipAddress, osName, hostname, ipAddress, osName)
		return err
	case model.ResourceTypeDatabaseInstance:
		engine := profileFieldStringPtr(fields, "engine")
		version := profileFieldStringPtr(fields, "version")
		host := profileFieldStringPtr(fields, "host")
		port := profileFieldIntPtr(fields, "port")
		role := profileFieldStringPtr(fields, "role")
		_, err := execer.ExecContext(ctx, patchDatabaseInstanceProfileSQL, resourceID, engine, version, host, port, role, engine, version, host, port, role)
		return err
	case model.ResourceTypeDatabaseCluster:
		engine := profileFieldStringPtr(fields, "engine")
		topologyMode := profileFieldStringPtr(fields, "topologyMode")
		primaryEndpoint := profileFieldStringPtr(fields, "primaryEndpoint")
		_, err := execer.ExecContext(ctx, patchDatabaseClusterProfileSQL, resourceID, engine, topologyMode, primaryEndpoint, engine, topologyMode, primaryEndpoint)
		return err
	case model.ResourceTypeService:
		systemName := profileFieldStringPtr(fields, "systemName")
		repositoryUrl := profileFieldStringPtr(fields, "repositoryUrl")
		runtimeEnv := profileFieldStringPtr(fields, "runtimeEnv")
		_, err := execer.ExecContext(ctx, patchServiceProfileSQL, resourceID, systemName, repositoryUrl, runtimeEnv, systemName, repositoryUrl, runtimeEnv)
		return err
	case model.ResourceTypeDomainName:
		fqdn := profileFieldStringPtr(fields, "fqdn")
		_, err := execer.ExecContext(ctx, patchDomainNameProfileSQL, resourceID, fqdn, fqdn)
		return err
	case model.ResourceTypeDatabaseProxy:
		technologySubtype := profileFieldStringPtr(fields, "technologySubtype")
		host := profileFieldStringPtr(fields, "host")
		port := profileFieldIntPtr(fields, "port")
		role := profileFieldStringPtr(fields, "role")
		version := profileFieldStringPtr(fields, "version")
		_, err := execer.ExecContext(ctx, patchDatabaseProxyProfileSQL, resourceID, technologySubtype, host, port, role, version, technologySubtype, host, port, role, version)
		return err
	case model.ResourceTypeVirtualIP:
		ipAddress := profileFieldStringPtr(fields, "ipAddress")
		_, err := execer.ExecContext(ctx, patchVirtualIPProfileSQL, resourceID, ipAddress, ipAddress)
		return err
	case model.ResourceTypeControlPlaneComponent:
		componentSubtype := profileFieldStringPtr(fields, "componentSubtype")
		endpoint := profileFieldStringPtr(fields, "endpoint")
		version := profileFieldStringPtr(fields, "version")
		role := profileFieldStringPtr(fields, "role")
		_, err := execer.ExecContext(ctx, patchControlPlaneComponentProfileSQL, resourceID, componentSubtype, endpoint, version, role, componentSubtype, endpoint, version, role)
		return err
	default:
		return service.ErrProfileNotSupported
	}
}

func (r *ResourceRepository) UpsertHostProfile(ctx context.Context, resourceID uint64, hostname, ipAddress, osName string) error {
	_, err := r.db.ExecContext(ctx, upsertHostProfileSQL, resourceID, hostname, ipAddress, osName)
	return err
}

func (r *ResourceRepository) UpsertDatabaseInstanceProfile(ctx context.Context, resourceID uint64, engine, version, host string, port int, role string) error {
	_, err := r.db.ExecContext(ctx, upsertDatabaseInstanceProfileSQL, resourceID, engine, version, host, port, role)
	return err
}

func (r *ResourceRepository) UpsertDatabaseClusterProfile(ctx context.Context, resourceID uint64, engine, topologyMode, primaryEndpoint string) error {
	_, err := r.db.ExecContext(ctx, upsertDatabaseClusterProfileSQL, resourceID, engine, topologyMode, primaryEndpoint)
	return err
}

func (r *ResourceRepository) UpsertServiceProfile(ctx context.Context, resourceID uint64, systemName, repositoryUrl, runtimeEnv string) error {
	_, err := r.db.ExecContext(ctx, upsertServiceProfileSQL, resourceID, systemName, repositoryUrl, runtimeEnv)
	return err
}

func (r *ResourceRepository) UpsertDomainNameProfile(ctx context.Context, resourceID uint64, fqdn string) error {
	_, err := r.db.ExecContext(ctx, upsertDomainNameProfileSQL, resourceID, fqdn)
	return err
}

func (r *ResourceRepository) UpsertDatabaseProxyProfile(ctx context.Context, resourceID uint64, technologySubtype, host string, port int, role, version string) error {
	_, err := r.db.ExecContext(ctx, upsertDatabaseProxyProfileSQL, resourceID, technologySubtype, host, port, role, version)
	return err
}

func (r *ResourceRepository) UpsertVirtualIPProfile(ctx context.Context, resourceID uint64, ipAddress string) error {
	_, err := r.db.ExecContext(ctx, upsertVirtualIPProfileSQL, resourceID, ipAddress)
	return err
}

func (r *ResourceRepository) UpsertControlPlaneComponentProfile(ctx context.Context, resourceID uint64, componentSubtype, endpoint, version, role string) error {
	_, err := r.db.ExecContext(ctx, upsertControlPlaneComponentProfileSQL, resourceID, componentSubtype, endpoint, version, role)
	return err
}

func (r *ResourceRepository) DeleteProfile(ctx context.Context, resourceID uint64, resourceType string) error {
	return deleteProfile(ctx, r.db, resourceID, resourceType)
}

func deleteProfile(ctx context.Context, execer sqlExecer, resourceID uint64, resourceType string) error {
	tableMap := map[string]string{
		"host":                    "resource_profiles_host",
		"database_instance":       "resource_profiles_database_instance",
		"database_cluster":        "resource_profiles_database_cluster",
		"service":                 "resource_profiles_service",
		"domain_name":             "resource_profiles_domain_name",
		"virtual_ip":              "resource_profiles_virtual_ip",
		"database_proxy":          "resource_profiles_database_proxy",
		"control_plane_component": "resource_profiles_control_plane_component",
	}
	table, ok := tableMap[resourceType]
	if !ok {
		return nil
	}
	_, err := execer.ExecContext(ctx, "DELETE FROM "+table+" WHERE resource_id = ?", resourceID)
	return err
}

func (r *ResourceRepository) PutProfileWithAudit(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]any, actorUserID uint64, eventType string) error {
	return r.mutateProfileWithAudit(ctx, resourceID, resourceType, fields, actorUserID, eventType, "put")
}

func (r *ResourceRepository) PatchProfileWithAudit(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]any, actorUserID uint64, eventType string) error {
	return r.mutateProfileWithAudit(ctx, resourceID, resourceType, fields, actorUserID, eventType, "patch")
}

func (r *ResourceRepository) DeleteProfileWithAudit(ctx context.Context, resourceID uint64, resourceType model.ResourceType, actorUserID uint64, eventType string) error {
	return r.mutateProfileWithAudit(ctx, resourceID, resourceType, nil, actorUserID, eventType, "delete")
}

func (r *ResourceRepository) mutateProfileWithAudit(ctx context.Context, resourceID uint64, resourceType model.ResourceType, fields map[string]any, actorUserID uint64, eventType, mutation string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin inventory profile transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := getResourceForUpdate(ctx, tx, resourceID); err != nil {
		return err
	}
	before, err := fetchProfile(ctx, tx, resourceID, resourceType)
	if err != nil {
		return err
	}
	switch mutation {
	case "put":
		err = upsertProfileTx(ctx, tx, resourceID, resourceType, fields)
	case "patch":
		err = patchProfile(ctx, tx, resourceID, resourceType, fields)
	case "delete":
		err = deleteProfile(ctx, tx, resourceID, string(resourceType))
	default:
		return fmt.Errorf("unsupported profile mutation %q", mutation)
	}
	if err != nil {
		return fmt.Errorf("%s inventory profile: %w", mutation, err)
	}
	after, err := fetchProfile(ctx, tx, resourceID, resourceType)
	if err != nil {
		return err
	}
	if err := insertInventoryAuditTx(ctx, tx, actorUserID, resourceID, eventType, auditMapChanges("profile.", before, after)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit inventory profile transaction: %w", err)
	}
	return nil
}

// insertResourceSQL is shared by CreateResource and CreateResourceWithProfile
// so the single-transaction path and the standalone path write the same row.
const insertResourceSQL = `insert into resources
	(resource_type, resource_subtype, name, display_name,
	 environment_id, owner_id, lifecycle_status, health_status,
	 origin, labels, created_at, updated_at)
	values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())`

func (r *ResourceRepository) CreateResource(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error) {
	return r.createResource(ctx, input, nil)
}

// CreateResourceWithProfile inserts the resource and its initial typed profile
// in a single transaction: if the profile write fails, the resource insert is
// rolled back, so a client can never receive success for a resource whose
// submitted profile was not persisted. The field dispatch mirrors
// ProfileService.writeProfile in internal/service/profile_service.go; keep the
// two mappings in sync when profile fields change.
func (r *ResourceRepository) CreateResourceWithProfile(ctx context.Context, input model.ResourceCreateInput, profile map[string]any) (*model.Resource, error) {
	return r.createResource(ctx, input, profile)
}

func (r *ResourceRepository) createResource(ctx context.Context, input model.ResourceCreateInput, profile map[string]any) (*model.Resource, error) {
	if input.Origin == "" {
		switch input.Source {
		case "", "manual":
			input.Origin = model.ResourceOriginManual
		case "import", "imported", "terraform":
			input.Origin = model.ResourceOriginImported
		case "discovery", "discovered":
			input.Origin = model.ResourceOriginDiscovered
		default:
			input.Origin = model.ResourceOriginImported
		}
	}
	if len(input.ExternalIdentifiers) == 0 && strings.TrimSpace(input.ExternalID) != "" {
		input.ExternalIdentifiers = []model.ResourceExternalIdentifier{{System: "legacy", Value: input.ExternalID}}
	}
	labelsJSON, err := json.Marshal(input.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create resource transaction: %w", err)
	}
	defer tx.Rollback() // safe no-op after commit

	result, err := tx.ExecContext(ctx, insertResourceSQL,
		input.ResourceType, input.ResourceSubtype,
		input.Name, input.DisplayName,
		input.EnvironmentID, input.OwnerID,
		string(input.LifecycleStatus), string(input.HealthStatus),
		input.Origin,
		string(labelsJSON),
	)
	if err != nil {
		if conflict := classifyResourceConflict(err); conflict != nil {
			return nil, conflict
		}
		return nil, fmt.Errorf("insert resource: %w", err)
	}

	insertID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("resource last insert id: %w", err)
	}

	resourceID := uint64(insertID)
	if err := insertResourceIdentityTx(ctx, tx, resourceID, input.EnvironmentID, input.Aliases, input.ExternalIdentifiers); err != nil {
		return nil, err
	}
	if profile != nil {
		if err := upsertProfileTx(ctx, tx, resourceID, input.ResourceType, profile); err != nil {
			return nil, fmt.Errorf("upsert profile for resource %d: %w", insertID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create resource transaction: %w", err)
	}

	return r.GetResource(resourceID)
}

func (r *ResourceRepository) UpdateResource(ctx context.Context, id uint64, input model.ResourceUpdateInput) (*model.Resource, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin resource update transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := updateResourceTx(ctx, tx, id, input); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit resource update transaction: %w", err)
	}
	return r.GetResource(id)
}

// UpdateResourceWithAudit locks the current CI, derives a server-owned field
// diff, and commits the resource update and audit evidence together.
func (r *ResourceRepository) UpdateResourceWithAudit(ctx context.Context, id uint64, input model.ResourceUpdateInput, actorUserID uint64, eventType string) (*model.Resource, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin inventory update transaction: %w", err)
	}
	defer tx.Rollback()

	existing, err := updateResourceTx(ctx, tx, id, input)
	if err != nil {
		return nil, err
	}

	if err := insertInventoryAuditTx(ctx, tx, actorUserID, id, eventType, resourceAuditChanges(*existing, input)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit inventory update transaction: %w", err)
	}
	return r.GetResource(id)
}

func updateResourceTx(ctx context.Context, tx *sql.Tx, id uint64, input model.ResourceUpdateInput) (*model.Resource, error) {
	existing, err := getResourceForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	query, args := resourceUpdateStatement(id, input)
	if query != "" {
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			if conflict := classifyResourceConflict(err); conflict != nil {
				return nil, conflict
			}
			return nil, fmt.Errorf("update resource %d: %w", id, err)
		}
	}
	environmentID := existing.EnvironmentID
	if input.EnvironmentID != nil {
		environmentID = *input.EnvironmentID
	}
	if err := replaceResourceIdentityTx(ctx, tx, id, environmentID, input); err != nil {
		return nil, err
	}
	return existing, nil
}

func getResourceForUpdate(ctx context.Context, tx *sql.Tx, id uint64) (*model.Resource, error) {
	item, err := scanResource(tx.QueryRowContext(ctx, "select "+resourceColumns+" from resources where id = ? for update", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrResourceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock resource %d: %w", id, err)
	}
	if err := loadResourceIdentity(ctx, tx, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

type resourceIdentityQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadResourceIdentity(ctx context.Context, q resourceIdentityQueryer, item *model.Resource) error {
	aliasRows, err := q.QueryContext(ctx, `select alias from resource_aliases where resource_id = ? order by alias`, item.ID)
	if err != nil {
		return fmt.Errorf("list aliases for resource %d: %w", item.ID, err)
	}
	defer aliasRows.Close()
	item.Aliases = []string{}
	for aliasRows.Next() {
		var alias string
		if err := aliasRows.Scan(&alias); err != nil {
			return err
		}
		item.Aliases = append(item.Aliases, alias)
	}
	if err := aliasRows.Err(); err != nil {
		return err
	}
	aliasRows.Close()

	identifierRows, err := q.QueryContext(ctx, `select external_system, external_value from resource_external_identifiers where resource_id = ? order by external_system, external_value`, item.ID)
	if err != nil {
		return fmt.Errorf("list external identifiers for resource %d: %w", item.ID, err)
	}
	defer identifierRows.Close()
	item.ExternalIdentifiers = []model.ResourceExternalIdentifier{}
	for identifierRows.Next() {
		var identifier model.ResourceExternalIdentifier
		if err := identifierRows.Scan(&identifier.System, &identifier.Value); err != nil {
			return err
		}
		item.ExternalIdentifiers = append(item.ExternalIdentifiers, identifier)
		if identifier.System == "legacy" {
			item.ExternalID = identifier.Value
		}
	}
	if err := identifierRows.Err(); err != nil {
		return err
	}
	item.Source = string(item.Origin)
	return nil
}

func insertResourceIdentityTx(ctx context.Context, tx *sql.Tx, resourceID, environmentID uint64, aliases []string, identifiers []model.ResourceExternalIdentifier) error {
	seenAliases := make(map[string]bool, len(aliases))
	for _, rawAlias := range aliases {
		alias := strings.ToLower(strings.TrimSpace(rawAlias))
		if seenAliases[alias] {
			continue
		}
		seenAliases[alias] = true
		if _, err := tx.ExecContext(ctx, `insert into resource_aliases (resource_id, environment_id, alias) values (?, ?, ?)`, resourceID, environmentID, alias); err != nil {
			if conflict := classifyResourceConflict(err); conflict != nil {
				return conflict
			}
			return fmt.Errorf("insert alias for resource %d: %w", resourceID, err)
		}
	}
	seenIdentifiers := make(map[string]bool, len(identifiers))
	for _, identifier := range identifiers {
		identifier.System = strings.ToLower(strings.TrimSpace(identifier.System))
		identifier.Value = strings.TrimSpace(identifier.Value)
		key := identifier.System + "\x00" + identifier.Value
		if seenIdentifiers[key] {
			continue
		}
		seenIdentifiers[key] = true
		if _, err := tx.ExecContext(ctx, `insert into resource_external_identifiers (resource_id, external_system, external_value) values (?, ?, ?)`, resourceID, identifier.System, identifier.Value); err != nil {
			if conflict := classifyResourceConflict(err); conflict != nil {
				return conflict
			}
			return fmt.Errorf("insert external identifier for resource %d: %w", resourceID, err)
		}
	}
	return nil
}

func replaceResourceIdentityTx(ctx context.Context, tx *sql.Tx, resourceID, environmentID uint64, input model.ResourceUpdateInput) error {
	if input.Aliases != nil {
		if _, err := tx.ExecContext(ctx, `delete from resource_aliases where resource_id = ?`, resourceID); err != nil {
			return fmt.Errorf("delete aliases for resource %d: %w", resourceID, err)
		}
		if err := insertResourceIdentityTx(ctx, tx, resourceID, environmentID, *input.Aliases, nil); err != nil {
			return err
		}
	} else if input.EnvironmentID != nil {
		if _, err := tx.ExecContext(ctx, `update resource_aliases set environment_id = ? where resource_id = ?`, environmentID, resourceID); err != nil {
			if conflict := classifyResourceConflict(err); conflict != nil {
				return conflict
			}
			return fmt.Errorf("move aliases for resource %d: %w", resourceID, err)
		}
	}
	if input.ExternalIdentifiers != nil {
		if _, err := tx.ExecContext(ctx, `delete from resource_external_identifiers where resource_id = ?`, resourceID); err != nil {
			return fmt.Errorf("delete external identifiers for resource %d: %w", resourceID, err)
		}
		if err := insertResourceIdentityTx(ctx, tx, resourceID, environmentID, nil, *input.ExternalIdentifiers); err != nil {
			return err
		}
	}
	return nil
}

func classifyResourceConflict(err error) error {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1062 {
		return nil
	}
	switch {
	case strings.Contains(mysqlErr.Message, "uq_resource_alias_"):
		return service.ErrResourceAliasConflict
	case strings.Contains(mysqlErr.Message, "uq_resource_external_identifier"):
		return service.ErrResourceExternalIdentifierConflict
	case strings.Contains(mysqlErr.Message, "uq_resource_name_env_type"):
		return service.ErrResourceNameConflict
	default:
		return service.ErrResourceConflict
	}
}

func resourceUpdateStatement(id uint64, input model.ResourceUpdateInput) (string, []any) {
	setClauses := make([]string, 0, 11)
	args := make([]any, 0, 11)
	add := func(column string, value any) {
		setClauses = append(setClauses, column+" = ?")
		args = append(args, value)
	}
	if input.Name != nil {
		add("name", *input.Name)
	}
	if input.ResourceSubtype != nil {
		add("resource_subtype", *input.ResourceSubtype)
	}
	if input.DisplayName != nil {
		add("display_name", *input.DisplayName)
	}
	if input.EnvironmentID != nil {
		add("environment_id", *input.EnvironmentID)
	}
	if input.OwnerID != nil {
		add("owner_id", *input.OwnerID)
	}
	if input.LifecycleStatus != nil {
		add("lifecycle_status", string(*input.LifecycleStatus))
	}
	if input.HealthStatus != nil {
		add("health_status", string(*input.HealthStatus))
	}
	if input.Labels != nil {
		labelsJSON, _ := json.Marshal(*input.Labels)
		add("labels", string(labelsJSON))
	}
	if len(setClauses) == 0 && input.Aliases == nil && input.ExternalIdentifiers == nil {
		return "", nil
	}
	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)
	return "update resources set " + strings.Join(setClauses, ", ") + " where id = ?", args
}

func resourceAuditChanges(before model.Resource, input model.ResourceUpdateInput) []model.AuditChange {
	changes := make([]model.AuditChange, 0, 10)
	add := func(field string, old, next any) {
		if old != next {
			changes = append(changes, model.AuditChange{Field: field, Operation: model.AuditChangeUpdate, Before: old, After: next})
		}
	}
	if input.Name != nil {
		add("identity.name", before.Name, *input.Name)
	}
	if input.ResourceSubtype != nil {
		add("identity.resourceSubtype", before.ResourceSubtype, *input.ResourceSubtype)
	}
	if input.DisplayName != nil {
		add("identity.displayName", before.DisplayName, *input.DisplayName)
	}
	if input.EnvironmentID != nil {
		add("environmentId", before.EnvironmentID, *input.EnvironmentID)
	}
	if input.OwnerID != nil {
		add("ownerId", before.OwnerID, *input.OwnerID)
	}
	if input.LifecycleStatus != nil {
		add("lifecycleStatus", before.LifecycleStatus, string(*input.LifecycleStatus))
	}
	if input.HealthStatus != nil {
		add("manualHealthOverride", before.HealthStatus, string(*input.HealthStatus))
	}
	if input.Aliases != nil && !reflect.DeepEqual(before.Aliases, *input.Aliases) {
		changes = append(changes, model.AuditChange{Field: "identity.aliases", Operation: model.AuditChangeUpdate, Before: before.Aliases, After: *input.Aliases})
	}
	if input.ExternalIdentifiers != nil && !reflect.DeepEqual(before.ExternalIdentifiers, *input.ExternalIdentifiers) {
		changes = append(changes, model.AuditChange{Field: "identity.externalIdentifiers", Operation: model.AuditChangeUpdate, Before: before.ExternalIdentifiers, After: *input.ExternalIdentifiers})
	}
	if input.Labels != nil {
		beforeKeys := make([]string, 0, len(before.Labels))
		for key := range before.Labels {
			beforeKeys = append(beforeKeys, key)
		}
		sort.Strings(beforeKeys)
		for _, key := range beforeKeys {
			old := before.Labels[key]
			next, ok := (*input.Labels)[key]
			switch {
			case !ok:
				changes = append(changes, model.AuditChange{Field: "labels." + key, Operation: model.AuditChangeRemove, Before: old})
			case next != old:
				changes = append(changes, model.AuditChange{Field: "labels." + key, Operation: model.AuditChangeUpdate, Before: old, After: next})
			}
		}
		afterKeys := make([]string, 0, len(*input.Labels))
		for key := range *input.Labels {
			afterKeys = append(afterKeys, key)
		}
		sort.Strings(afterKeys)
		for _, key := range afterKeys {
			next := (*input.Labels)[key]
			if _, ok := before.Labels[key]; !ok {
				changes = append(changes, model.AuditChange{Field: "labels." + key, Operation: model.AuditChangeAdd, After: next})
			}
		}
	}
	return changes
}

func auditMapChanges(prefix string, before, after map[string]any) []model.AuditChange {
	keys := make([]string, 0, len(before)+len(after))
	seen := make(map[string]bool, len(before)+len(after))
	for key := range before {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range after {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	changes := make([]model.AuditChange, 0, len(keys))
	for _, key := range keys {
		old, hadOld := before[key]
		next, hasNext := after[key]
		switch {
		case !hadOld:
			changes = append(changes, model.AuditChange{Field: prefix + key, Operation: model.AuditChangeAdd, After: next})
		case !hasNext:
			changes = append(changes, model.AuditChange{Field: prefix + key, Operation: model.AuditChangeRemove, Before: old})
		case !reflect.DeepEqual(old, next):
			changes = append(changes, model.AuditChange{Field: prefix + key, Operation: model.AuditChangeUpdate, Before: old, After: next})
		}
	}
	return changes
}

func insertInventoryAuditTx(ctx context.Context, tx *sql.Tx, actorUserID, targetResourceID uint64, eventType string, changes []model.AuditChange) error {
	changesJSON, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("marshal inventory audit changes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `insert into audit_events
		(actor_user_id, target_resource_id, event_type, result, changes)
		values (?, ?, ?, 'success', ?)`, actorUserID, targetResourceID, eventType, changesJSON); err != nil {
		return fmt.Errorf("insert inventory audit event: %w", err)
	}
	return nil
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
		&item.Origin,
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
		item.Source = string(item.Origin)
		return item, nil
	}

	if err := json.Unmarshal([]byte(rawLabels), &item.Labels); err != nil {
		return model.Resource{}, err
	}
	item.Source = string(item.Origin)

	return item, nil
}

func (r *ResourceRepository) buildProfileSummary(ctx context.Context, resourceID uint64, resourceType model.ResourceType) *model.ProfileSummary {
	switch resourceType {
	case model.ResourceTypeDatabaseInstance:
		return r.buildInstanceProfileSummary(ctx, resourceID)
	case model.ResourceTypeDatabaseCluster:
		return r.buildClusterProfileSummary(ctx, resourceID)
	case model.ResourceTypeHost:
		return r.buildHostProfileSummary(ctx, resourceID)
	case model.ResourceTypeService:
		return r.buildServiceProfileSummary(ctx, resourceID)
	case model.ResourceTypeDatabaseProxy:
		return r.buildDatabaseProxyProfileSummary(ctx, resourceID)
	default:
		return nil
	}
}

func (r *ResourceRepository) buildInstanceProfileSummary(ctx context.Context, id uint64) *model.ProfileSummary {
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
		Hostname: host,
		Port:     port,
		Engine:   engine,
		Version:  version,
		Role:     role,
	}
}

func (r *ResourceRepository) buildClusterProfileSummary(ctx context.Context, id uint64) *model.ProfileSummary {
	var engine string
	err := r.db.QueryRowContext(ctx,
		`select engine from resource_profiles_database_cluster where resource_id = ?`,
		id,
	).Scan(&engine)
	if err != nil {
		return nil
	}
	var nodeCount int
	r.db.QueryRowContext(ctx,
		`select count(*) from resource_relations where to_resource_id = ? and relation_type = 'member_of'`,
		id,
	).Scan(&nodeCount)
	return &model.ProfileSummary{
		Engine:    engine,
		NodeCount: nodeCount,
	}
}

func (r *ResourceRepository) buildHostProfileSummary(ctx context.Context, id uint64) *model.ProfileSummary {
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

func (r *ResourceRepository) buildServiceProfileSummary(ctx context.Context, id uint64) *model.ProfileSummary {
	var systemName string
	err := r.db.QueryRowContext(ctx,
		`select system_name from resource_profiles_service where resource_id = ?`,
		id,
	).Scan(&systemName)
	if err != nil {
		return nil
	}
	return &model.ProfileSummary{
		Hostname: systemName,
	}
}

func (r *ResourceRepository) buildDatabaseProxyProfileSummary(ctx context.Context, id uint64) *model.ProfileSummary {
	var host, role, version string
	var port int
	err := r.db.QueryRowContext(ctx,
		`select host, port, role, version from resource_profiles_database_proxy where resource_id = ?`,
		id,
	).Scan(&host, &port, &role, &version)
	if err != nil {
		return nil
	}
	return &model.ProfileSummary{
		Hostname: host,
		Port:     port,
		Role:     role,
		Version:  version,
	}
}

// attachDatabaseOperationalSummaries batch-fetches cluster member rollups for
// database_cluster resources in the given list.
func (r *ResourceRepository) attachDatabaseOperationalSummaries(ctx context.Context, items []model.Resource) {
	var clusterIDs []uint64
	for _, item := range items {
		if item.ResourceType == model.ResourceTypeDatabaseCluster {
			clusterIDs = append(clusterIDs, item.ID)
		}
	}
	if len(clusterIDs) == 0 {
		return
	}

	summaries := r.fetchDatabaseOperationalSummaries(ctx, clusterIDs)
	for i := range items {
		if s, ok := summaries[items[i].ID]; ok {
			items[i].DatabaseOperationalSummary = s
		}
	}
}

// fetchDatabaseOperationalSummaries computes operational rollups for the given
// cluster IDs in a single batch query.
func (r *ResourceRepository) fetchDatabaseOperationalSummaries(ctx context.Context, clusterIDs []uint64) map[uint64]*model.DatabaseOperationalSummary {
	ph := buildInClause(len(clusterIDs))
	args := make([]any, len(clusterIDs))
	for i, id := range clusterIDs {
		args[i] = id
	}

	countQuery := `SELECT
		rr.to_resource_id AS cluster_id,
		COUNT(*) AS member_count,
		SUM(CASE WHEN child.health_status = 'critical' THEN 1 ELSE 0 END) AS critical_member_count,
		SUM(CASE WHEN child.health_status = 'warning' THEN 1 ELSE 0 END) AS warning_member_count,
		SUM(CASE WHEN child.lifecycle_status = 'stopped' THEN 1 ELSE 0 END) AS stopped_member_count,
		SUM(CASE WHEN child.lifecycle_status = 'degraded' THEN 1 ELSE 0 END) AS degraded_member_count
	FROM resource_relations rr
	JOIN resources child ON child.id = rr.from_resource_id
	WHERE rr.relation_type = 'member_of'
	  AND rr.to_resource_id IN (` + ph + `)
	GROUP BY rr.to_resource_id`

	rows, err := r.db.QueryContext(ctx, countQuery, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	type counts struct {
		memberCount         int64
		criticalMemberCount int64
		warningMemberCount  int64
		stoppedMemberCount  int64
		degradedMemberCount int64
	}
	countMap := make(map[uint64]*counts)
	for rows.Next() {
		var cid uint64
		var c counts
		if err := rows.Scan(&cid, &c.memberCount, &c.criticalMemberCount, &c.warningMemberCount, &c.stoppedMemberCount, &c.degradedMemberCount); err != nil {
			return nil
		}
		countMap[cid] = &c
	}
	if err := rows.Err(); err != nil {
		return nil
	}

	// Fetch role counts from instance profiles.
	roleQuery := `SELECT
		rr.to_resource_id AS cluster_id,
		SUM(CASE WHEN LOWER(pi.role) IN ('primary','master','writer') THEN 1 ELSE 0 END) AS primary_count,
		SUM(CASE WHEN LOWER(pi.role) IN ('replica','secondary','reader') THEN 1 ELSE 0 END) AS replica_count,
		SUM(CASE WHEN pi.role IS NULL OR pi.role = '' THEN 1 ELSE 0 END) AS unknown_role_count
	FROM resource_relations rr
	JOIN resources child ON child.id = rr.from_resource_id
	LEFT JOIN resource_profiles_database_instance pi ON pi.resource_id = child.id
	WHERE rr.relation_type = 'member_of'
	  AND rr.to_resource_id IN (` + ph + `)
	GROUP BY rr.to_resource_id`

	roleRows, err := r.db.QueryContext(ctx, roleQuery, args...)
	if err != nil {
		// Non-fatal: role data is optional.
		roleRows = nil
	}
	type roleCounts struct {
		primaryCount     int64
		replicaCount     int64
		unknownRoleCount int64
	}
	roleMap := make(map[uint64]*roleCounts)
	if roleRows != nil {
		for roleRows.Next() {
			var cid uint64
			var rc roleCounts
			if err := roleRows.Scan(&cid, &rc.primaryCount, &rc.replicaCount, &rc.unknownRoleCount); err != nil {
				break
			}
			roleMap[cid] = &rc
		}
		roleRows.Close()
	}

	// Fetch worst member for each cluster.
	worstQuery := `SELECT
		rr.to_resource_id AS cluster_id,
		child.id AS member_id,
		child.display_name AS member_name,
		CASE child.health_status
			WHEN 'critical' THEN 4
			WHEN 'warning' THEN 3
			WHEN 'unknown' THEN 2
			ELSE 1
		END AS health_rank,
		CASE child.lifecycle_status
			WHEN 'stopped' THEN 2
			WHEN 'degraded' THEN 1
			ELSE 0
		END AS lifecycle_rank,
		child.health_status,
		child.lifecycle_status
	FROM resource_relations rr
	JOIN resources child ON child.id = rr.from_resource_id
	WHERE rr.relation_type = 'member_of'
	  AND rr.to_resource_id IN (` + ph + `)
	ORDER BY cluster_id, health_rank DESC, lifecycle_rank DESC, child.display_name ASC`

	worstRows, err := r.db.QueryContext(ctx, worstQuery, args...)
	if err != nil {
		return nil
	}
	defer worstRows.Close()

	type worstInfo struct {
		id     int64
		name   string
		status string
	}
	worstMap := make(map[uint64]*worstInfo)
	for worstRows.Next() {
		var cid uint64
		var wi worstInfo
		var healthRank, lifecycleRank int
		var healthStatus, lifecycleStatus string
		if err := worstRows.Scan(&cid, &wi.id, &wi.name, &healthRank, &lifecycleRank, &healthStatus, &lifecycleStatus); err != nil {
			return nil
		}
		if _, exists := worstMap[cid]; !exists {
			wi.status = healthStatus
			if healthStatus == "healthy" || healthStatus == "" {
				if lifecycleStatus == "stopped" {
					wi.status = "stopped"
				} else if lifecycleStatus == "degraded" {
					wi.status = "degraded"
				}
			}
			worstMap[cid] = &wi
		}
	}
	if err := worstRows.Err(); err != nil {
		return nil
	}

	result := make(map[uint64]*model.DatabaseOperationalSummary, len(clusterIDs))
	for _, cid := range clusterIDs {
		c, ok := countMap[cid]
		if !ok {
			continue
		}
		s := &model.DatabaseOperationalSummary{
			MemberCount:         c.memberCount,
			CriticalMemberCount: c.criticalMemberCount,
			WarningMemberCount:  c.warningMemberCount,
			StoppedMemberCount:  c.stoppedMemberCount,
			DegradedMemberCount: c.degradedMemberCount,
		}
		if rc, ok := roleMap[cid]; ok {
			s.PrimaryMemberCount = rc.primaryCount
			s.ReplicaMemberCount = rc.replicaCount
			s.UnknownRoleCount = rc.unknownRoleCount
		}
		if w, ok := worstMap[cid]; ok {
			s.WorstMemberID = &w.id
			s.WorstMemberName = w.name
			s.WorstMemberStatus = w.status
		}
		result[cid] = s
	}
	return result
}

// buildDatabaseOperationalSummary computes a rollup for a single cluster.
func (r *ResourceRepository) buildDatabaseOperationalSummary(ctx context.Context, clusterID uint64) *model.DatabaseOperationalSummary {
	summaries := r.fetchDatabaseOperationalSummaries(ctx, []uint64{clusterID})
	return summaries[clusterID]
}
