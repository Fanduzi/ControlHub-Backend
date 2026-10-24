// Package config loads environment variables into a typed Config struct.
// input: os.LookupEnv, os.Getenv, godotenv
// output: LoadDotEnv, Load, Config struct, HTTPAddress, ValidateJWTSecret, ErrInvalidJWTSecret, ErrQueryExecutionTokenMaxAgeRejected, ErrInvalidPGHTTPEntries
// pos: Configuration loading and signing-secret validation layer
// note: if config vars or validation change, update this header and internal/config/README.md
package config

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	DatabaseDSN string
	JWTSecret   string
	// PGHTTPEntries is the production PostgreSQL HTTP release gate: true only
	// when CONTROLHUB_PG_HTTP_ENTRIES is exactly "on" at process start.
	PGHTTPEntries bool
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

// ErrQueryExecutionTokenMaxAgeRejected is returned by Load when the deprecated
// QUERY_EXECUTION_TOKEN_MAX_AGE environment variable is set. The eight-hour
// freshness bound is a fixed backend contract (Issue #21) and must not be
// configurable.
var ErrQueryExecutionTokenMaxAgeRejected = errors.New("QUERY_EXECUTION_TOKEN_MAX_AGE is removed; the eight-hour freshness bound is a fixed backend contract")

func Load() (Config, error) {
	if _, ok := os.LookupEnv("QUERY_EXECUTION_TOKEN_MAX_AGE"); ok {
		return Config{}, ErrQueryExecutionTokenMaxAgeRejected
	}

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	pgHTTPEntries, err := parsePGHTTPEntries(os.Getenv("CONTROLHUB_PG_HTTP_ENTRIES"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:          port,
		DatabaseDSN:   os.Getenv("DATABASE_DSN"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		PGHTTPEntries: pgHTTPEntries,
	}, nil
}

// ErrInvalidPGHTTPEntries is returned by Load when
// CONTROLHUB_PG_HTTP_ENTRIES holds a value outside the exact accepted set —
// the production release gate fails closed at startup rather than guessing
// at a typo. The message is fixed and never echoes the raw value.
var ErrInvalidPGHTTPEntries = errors.New("CONTROLHUB_PG_HTTP_ENTRIES must be unset, empty, \"off\", or \"on\" (exact match)")

// parsePGHTTPEntries maps the deployment gate to a boolean. Unset, empty,
// and "off" are the only closed spellings; "on" is the only opening
// spelling; every other value — including "true", "1", "ON", or
// whitespace-padded variants — is a configuration error, never a silent
// default. Matching is exact: no trimming, no case folding.
func parsePGHTTPEntries(raw string) (bool, error) {
	switch raw {
	case "", "off":
		return false, nil
	case "on":
		return true, nil
	default:
		return false, ErrInvalidPGHTTPEntries
	}
}

func (c Config) HTTPAddress() string {
	return ":" + c.Port
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
