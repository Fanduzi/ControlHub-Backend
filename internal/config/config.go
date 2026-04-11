package config

import "os"

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
}

func Load() Config {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	return Config{
		Port:        port,
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
	}
}

func (c Config) HTTPAddress() string {
	return ":" + c.Port
}
