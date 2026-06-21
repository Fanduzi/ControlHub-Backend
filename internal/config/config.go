// Package config loads environment variables into a typed Config struct.
// input: os.Getenv, godotenv, time
// output: LoadDotEnv, Load, Config struct, HTTPAddress
// pos: Configuration loading layer
// note: if config vars change, update this header and internal/config/README.md
package config

import (
	"errors"
	"io/fs"
	"os"
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
