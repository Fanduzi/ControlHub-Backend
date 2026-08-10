// Package config loads environment variables into a typed Config struct.
// input: os.Getenv, godotenv, time
// output: LoadDotEnv, Load, Config struct, HTTPAddress, ValidateJWTSecret, ErrInvalidJWTSecret
// pos: Configuration loading and signing-secret validation layer
// note: if config vars or validation change, update this header and internal/config/README.md
package config

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseDSN string
	JWTSecret   string
	// QueryExecutionTokenMaxAge bounds how old a bearer token may be for query
	// execution routes. Defaults to 8h; zero/invalid fails closed (reject all).
	QueryExecutionTokenMaxAge time.Duration
}

func LoadDotEnv(filenames ...string) error {
	if len(filenames) == 0 {
		filenames = []string{".env"}
	}

	if err := godotenv.Load(filenames...); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return err
	}

	return nil
}

func Load() Config {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	return Config{
		Port:                      port,
		DatabaseDSN:               os.Getenv("DATABASE_DSN"),
		JWTSecret:                 os.Getenv("JWT_SECRET"),
		QueryExecutionTokenMaxAge: loadDuration("QUERY_EXECUTION_TOKEN_MAX_AGE", 8*time.Hour),
	}
}

func (c Config) HTTPAddress() string {
	return ":" + c.Port
}

// loadDuration parses a duration env var, falling back to def on missing/invalid
// input. An invalid value falls back rather than failing closed because a bad
// duration must not stop the server from booting; the query middleware fails
// closed only on a zero configured max age.
func loadDuration(key string, def time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return def
	}
	return parsed
}

// ErrInvalidJWTSecret reports a blank, short, or known-placeholder signing
// secret. The message is fixed and never echoes the secret itself.
var ErrInvalidJWTSecret = errors.New("JWT_SECRET must be at least 32 bytes and must not be a known placeholder")

// jwtSecretPlaceholders are the documented placeholder signing secrets a
// server must refuse to boot with (ADR phase-38x item 6): the widely reused
// override-secret example and the .env.example <generated-hex-value> marker,
// so an unedited example file can never be accepted. Compared
// case-insensitively after trimming.
var jwtSecretPlaceholders = map[string]struct{}{
	"change-me":             {},
	"changeme":              {},
	"secret":                {},
	"your-secret-key":       {},
	"override-secret":       {},
	"<generated-hex-value>": {},
}

// ValidateJWTSecret rejects blank, whitespace-only, short, and known-placeholder
// signing secrets so startup fails before any service that signs tokens runs.
func ValidateJWTSecret(secret string) error {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return ErrInvalidJWTSecret
	}
	if _, ok := jwtSecretPlaceholders[strings.ToLower(trimmed)]; ok {
		return ErrInvalidJWTSecret
	}
	// len(string) counts UTF-8 bytes, which is the signing-secret contract.
	if len(trimmed) < 32 {
		return ErrInvalidJWTSecret
	}
	return nil
}
