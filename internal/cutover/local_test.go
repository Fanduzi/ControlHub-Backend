package cutover

import (
	"context"
	"errors"
	"reflect"
	"testing"

	gosqlmysql "github.com/go-sql-driver/mysql"
)

func TestRunLocalPreserveThenImport_PreservesRebuildsAndImports(t *testing.T) {
	ctx := context.Background()
	admin := &fakeAdminStore{
		databaseExists: map[string]bool{
			"controlhub":    true,
			"controlhub_v1": false,
		},
		tables: map[string][]string{
			"controlhub": {"goose_db_version", "roles", "resources"},
		},
	}

	var migrateTarget string
	var preparedTarget string
	var importConfig ImportConfig

	err := runLocalPreserveThenImport(ctx, LocalCutoverConfig{
		RuntimeDSN:      "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4",
		PreserveDBName:  "controlhub_v1",
		TargetDBName:    "controlhub",
	}, localCutoverDeps{
		openAdmin: func(string) (adminStore, error) { return admin, nil },
		runMigrations: func(context.Context, string) error {
			migrateTarget = "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4"
			return nil
		},
		prepareTarget: func(_ context.Context, targetDSN string) error {
			preparedTarget = targetDSN
			return nil
		},
		importLegacyData: func(_ context.Context, cfg ImportConfig) error {
			importConfig = cfg
			return nil
		},
	})
	if err != nil {
		t.Fatalf("run local preserve-then-import: %v", err)
	}

	wantCalls := []string{
		"databaseExists:controlhub",
		"listTables:controlhub",
		"databaseExists:controlhub_v1",
		"columnDataType:controlhub.resources.id",
		"createDatabase:controlhub_v1",
		"renameTables:controlhub->controlhub_v1:goose_db_version,resources,roles",
		"dropDatabase:controlhub",
		"createDatabase:controlhub",
		"close",
	}
	if !reflect.DeepEqual(admin.calls, wantCalls) {
		t.Fatalf("admin calls = %#v, want %#v", admin.calls, wantCalls)
	}

	assertDSNDatabaseName(t, migrateTarget, "controlhub")
	assertDSNDatabaseName(t, preparedTarget, "controlhub")
	assertDSNDatabaseName(t, importConfig.SourceDSN, "controlhub_v1")
	assertDSNDatabaseName(t, importConfig.TargetDSN, "controlhub")
}

func TestRunLocalPreserveThenImport_RejectsNonEmptyPreservedDatabaseWhenRuntimeIsStillLegacy(t *testing.T) {
	ctx := context.Background()
	admin := &fakeAdminStore{
		databaseExists: map[string]bool{
			"controlhub":    true,
			"controlhub_v1": true,
		},
		tables: map[string][]string{
			"controlhub":    {"roles", "resources"},
			"controlhub_v1": {"roles"},
		},
		columnDataTypes: map[string]string{
			"controlhub.resources.id": "char",
		},
	}

	err := runLocalPreserveThenImport(ctx, LocalCutoverConfig{
		RuntimeDSN:     "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4",
		PreserveDBName: "controlhub_v1",
		TargetDBName:   "controlhub",
	}, localCutoverDeps{
		openAdmin:     func(string) (adminStore, error) { return admin, nil },
		runMigrations: func(context.Context, string) error { return nil },
		prepareTarget: func(context.Context, string) error {
			t.Fatal("prepare target should not run when preserved database is non-empty and runtime is still legacy")
			return nil
		},
		importLegacyData: func(context.Context, ImportConfig) error { return nil },
	})
	if err == nil {
		t.Fatal("expected non-empty preserved database with legacy runtime to be rejected")
	}
	if err.Error() != "preserved database controlhub_v1 already contains data and runtime database controlhub is still legacy schema" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLocalPreserveThenImport_RejectsMissingRuntimeDatabase(t *testing.T) {
	ctx := context.Background()
	admin := &fakeAdminStore{
		databaseExists: map[string]bool{
			"controlhub": false,
		},
	}

	err := runLocalPreserveThenImport(ctx, LocalCutoverConfig{
		RuntimeDSN:   "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4",
		TargetDBName: "controlhub",
	}, localCutoverDeps{
		openAdmin:     func(string) (adminStore, error) { return admin, nil },
		runMigrations: func(context.Context, string) error { return nil },
		prepareTarget: func(context.Context, string) error {
			t.Fatal("prepare target should not run when runtime database is missing")
			return nil
		},
		importLegacyData: func(context.Context, ImportConfig) error { return nil },
	})
	if err == nil {
		t.Fatal("expected missing runtime database to be rejected")
	}
	if err.Error() != "runtime database controlhub does not exist" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLocalPreserveThenImport_PropagatesMigrationFailure(t *testing.T) {
	ctx := context.Background()
	admin := &fakeAdminStore{
		databaseExists: map[string]bool{
			"controlhub":    true,
			"controlhub_v1": false,
		},
		tables: map[string][]string{
			"controlhub": {"roles"},
		},
	}

	err := runLocalPreserveThenImport(ctx, LocalCutoverConfig{
		RuntimeDSN:     "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4",
		PreserveDBName: "controlhub_v1",
		TargetDBName:   "controlhub",
	}, localCutoverDeps{
		openAdmin:     func(string) (adminStore, error) { return admin, nil },
		runMigrations: func(context.Context, string) error { return errors.New("boom") },
		prepareTarget: func(context.Context, string) error {
			t.Fatal("prepare target should not run when migration fails")
			return nil
		},
		importLegacyData: func(context.Context, ImportConfig) error {
			t.Fatal("import should not run when migration fails")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected migration failure")
	}
	if err.Error() != "run target migrations: boom" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLocalPreserveThenImport_RejectsResumeWithoutExplicitFlag(t *testing.T) {
	ctx := context.Background()
	admin := &fakeAdminStore{
		databaseExists: map[string]bool{
			"controlhub":    true,
			"controlhub_v1": true,
		},
		tables: map[string][]string{
			"controlhub":    {"goose_db_version", "roles", "resources"},
			"controlhub_v1": {"goose_db_version", "roles", "resources"},
		},
		columnDataTypes: map[string]string{
			"controlhub.resources.id": "bigint",
		},
	}

	err := runLocalPreserveThenImport(ctx, LocalCutoverConfig{
		RuntimeDSN:     "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4",
		PreserveDBName: "controlhub_v1",
		TargetDBName:   "controlhub",
	}, localCutoverDeps{
		openAdmin:     func(string) (adminStore, error) { return admin, nil },
		runMigrations: func(context.Context, string) error { return nil },
		prepareTarget: func(context.Context, string) error {
			t.Fatal("prepare target should not run without explicit resume flag")
			return nil
		},
		importLegacyData: func(context.Context, ImportConfig) error { return nil },
	})
	if err == nil {
		t.Fatal("expected resume without explicit flag to be rejected")
	}
	if err.Error() != "preserved database controlhub_v1 already contains data; rerun with resume enabled to continue cutover" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLocalPreserveThenImport_ResumesFromExistingPreservedDatabase(t *testing.T) {
	ctx := context.Background()
	admin := &fakeAdminStore{
		databaseExists: map[string]bool{
			"controlhub":    true,
			"controlhub_v1": true,
		},
		tables: map[string][]string{
			"controlhub":    {"goose_db_version", "roles", "resources"},
			"controlhub_v1": {"goose_db_version", "roles", "resources"},
		},
		columnDataTypes: map[string]string{
			"controlhub.resources.id": "bigint",
		},
	}

	var preparedTarget string
	var importConfig ImportConfig

	err := runLocalPreserveThenImport(ctx, LocalCutoverConfig{
		RuntimeDSN:     "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4",
		PreserveDBName: "controlhub_v1",
		TargetDBName:   "controlhub",
		Resume:         true,
	}, localCutoverDeps{
		openAdmin:     func(string) (adminStore, error) { return admin, nil },
		runMigrations: func(context.Context, string) error { return nil },
		prepareTarget: func(_ context.Context, targetDSN string) error {
			preparedTarget = targetDSN
			return nil
		},
		importLegacyData: func(_ context.Context, cfg ImportConfig) error {
			importConfig = cfg
			return nil
		},
	})
	if err != nil {
		t.Fatalf("resume local preserve-then-import: %v", err)
	}

	wantCalls := []string{
		"databaseExists:controlhub",
		"listTables:controlhub",
		"databaseExists:controlhub_v1",
		"listTables:controlhub_v1",
		"columnDataType:controlhub.resources.id",
		"dropDatabase:controlhub",
		"createDatabase:controlhub",
		"close",
	}
	if !reflect.DeepEqual(admin.calls, wantCalls) {
		t.Fatalf("admin calls = %#v, want %#v", admin.calls, wantCalls)
	}

	assertDSNDatabaseName(t, preparedTarget, "controlhub")
	assertDSNDatabaseName(t, importConfig.SourceDSN, "controlhub_v1")
	assertDSNDatabaseName(t, importConfig.TargetDSN, "controlhub")
}

func TestRunLocalPreserveThenImport_RejectsEmptyPreserveDatabaseWhenRuntimeIsAlreadyBigint(t *testing.T) {
	ctx := context.Background()
	admin := &fakeAdminStore{
		databaseExists: map[string]bool{
			"controlhub":    true,
			"controlhub_v1": true,
		},
		tables: map[string][]string{
			"controlhub":    {"goose_db_version", "roles", "resources"},
			"controlhub_v1": {},
		},
		columnDataTypes: map[string]string{
			"controlhub.resources.id": "bigint",
		},
	}

	err := runLocalPreserveThenImport(ctx, LocalCutoverConfig{
		RuntimeDSN:     "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4",
		PreserveDBName: "controlhub_v1",
		TargetDBName:   "controlhub",
	}, localCutoverDeps{
		openAdmin:     func(string) (adminStore, error) { return admin, nil },
		runMigrations: func(context.Context, string) error { return nil },
		prepareTarget: func(context.Context, string) error {
			t.Fatal("prepare target should not run when runtime is already bigint and preserve db is empty")
			return nil
		},
		importLegacyData: func(context.Context, ImportConfig) error { return nil },
	})
	if err == nil {
		t.Fatal("expected bigint runtime with empty preserve db to be rejected")
	}
	if err.Error() != "runtime database controlhub already uses bigint schema; refuse to preserve it as legacy source" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLocalPreserveThenImport_PropagatesPrepareTargetFailure(t *testing.T) {
	ctx := context.Background()
	admin := &fakeAdminStore{
		databaseExists: map[string]bool{
			"controlhub":    true,
			"controlhub_v1": false,
		},
		tables: map[string][]string{
			"controlhub": {"roles"},
		},
	}

	err := runLocalPreserveThenImport(ctx, LocalCutoverConfig{
		RuntimeDSN:     "controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4",
		PreserveDBName: "controlhub_v1",
		TargetDBName:   "controlhub",
	}, localCutoverDeps{
		openAdmin:     func(string) (adminStore, error) { return admin, nil },
		runMigrations: func(context.Context, string) error { return nil },
		prepareTarget: func(context.Context, string) error { return errors.New("prepare failed") },
		importLegacyData: func(context.Context, ImportConfig) error {
			t.Fatal("import should not run when prepare target fails")
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected prepare target failure")
	}
	if err.Error() != "prepare target database controlhub: prepare failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

type fakeAdminStore struct {
	databaseExists  map[string]bool
	tables          map[string][]string
	columnDataTypes map[string]string
	calls           []string
}

func (f *fakeAdminStore) Close() error {
	f.calls = append(f.calls, "close")
	return nil
}

func (f *fakeAdminStore) DatabaseExists(_ context.Context, dbName string) (bool, error) {
	f.calls = append(f.calls, "databaseExists:"+dbName)
	return f.databaseExists[dbName], nil
}

func (f *fakeAdminStore) ListTables(_ context.Context, dbName string) ([]string, error) {
	f.calls = append(f.calls, "listTables:"+dbName)
	return append([]string(nil), f.tables[dbName]...), nil
}

func (f *fakeAdminStore) CreateDatabase(_ context.Context, dbName string) error {
	f.calls = append(f.calls, "createDatabase:"+dbName)
	return nil
}

func (f *fakeAdminStore) DropDatabase(_ context.Context, dbName string) error {
	f.calls = append(f.calls, "dropDatabase:"+dbName)
	return nil
}

func (f *fakeAdminStore) RenameTables(_ context.Context, fromDB string, toDB string, tables []string) error {
	f.calls = append(f.calls, "renameTables:"+fromDB+"->"+toDB+":"+joinStrings(tables))
	return nil
}

func (f *fakeAdminStore) ColumnDataType(_ context.Context, dbName string, tableName string, columnName string) (string, error) {
	f.calls = append(f.calls, "columnDataType:"+dbName+"."+tableName+"."+columnName)
	return f.columnDataTypes[dbName+"."+tableName+"."+columnName], nil
}

func joinStrings(items []string) string {
	if len(items) == 0 {
		return ""
	}
	joined := items[0]
	for _, item := range items[1:] {
		joined += "," + item
	}
	return joined
}

func assertDSNDatabaseName(t *testing.T, dsn string, wantDBName string) {
	t.Helper()
	cfg, err := gosqlmysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse dsn %q: %v", dsn, err)
	}
	if cfg.DBName != wantDBName {
		t.Fatalf("dsn db name = %q, want %q", cfg.DBName, wantDBName)
	}
	if !cfg.ParseTime {
		t.Fatalf("dsn parseTime = false, want true")
	}
}
