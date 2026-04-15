//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/go-sql-driver/mysql"
)

// globalEnv is the shared container started once in TestMain.
var globalEnv *testEnv

// testEnv holds the disposable MySQL container and a *sql.DB connected
// to the fully-migrated test database.
type testEnv struct {
	container *tcmysql.MySQLContainer
	db        *sql.DB
	dsn       string
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	c, err := tcmysql.Run(ctx,
		"mysql:8.0",
		tcmysql.WithDatabase("controlhub_test"),
		tcmysql.WithUsername("root"),
		tcmysql.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("port: 3306  MySQL Community Server - GPL").
				WithOccurrence(1).
				WithStartupTimeout(120*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "start mysql container: %v\n", err)
		os.Exit(1)
	}

	host, err := c.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "get container host: %v\n", err)
		c.Terminate(ctx)
		os.Exit(1)
	}
	port, err := c.MappedPort(ctx, "3306")
	if err != nil {
		fmt.Fprintf(os.Stderr, "get container port: %v\n", err)
		c.Terminate(ctx)
		os.Exit(1)
	}

	dsn := fmt.Sprintf("root:test@tcp(%s:%s)/controlhub_test?parseTime=true&charset=utf8mb4", host, port.Port())

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		c.Terminate(ctx)
		os.Exit(1)
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(30 * time.Second)

	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ping db: %v\n", err)
		db.Close()
		c.Terminate(ctx)
		os.Exit(1)
	}

	// Run goose migrations.
	migrationsDir := resolveMigrationsDir()
	goose.SetDialect("mysql")
	if err := goose.Up(db, migrationsDir); err != nil {
		fmt.Fprintf(os.Stderr, "goose up: %v\n", err)
		db.Close()
		c.Terminate(ctx)
		os.Exit(1)
	}

	globalEnv = &testEnv{
		container: c,
		db:        db,
		dsn:       dsn,
	}

	code := m.Run()

	db.Close()
	c.Terminate(ctx)
	os.Exit(code)
}

// setupTestDB returns a clean *sql.DB connection to the shared migrated
// test database. Each test gets its own connection so they can run in
// parallel without sharing session state.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", globalEnv.dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// resolveMigrationsDir returns the absolute path to migrations/.
func resolveMigrationsDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("could not determine test file path")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	abs, err := filepath.Abs(dir)
	if err != nil {
		panic(fmt.Sprintf("resolve migrations dir: %v", err))
	}
	return abs
}
