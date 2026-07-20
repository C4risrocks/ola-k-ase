package main

import (
	"os"
	"strings"
)

type Config struct {
	Port         string
	DatabasePath string
	AppEnv       string
	LogLevel     string
}

func LoadConfig() Config {
	return Config{
		Port:         getEnvOrDefault("PORT", "8080"),
		DatabasePath: getEnvOrDefault("DATABASE_PATH", "/data/site.db"),
		AppEnv:       strings.ToLower(getEnvOrDefault("APP_ENV", "development")),
		LogLevel:     strings.ToLower(getEnvOrDefault("LOG_LEVEL", "info")),
	}
}

func getEnvOrDefault(key, fallback string) string {
	if val, exists := os.LookupEnv(key); exists {
		return val
	}
	return fallback
}
