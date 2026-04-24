package cutover

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"

	gosqlmysql "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
)

type LocalCutoverConfig struct {
	RuntimeDSN     string
	PreserveDBName string
	TargetDBName   string
	Resume         bool
}

type adminStore interface {
	Close() error
	DatabaseExists(ctx context.Context, dbName string) (bool, error)
	ListTables(ctx context.Context, dbName string) ([]string, error)
	CreateDatabase(ctx context.Context, dbName string) error
	DropDatabase(ctx context.Context, dbName string) error
	RenameTables(ctx context.Context, fromDB string, toDB string, tables []string) error
	ColumnDataType(ctx context.Context, dbName string, tableName string, columnName string) (string, error)
}

type localCutoverDeps struct {
	openAdmin        func(runtimeDSN string) (adminStore, error)
	runMigrations    func(ctx context.Context, targetDSN string) error
	prepareTarget    func(ctx context.Context, targetDSN string) error
	importLegacyData func(ctx context.Context, cfg ImportConfig) error
}

func RunLocalPreserveThenImport(ctx context.Context, cfg LocalCutoverConfig) error {
	return runLocalPreserveThenImport(ctx, cfg, localCutoverDeps{
		openAdmin:        openMySQLAdminStore,
		runMigrations:    runGooseMigrations,
		prepareTarget:    truncateTargetBusinessTables,
		importLegacyData: ImportLegacyData,
	})
}

func runLocalPreserveThenImport(ctx context.Context, cfg LocalCutoverConfig, deps localCutoverDeps) error {
	if cfg.RuntimeDSN == "" {
		return fmt.Errorf("runtime dsn is required")
	}
	if cfg.TargetDBName == "" {
		return fmt.Errorf("target database name is required")
	}
	if cfg.PreserveDBName == "" {
		cfg.PreserveDBName = cfg.TargetDBName + "_v1"
	}
	if cfg.PreserveDBName == cfg.TargetDBName {
		return fmt.Errorf("preserve database name must differ from target database name")
	}
	if deps.openAdmin == nil {
		return fmt.Errorf("open admin dependency is required")
	}
	if deps.runMigrations == nil {
		return fmt.Errorf("run migrations dependency is required")
	}
	if deps.prepareTarget == nil {
		return fmt.Errorf("prepare target dependency is required")
	}
	if deps.importLegacyData == nil {
		return fmt.Errorf("import legacy data dependency is required")
	}
	if err := validateImportDSN(cfg.RuntimeDSN, "runtime"); err != nil {
		return err
	}

	admin, err := deps.openAdmin(cfg.RuntimeDSN)
	if err != nil {
		return fmt.Errorf("open admin connection: %w", err)
	}
	defer admin.Close()

	runtimeExists, err := admin.DatabaseExists(ctx, cfg.TargetDBName)
	if err != nil {
		return fmt.Errorf("check runtime database %s: %w", cfg.TargetDBName, err)
	}
	if !runtimeExists {
		return fmt.Errorf("runtime database %s does not exist", cfg.TargetDBName)
	}

	tables, err := admin.ListTables(ctx, cfg.TargetDBName)
	if err != nil {
		return fmt.Errorf("list runtime tables for %s: %w", cfg.TargetDBName, err)
	}
	if len(tables) == 0 {
		return fmt.Errorf("runtime database %s has no tables to preserve", cfg.TargetDBName)
	}
	sort.Strings(tables)

	preserveExists, err := admin.DatabaseExists(ctx, cfg.PreserveDBName)
	if err != nil {
		return fmt.Errorf("check preserved database %s: %w", cfg.PreserveDBName, err)
	}
	if preserveExists {
		preservedTables, err := admin.ListTables(ctx, cfg.PreserveDBName)
		if err != nil {
			return fmt.Errorf("list preserved tables for %s: %w", cfg.PreserveDBName, err)
		}
		if len(preservedTables) != 0 {
			legacyRuntime, err := isLegacyRuntimeSchema(ctx, admin, cfg.TargetDBName)
			if err != nil {
				return err
			}
			if legacyRuntime {
				return fmt.Errorf("preserved database %s already contains data and runtime database %s is still legacy schema", cfg.PreserveDBName, cfg.TargetDBName)
			}
			if !cfg.Resume {
				return fmt.Errorf("preserved database %s already contains data; rerun with resume enabled to continue cutover", cfg.PreserveDBName)
			}
		} else {
			legacyRuntime, err := isLegacyRuntimeSchema(ctx, admin, cfg.TargetDBName)
			if err != nil {
				return err
			}
			if !legacyRuntime {
				return fmt.Errorf("runtime database %s already uses bigint schema; refuse to preserve it as legacy source", cfg.TargetDBName)
			}
			if err := admin.RenameTables(ctx, cfg.TargetDBName, cfg.PreserveDBName, tables); err != nil {
				return fmt.Errorf("rename runtime tables into %s: %w", cfg.PreserveDBName, err)
			}
		}
	} else {
		legacyRuntime, err := isLegacyRuntimeSchema(ctx, admin, cfg.TargetDBName)
		if err != nil {
			return err
		}
		if !legacyRuntime {
			return fmt.Errorf("runtime database %s already uses bigint schema; refuse to preserve it as legacy source", cfg.TargetDBName)
		}
		if err := admin.CreateDatabase(ctx, cfg.PreserveDBName); err != nil {
			return fmt.Errorf("create preserved database %s: %w", cfg.PreserveDBName, err)
		}
		if err := admin.RenameTables(ctx, cfg.TargetDBName, cfg.PreserveDBName, tables); err != nil {
			return fmt.Errorf("rename runtime tables into %s: %w", cfg.PreserveDBName, err)
		}
	}

	if err := admin.DropDatabase(ctx, cfg.TargetDBName); err != nil {
		return fmt.Errorf("drop rebuilt target database %s placeholder: %w", cfg.TargetDBName, err)
	}
	if err := admin.CreateDatabase(ctx, cfg.TargetDBName); err != nil {
		return fmt.Errorf("create target database %s: %w", cfg.TargetDBName, err)
	}

	targetDSN, err := dsnWithDatabaseName(cfg.RuntimeDSN, cfg.TargetDBName)
	if err != nil {
		return fmt.Errorf("build target dsn: %w", err)
	}
	preservedDSN, err := dsnWithDatabaseName(cfg.RuntimeDSN, cfg.PreserveDBName)
	if err != nil {
		return fmt.Errorf("build preserved dsn: %w", err)
	}

	if err := deps.runMigrations(ctx, targetDSN); err != nil {
		return fmt.Errorf("run target migrations: %w", err)
	}
	if err := deps.prepareTarget(ctx, targetDSN); err != nil {
		return fmt.Errorf("prepare target database %s: %w", cfg.TargetDBName, err)
	}
	if err := deps.importLegacyData(ctx, ImportConfig{SourceDSN: preservedDSN, TargetDSN: targetDSN}); err != nil {
		return fmt.Errorf("import preserved legacy data: %w", err)
	}

	return nil
}

type mysqlAdminStore struct {
	db *sql.DB
}

func openMySQLAdminStore(runtimeDSN string) (adminStore, error) {
	cfg, err := gosqlmysql.ParseDSN(runtimeDSN)
	if err != nil {
		return nil, fmt.Errorf("parse runtime dsn: %w", err)
	}
	cfg.DBName = ""
	adminDB, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open admin db: %w", err)
	}
	return &mysqlAdminStore{db: adminDB}, nil
}

func (s *mysqlAdminStore) Close() error {
	return s.db.Close()
}

func (s *mysqlAdminStore) DatabaseExists(ctx context.Context, dbName string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		select count(*)
		from information_schema.schemata
		where schema_name = ?`, dbName).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *mysqlAdminStore) ListTables(ctx context.Context, dbName string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		select table_name
		from information_schema.tables
		where table_schema = ?
		order by table_name`, dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tables = append(tables, tableName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tables, nil
}

func (s *mysqlAdminStore) CreateDatabase(ctx context.Context, dbName string) error {
	quotedDBName, err := quoteMySQLIdentifier(dbName)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "CREATE DATABASE "+quotedDBName+" CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci")
	return err
}

func (s *mysqlAdminStore) DropDatabase(ctx context.Context, dbName string) error {
	quotedDBName, err := quoteMySQLIdentifier(dbName)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quotedDBName)
	return err
}

func (s *mysqlAdminStore) RenameTables(ctx context.Context, fromDB string, toDB string, tables []string) error {
	if len(tables) == 0 {
		return nil
	}
	quotedFromDB, err := quoteMySQLIdentifier(fromDB)
	if err != nil {
		return err
	}
	quotedToDB, err := quoteMySQLIdentifier(toDB)
	if err != nil {
		return err
	}
	query := "RENAME TABLE "
	for index, tableName := range tables {
		quotedTableName, err := quoteMySQLIdentifier(tableName)
		if err != nil {
			return err
		}
		if index > 0 {
			query += ", "
		}
		query += fmt.Sprintf("%s.%s TO %s.%s", quotedFromDB, quotedTableName, quotedToDB, quotedTableName)
	}
	_, err = s.db.ExecContext(ctx, query)
	return err
}

func (s *mysqlAdminStore) ColumnDataType(ctx context.Context, dbName string, tableName string, columnName string) (string, error) {
	var dataType string
	if err := s.db.QueryRowContext(ctx, `
		select data_type
		from information_schema.columns
		where table_schema = ? and table_name = ? and column_name = ?`, dbName, tableName, columnName).Scan(&dataType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("column %s.%s.%s not found", dbName, tableName, columnName)
		}
		return "", err
	}
	return dataType, nil
}

func dsnWithDatabaseName(runtimeDSN string, dbName string) (string, error) {
	cfg, err := gosqlmysql.ParseDSN(runtimeDSN)
	if err != nil {
		return "", fmt.Errorf("parse runtime dsn: %w", err)
	}
	cfg.DBName = dbName
	return cfg.FormatDSN(), nil
}

func isLegacyRuntimeSchema(ctx context.Context, admin adminStore, dbName string) (bool, error) {
	dataType, err := admin.ColumnDataType(ctx, dbName, "resources", "id")
	if err != nil {
		return false, fmt.Errorf("inspect runtime schema for %s: %w", dbName, err)
	}
	return dataType != "bigint", nil
}

func runGooseMigrations(ctx context.Context, targetDSN string) error {
	cfg, err := gosqlmysql.ParseDSN(targetDSN)
	if err != nil {
		return fmt.Errorf("parse target dsn: %w", err)
	}
	cfg.ParseTime = false

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return fmt.Errorf("open migration db: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migration db: %w", err)
	}
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(db, resolveMigrationsDir()); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

func truncateTargetBusinessTables(ctx context.Context, targetDSN string) error {
	db, err := sql.Open("mysql", targetDSN)
	if err != nil {
		return fmt.Errorf("open target db: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping target db: %w", err)
	}
	for _, tableName := range targetBusinessTables {
		quotedTableName, err := quoteMySQLIdentifier(tableName)
		if err != nil {
			return fmt.Errorf("quote target table %s: %w", tableName, err)
		}
		if _, err := db.ExecContext(ctx, "TRUNCATE TABLE "+quotedTableName); err != nil {
			return fmt.Errorf("truncate target table %s: %w", tableName, err)
		}
	}
	return nil
}

func resolveMigrationsDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("could not determine cutover file path")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	abs, err := filepath.Abs(dir)
	if err != nil {
		panic(fmt.Sprintf("resolve migrations dir: %v", err))
	}
	return abs
}

func quoteMySQLIdentifier(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("mysql identifier is required")
	}
	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' {
			continue
		}
		return "", fmt.Errorf("mysql identifier %q contains unsupported character %q", name, string(char))
	}
	return "`" + name + "`", nil
}
