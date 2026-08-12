// Package config loads environment variables into a typed Config struct.
// input: os.Getenv, godotenv, time
// output: LoadDotEnv, Load, Config struct, HTTPAddress, ValidateJWTSecret, ErrInvalidJWTSecret, ErrQueryExecutionTokenMaxAgeRejected
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
	if os.Getenv("QUERY_EXECUTION_TOKEN_MAX_AGE") != "" {
		return Config{}, ErrQueryExecutionTokenMaxAgeRejected
	}

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	return Config{
		Port:        port,
		DatabaseDSN: os.Getenv("DATABASE_DSN"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}, nil
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
