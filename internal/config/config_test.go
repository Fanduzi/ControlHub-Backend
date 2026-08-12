// Package config tests validate configuration loading behavior.
// input: internal/config (Config, Load, LoadDotEnv, ValidateJWTSecret, ErrQueryExecutionTokenMaxAgeRejected)
// output: TestConfig* test functions
// pos: Validates config loading, signing-secret validation, and fixed-freshness enforcement
// note: if config loading or validation changes, update this header
package config

import (
	"os"
	"path/filepath"
	"strings"
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

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}

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

func TestValidateJWTSecret_RejectsBlankAndWhitespaceOnly(t *testing.T) {
	for _, secret := range []string{"", "   ", "\t", "\n", " \t\n "} {
		if err := ValidateJWTSecret(secret); err == nil {
			t.Errorf("ValidateJWTSecret(%q) = nil, want error", secret)
		}
	}
}

func TestValidateJWTSecret_RejectsKnownPlaceholders(t *testing.T) {
	// ADR phase-38x item 6: a deployed instance must not boot with a
	// guessable signing key, so documented placeholders are hard errors.
	for _, secret := range []string{"change-me", "CHANGE-ME", "ChangE-Me", "changeme", "secret", "SECRET", "your-secret-key", "override-secret", "OVERRIDE-SECRET", "oVeRrIdE-sEcReT", "<generated-hex-value>", "<GENERATED-HEX-VALUE>"} {
		err := ValidateJWTSecret(secret)
		if err == nil {
			t.Errorf("ValidateJWTSecret(%q) = nil, want error", secret)
			continue
		}
		// The error must be the fixed sentinel: rejection text never varies
		// with (and so never echoes) the rejected secret.
		if err != ErrInvalidJWTSecret {
			t.Errorf("ValidateJWTSecret(%q) = %v, want ErrInvalidJWTSecret", secret, err)
		}
	}
}

func TestValidateJWTSecret_AcceptsNonPlaceholderSecrets(t *testing.T) {
	for _, secret := range []string{strings.Repeat("a", 32), "9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a3b2c1d0e9f8a7"} {
		if err := ValidateJWTSecret(secret); err != nil {
			t.Errorf("ValidateJWTSecret(%q) = %v, want nil", secret, err)
		}
	}
}

func TestValidateJWTSecret_RejectsShortSecrets(t *testing.T) {
	for _, secret := range []string{"test-secret", "ci-e2e-secret", strings.Repeat("a", 31)} {
		err := ValidateJWTSecret(secret)
		if err != ErrInvalidJWTSecret {
			t.Errorf("ValidateJWTSecret(%q) = %v, want ErrInvalidJWTSecret", secret, err)
		}
	}
}

func TestLoadRejectsQueryExecutionTokenMaxAgeEnvVar(t *testing.T) {
	// WHY: the eight-hour freshness bound is a fixed backend contract (Issue #21).
	// Supplying QUERY_EXECUTION_TOKEN_MAX_AGE must fail startup with a controlled
	// error so no deployment can silently extend the Operator Access Boundary.
	t.Setenv("QUERY_EXECUTION_TOKEN_MAX_AGE", "12h")
	unsetEnv(t, "APP_PORT")
	unsetEnv(t, "DATABASE_DSN")
	unsetEnv(t, "JWT_SECRET")

	_, err := Load()
	if err != ErrQueryExecutionTokenMaxAgeRejected {
		t.Fatalf("Load() with QUERY_EXECUTION_TOKEN_MAX_AGE set: err = %v, want ErrQueryExecutionTokenMaxAgeRejected", err)
	}
}

func TestLoadAcceptsMissingQueryExecutionTokenMaxAgeEnvVar(t *testing.T) {
	// WHY: the eight-hour constant is hardcoded in the middleware; Load() must
	// succeed when the deprecated env var is absent.
	unsetEnv(t, "QUERY_EXECUTION_TOKEN_MAX_AGE")
	unsetEnv(t, "APP_PORT")
	unsetEnv(t, "DATABASE_DSN")
	unsetEnv(t, "JWT_SECRET")

	_, err := Load()
	if err != nil {
		t.Fatalf("Load() without QUERY_EXECUTION_TOKEN_MAX_AGE: err = %v", err)
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
