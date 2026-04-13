package config

import (
	"errors"
	"io/fs"
	"os"

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

func Load() Config {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	return Config{
		Port:        port,
		DatabaseDSN: os.Getenv("DATABASE_DSN"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}
}

func (c Config) HTTPAddress() string {
	return ":" + c.Port
}
