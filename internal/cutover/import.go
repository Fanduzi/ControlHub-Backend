// Package cutover preserves legacy data into the bigint runtime schema.
// input: context, database/sql, errors, fmt, go-sql-driver/mysql
// output: ImportLegacyData, ImportConfig
// pos: One-shot migration boundary translating legacy UUID identities into current bigint rows inside a single target transaction
// note: if this file changes, update this header and README.md
package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
	gosqlmysql "github.com/go-sql-driver/mysql"
)

type ImportConfig struct {
	SourceDSN string
	TargetDSN string
}

type importer struct {
	source *sql.DB
	target *sql.DB
}

var targetBusinessTables = []string{
	"roles",
	"users",
	"environments",
	"owners",
	"resources",
	"resource_profiles_host",
	"resource_profiles_database_instance",
	"resource_profiles_database_cluster",
	"resource_profiles_service",
	"resource_relations",
	"audit_events",
}

type roleRow struct {
	ID          string
	Name        string
	Description string
	CreatedAt   any
}

type environmentRow struct {
	ID          string
	Name        string
	Slug        string
	Description string
	CreatedAt   any
}

type ownerRow struct {
	ID        string
	Name      string
	Email     string
	CreatedAt any
}

type userRow struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	RoleID       string
	CreatedAt    any
}

type resourceRow struct {
	ID              string
	ResourceType    string
	ResourceSubtype string
	Name            string
	DisplayName     string
	EnvironmentID   string
	OwnerID         string
	LifecycleStatus string
	HealthStatus    string
	Labels          string
	Source          string
	ExternalID      string
	CreatedAt       any
	UpdatedAt       any
	ArchivedAt      sql.NullTime
	ArchivedBy      sql.NullString
	ArchiveReason   sql.NullString
}

type hostProfileRow struct {
	ResourceID string
	Hostname   string
	IPAddress  string
	OSName     string
}

type dbInstanceProfileRow struct {
	ResourceID string
	Engine     string
	Version    string
	Host       string
	Port       int
	Role       string
}

type dbClusterProfileRow struct {
	ResourceID      string
	Engine          string
	TopologyMode    string
	PrimaryEndpoint string
}

type serviceProfileRow struct {
	ResourceID    string
	SystemName    string
	RepositoryURL string
	RuntimeEnv    string
}

type relationRow struct {
	FromResourceID string
	ToResourceID   string
	RelationType   string
	CreatedAt      any
}

type auditRow struct {
	ActorUserID      sql.NullString
	TargetResourceID sql.NullString
	EventType        string
	Result           string
	CreatedAt        any
}

func ImportLegacyData(ctx context.Context, cfg ImportConfig) error {
	if cfg.SourceDSN == "" {
		return errors.New("source dsn is required")
	}
	if cfg.TargetDSN == "" {
		return errors.New("target dsn is required")
	}
	if err := validateImportDSN(cfg.SourceDSN, "source"); err != nil {
		return err
	}
	if err := validateImportDSN(cfg.TargetDSN, "target"); err != nil {
		return err
	}

	sourceDB, err := sql.Open("mysql", cfg.SourceDSN)
	if err != nil {
		return fmt.Errorf("open source db: %w", err)
	}
	defer sourceDB.Close()

	targetDB, err := sql.Open("mysql", cfg.TargetDSN)
	if err != nil {
		return fmt.Errorf("open target db: %w", err)
	}
	defer targetDB.Close()

	if err := sourceDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping source db: %w", err)
	}
	if err := targetDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping target db: %w", err)
	}

	imp := importer{source: sourceDB, target: targetDB}
	return imp.run(ctx)
}

func (imp importer) run(ctx context.Context) error {
	if err := imp.ensureEmptyTarget(ctx); err != nil {
		return err
	}

	tx, err := imp.target.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin target transaction: %w", err)
	}
	defer tx.Rollback()

	roleMap, err := imp.importRoles(ctx, tx)
	if err != nil {
		return err
	}
	environmentMap, err := imp.importEnvironments(ctx, tx)
	if err != nil {
		return err
	}
	ownerMap, err := imp.importOwners(ctx, tx)
	if err != nil {
		return err
	}
	userMap, err := imp.importUsers(ctx, tx, roleMap)
	if err != nil {
		return err
	}
	resourceMap, err := imp.importResources(ctx, tx, environmentMap, ownerMap, userMap)
	if err != nil {
		return err
	}
	if err := imp.importHostProfiles(ctx, tx, resourceMap); err != nil {
		return err
	}
	if err := imp.importDatabaseInstanceProfiles(ctx, tx, resourceMap); err != nil {
		return err
	}
	if err := imp.importDatabaseClusterProfiles(ctx, tx, resourceMap); err != nil {
		return err
	}
	if err := imp.importServiceProfiles(ctx, tx, resourceMap); err != nil {
		return err
	}
	if err := imp.importRelations(ctx, tx, resourceMap); err != nil {
		return err
	}
	if err := imp.importAuditEvents(ctx, tx, userMap, resourceMap); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit target transaction: %w", err)
	}
	return nil
}

func (imp importer) importRoles(ctx context.Context, tx *sql.Tx) (map[string]uint64, error) {
	rows, err := imp.source.QueryContext(ctx, `select id, name, description, created_at from roles order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("query source roles: %w", err)
	}
	defer rows.Close()

	mapping := make(map[string]uint64)
	for rows.Next() {
		var row roleRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Description, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan source role: %w", err)
		}
		result, err := tx.ExecContext(ctx, `insert into roles (name, description, created_at) values (?, ?, ?)`, row.Name, row.Description, row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("insert role %s: %w", row.Name, err)
		}
		newID, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("role last insert id %s: %w", row.Name, err)
		}
		mapping[row.ID] = uint64(newID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source roles: %w", err)
	}
	return mapping, nil
}

func (imp importer) importEnvironments(ctx context.Context, tx *sql.Tx) (map[string]uint64, error) {
	rows, err := imp.source.QueryContext(ctx, `select id, name, slug, description, created_at from environments order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("query source environments: %w", err)
	}
	defer rows.Close()

	mapping := make(map[string]uint64)
	for rows.Next() {
		var row environmentRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Slug, &row.Description, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan source environment: %w", err)
		}
		result, err := tx.ExecContext(ctx, `insert into environments (name, slug, description, created_at) values (?, ?, ?, ?)`, row.Name, row.Slug, row.Description, row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("insert environment %s: %w", row.Slug, err)
		}
		newID, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("environment last insert id %s: %w", row.Slug, err)
		}
		mapping[row.ID] = uint64(newID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source environments: %w", err)
	}
	return mapping, nil
}

func (imp importer) importOwners(ctx context.Context, tx *sql.Tx) (map[string]uint64, error) {
	rows, err := imp.source.QueryContext(ctx, `select id, name, email, created_at from owners order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("query source owners: %w", err)
	}
	defer rows.Close()

	mapping := make(map[string]uint64)
	for rows.Next() {
		var row ownerRow
		if err := rows.Scan(&row.ID, &row.Name, &row.Email, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan source owner: %w", err)
		}
		result, err := tx.ExecContext(ctx, `insert into owners (name, email, created_at) values (?, ?, ?)`, row.Name, row.Email, row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("insert owner %s: %w", row.Email, err)
		}
		newID, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("owner last insert id %s: %w", row.Email, err)
		}
		mapping[row.ID] = uint64(newID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source owners: %w", err)
	}
	return mapping, nil
}

func (imp importer) importUsers(ctx context.Context, tx *sql.Tx, roleMap map[string]uint64) (map[string]uint64, error) {
	rows, err := imp.source.QueryContext(ctx, `select id, email, password_hash, display_name, role_id, created_at from users order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("query source users: %w", err)
	}
	defer rows.Close()

	mapping := make(map[string]uint64)
	for rows.Next() {
		var row userRow
		if err := rows.Scan(&row.ID, &row.Email, &row.PasswordHash, &row.DisplayName, &row.RoleID, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan source user: %w", err)
		}
		roleID, ok := roleMap[row.RoleID]
		if !ok {
			return nil, fmt.Errorf("missing role mapping for legacy user %s", row.Email)
		}
		result, err := tx.ExecContext(ctx, `insert into users (email, password_hash, display_name, role_id, created_at) values (?, ?, ?, ?, ?)`, row.Email, row.PasswordHash, row.DisplayName, roleID, row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("insert user %s: %w", row.Email, err)
		}
		newID, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("user last insert id %s: %w", row.Email, err)
		}
		mapping[row.ID] = uint64(newID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source users: %w", err)
	}
	return mapping, nil
}

func (imp importer) importResources(ctx context.Context, tx *sql.Tx, environmentMap, ownerMap, userMap map[string]uint64) (map[string]uint64, error) {
	rows, err := imp.source.QueryContext(ctx, `
		select id, resource_type, resource_subtype, name, display_name, environment_id, owner_id,
		       lifecycle_status, health_status, labels, source, external_id, created_at, updated_at,
		       archived_at, archived_by, archive_reason
		from resources
		order by created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("query source resources: %w", err)
	}
	defer rows.Close()

	mapping := make(map[string]uint64)
	for rows.Next() {
		var row resourceRow
		if err := rows.Scan(
			&row.ID,
			&row.ResourceType,
			&row.ResourceSubtype,
			&row.Name,
			&row.DisplayName,
			&row.EnvironmentID,
			&row.OwnerID,
			&row.LifecycleStatus,
			&row.HealthStatus,
			&row.Labels,
			&row.Source,
			&row.ExternalID,
			&row.CreatedAt,
			&row.UpdatedAt,
			&row.ArchivedAt,
			&row.ArchivedBy,
			&row.ArchiveReason,
		); err != nil {
			return nil, fmt.Errorf("scan source resource: %w", err)
		}
		environmentID, ok := environmentMap[row.EnvironmentID]
		if !ok {
			return nil, fmt.Errorf("missing environment mapping for legacy resource %s", row.Name)
		}
		ownerID, ok := ownerMap[row.OwnerID]
		if !ok {
			return nil, fmt.Errorf("missing owner mapping for legacy resource %s", row.Name)
		}
		var archivedBy any
		if row.ArchivedBy.Valid {
			mappedArchivedBy, ok := userMap[row.ArchivedBy.String]
			if !ok {
				return nil, fmt.Errorf("missing archived_by mapping for legacy resource %s", row.Name)
			}
			archivedBy = mappedArchivedBy
		}
		var archivedAt any
		if row.ArchivedAt.Valid {
			archivedAt = row.ArchivedAt.Time
		}
		var archiveReason any
		if row.ArchiveReason.Valid {
			archiveReason = row.ArchiveReason.String
		}
		result, err := tx.ExecContext(ctx, `
			insert into resources (
				resource_type, resource_subtype, name, display_name, environment_id, owner_id,
				lifecycle_status, health_status, labels, source, external_id, created_at, updated_at,
				archived_at, archived_by, archive_reason
			) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.ResourceType,
			row.ResourceSubtype,
			row.Name,
			row.DisplayName,
			environmentID,
			ownerID,
			row.LifecycleStatus,
			row.HealthStatus,
			row.Labels,
			row.Source,
			row.ExternalID,
			row.CreatedAt,
			row.UpdatedAt,
			archivedAt,
			archivedBy,
			archiveReason,
		)
		if err != nil {
			return nil, fmt.Errorf("insert resource %s: %w", row.Name, err)
		}
		newID, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("resource last insert id %s: %w", row.Name, err)
		}
		mapping[row.ID] = uint64(newID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate source resources: %w", err)
	}
	return mapping, nil
}

func (imp importer) importHostProfiles(ctx context.Context, tx *sql.Tx, resourceMap map[string]uint64) error {
	if ok, err := imp.sourceTableExists(ctx, "resource_profiles_host"); err != nil {
		return err
	} else if !ok {
		return nil
	}
	rows, err := imp.source.QueryContext(ctx, `select resource_id, hostname, ip_address, os_name from resource_profiles_host`)
	if err != nil {
		return fmt.Errorf("query source host profiles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row hostProfileRow
		if err := rows.Scan(&row.ResourceID, &row.Hostname, &row.IPAddress, &row.OSName); err != nil {
			return fmt.Errorf("scan source host profile: %w", err)
		}
		resourceID, ok := resourceMap[row.ResourceID]
		if !ok {
			return fmt.Errorf("missing resource mapping for host profile %s", row.Hostname)
		}
		if _, err := tx.ExecContext(ctx, `insert into resource_profiles_host (resource_id, hostname, ip_address, os_name, spec) values (?, ?, ?, ?, '{}')`, resourceID, row.Hostname, row.IPAddress, row.OSName); err != nil {
			return fmt.Errorf("insert host profile %s: %w", row.Hostname, err)
		}
	}
	return rows.Err()
}

func (imp importer) importDatabaseInstanceProfiles(ctx context.Context, tx *sql.Tx, resourceMap map[string]uint64) error {
	if ok, err := imp.sourceTableExists(ctx, "resource_profiles_database_instance"); err != nil {
		return err
	} else if !ok {
		return nil
	}
	rows, err := imp.source.QueryContext(ctx, `select resource_id, engine, version, host, port, role from resource_profiles_database_instance`)
	if err != nil {
		return fmt.Errorf("query source database instance profiles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row dbInstanceProfileRow
		if err := rows.Scan(&row.ResourceID, &row.Engine, &row.Version, &row.Host, &row.Port, &row.Role); err != nil {
			return fmt.Errorf("scan source database instance profile: %w", err)
		}
		resourceID, ok := resourceMap[row.ResourceID]
		if !ok {
			return fmt.Errorf("missing resource mapping for database instance profile %s", row.Host)
		}
		if _, err := tx.ExecContext(ctx, `insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) values (?, ?, ?, ?, ?, ?, '{}')`, resourceID, row.Engine, row.Version, row.Host, row.Port, row.Role); err != nil {
			return fmt.Errorf("insert database instance profile %s: %w", row.Host, err)
		}
	}
	return rows.Err()
}

func (imp importer) importDatabaseClusterProfiles(ctx context.Context, tx *sql.Tx, resourceMap map[string]uint64) error {
	if ok, err := imp.sourceTableExists(ctx, "resource_profiles_database_cluster"); err != nil {
		return err
	} else if !ok {
		return nil
	}
	rows, err := imp.source.QueryContext(ctx, `select resource_id, engine, topology_mode, primary_endpoint from resource_profiles_database_cluster`)
	if err != nil {
		return fmt.Errorf("query source database cluster profiles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row dbClusterProfileRow
		if err := rows.Scan(&row.ResourceID, &row.Engine, &row.TopologyMode, &row.PrimaryEndpoint); err != nil {
			return fmt.Errorf("scan source database cluster profile: %w", err)
		}
		resourceID, ok := resourceMap[row.ResourceID]
		if !ok {
			return fmt.Errorf("missing resource mapping for database cluster profile %s", row.PrimaryEndpoint)
		}
		if _, err := tx.ExecContext(ctx, `insert into resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint, spec) values (?, ?, ?, ?, '{}')`, resourceID, row.Engine, row.TopologyMode, row.PrimaryEndpoint); err != nil {
			return fmt.Errorf("insert database cluster profile %s: %w", row.PrimaryEndpoint, err)
		}
	}
	return rows.Err()
}

func (imp importer) importServiceProfiles(ctx context.Context, tx *sql.Tx, resourceMap map[string]uint64) error {
	if ok, err := imp.sourceTableExists(ctx, "resource_profiles_service"); err != nil {
		return err
	} else if !ok {
		return nil
	}
	rows, err := imp.source.QueryContext(ctx, `select resource_id, system_name, repository_url, runtime_env from resource_profiles_service`)
	if err != nil {
		return fmt.Errorf("query source service profiles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row serviceProfileRow
		if err := rows.Scan(&row.ResourceID, &row.SystemName, &row.RepositoryURL, &row.RuntimeEnv); err != nil {
			return fmt.Errorf("scan source service profile: %w", err)
		}
		resourceID, ok := resourceMap[row.ResourceID]
		if !ok {
			return fmt.Errorf("missing resource mapping for service profile %s", row.SystemName)
		}
		if _, err := tx.ExecContext(ctx, `insert into resource_profiles_service (resource_id, system_name, repository_url, runtime_env, spec) values (?, ?, ?, ?, '{}')`, resourceID, row.SystemName, row.RepositoryURL, row.RuntimeEnv); err != nil {
			return fmt.Errorf("insert service profile %s: %w", row.SystemName, err)
		}
	}
	return rows.Err()
}

func (imp importer) importRelations(ctx context.Context, tx *sql.Tx, resourceMap map[string]uint64) error {
	rows, err := imp.source.QueryContext(ctx, `select from_resource_id, to_resource_id, relation_type, created_at from resource_relations order by created_at`)
	if err != nil {
		return fmt.Errorf("query source relations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row relationRow
		if err := rows.Scan(&row.FromResourceID, &row.ToResourceID, &row.RelationType, &row.CreatedAt); err != nil {
			return fmt.Errorf("scan source relation: %w", err)
		}
		fromID, ok := resourceMap[row.FromResourceID]
		if !ok {
			return fmt.Errorf("missing from_resource mapping for relation %s", row.RelationType)
		}
		toID, ok := resourceMap[row.ToResourceID]
		if !ok {
			return fmt.Errorf("missing to_resource mapping for relation %s", row.RelationType)
		}
		if _, err := tx.ExecContext(ctx, `insert into resource_relations (from_resource_id, to_resource_id, relation_type, created_at) values (?, ?, ?, ?)`, fromID, toID, row.RelationType, row.CreatedAt); err != nil {
			return fmt.Errorf("insert relation %s: %w", row.RelationType, err)
		}
	}
	return rows.Err()
}

func (imp importer) importAuditEvents(ctx context.Context, tx *sql.Tx, userMap, resourceMap map[string]uint64) error {
	rows, err := imp.source.QueryContext(ctx, `select actor_user_id, target_resource_id, event_type, result, created_at from audit_events order by created_at`)
	if err != nil {
		return fmt.Errorf("query source audit events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row auditRow
		if err := rows.Scan(&row.ActorUserID, &row.TargetResourceID, &row.EventType, &row.Result, &row.CreatedAt); err != nil {
			return fmt.Errorf("scan source audit event: %w", err)
		}
		// A NULL source actor (migration 00017: anonymous authentication outcomes
		// have no verified actor) must import as NULL without fabricated
		// attribution; a non-NULL source actor must map or the import fails loud.
		var actorUserID any
		if row.ActorUserID.Valid {
			mappedActorID, ok := userMap[row.ActorUserID.String]
			if !ok {
				return fmt.Errorf("missing actor user mapping for audit event %s", row.EventType)
			}
			actorUserID = mappedActorID
		}
		var targetResourceID any
		if row.TargetResourceID.Valid {
			mappedTargetID, ok := resourceMap[row.TargetResourceID.String]
			if !ok {
				return fmt.Errorf("missing target resource mapping for audit event %s", row.EventType)
			}
			targetResourceID = mappedTargetID
		}
		if _, err := tx.ExecContext(ctx, `insert into audit_events (actor_user_id, target_resource_id, event_type, result, created_at) values (?, ?, ?, ?, ?)`, actorUserID, targetResourceID, row.EventType, row.Result, row.CreatedAt); err != nil {
			return fmt.Errorf("insert audit event %s: %w", row.EventType, err)
		}
	}
	return rows.Err()
}

func (imp importer) sourceTableExists(ctx context.Context, tableName string) (bool, error) {
	var count int
	if err := imp.source.QueryRowContext(ctx, `
		select count(*)
		from information_schema.tables
		where table_schema = database() and table_name = ?`, tableName).Scan(&count); err != nil {
		return false, fmt.Errorf("check source table %s: %w", tableName, err)
	}
	return count > 0, nil
}

func (imp importer) ensureEmptyTarget(ctx context.Context) error {
	for _, tableName := range targetBusinessTables {
		var count int
		if err := imp.target.QueryRowContext(ctx, `SELECT count(*) FROM `+tableName).Scan(&count); err != nil {
			return fmt.Errorf("count target table %s: %w", tableName, err)
		}
		if count != 0 {
			return fmt.Errorf("target table %s must be empty before import", tableName)
		}
	}
	return nil
}

func validateImportDSN(dsn string, label string) error {
	cfg, err := gosqlmysql.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse %s dsn: %w", label, err)
	}
	if !cfg.ParseTime {
		return fmt.Errorf("%s dsn must set parseTime=true", label)
	}
	return nil
}
