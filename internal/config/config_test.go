package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvLoadsMissingValuesWithoutOverridingExistingEnv(t *testing.T) {
	t.Setenv("DATABASE_DSN", "env-dsn")
	unsetEnv(t, "JWT_SECRET")
	unsetEnv(t, "APP_PORT")

	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	if err := os.WriteFile(envFile, []byte("DATABASE_DSN=file-dsn\nJWT_SECRET=file-secret\nAPP_PORT=9090\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("load dot env: %v", err)
	}

	cfg := Load()

	if cfg.DatabaseDSN != "env-dsn" {
		t.Fatalf("expected DATABASE_DSN from exported env, got %q", cfg.DatabaseDSN)
	}

	if cfg.JWTSecret != "file-secret" {
		t.Fatalf("expected JWT_SECRET from .env, got %q", cfg.JWTSecret)
	}

	if cfg.Port != "9090" {
		t.Fatalf("expected APP_PORT from .env, got %q", cfg.Port)
	}
}

func TestLoadDotEnvIgnoresMissingFile(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatalf("expected missing .env to be ignored, got %v", err)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()

	value, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}

	t.Cleanup(func() {
		var err error
		if existed {
			err = os.Setenv(key, value)
		} else {
			err = os.Unsetenv(key)
		}

		if err != nil {
			t.Fatalf("restore %s: %v", key, err)
		}
	})
}
