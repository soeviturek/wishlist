package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all application configuration.
type Config struct {
	Server    ServerConfig
	Database  DatabaseConfig
	Scheduler SchedulerConfig
	SMTP      SMTPConfig
	Debug     bool
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int
}

// DatabaseConfig holds database settings.
type DatabaseConfig struct {
	Path string
}

// SchedulerConfig holds polling schedule settings.
type SchedulerConfig struct {
	Cron string
}

// SMTPConfig holds email settings.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port: getEnvInt("SERVER_PORT", 8080),
		},
		Database: DatabaseConfig{
			Path: getEnv("DATABASE_PATH", "./wishlist.db"),
		},
		Scheduler: SchedulerConfig{
			Cron: getEnv("SCHEDULER_CRON", "0 3 * * *"),
		},
		SMTP: SMTPConfig{
			Host:     getEnv("SMTP_HOST", "smtp.gmail.com"),
			Port:     getEnvInt("SMTP_PORT", 587),
			Username: getEnv("SMTP_USERNAME", ""),
			Password: getEnv("SMTP_PASSWORD", ""),
			From:     getEnv("SMTP_FROM", ""),
		},
		Debug: getEnvBool("DEBUG", false),
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val, ok := os.LookupEnv(key); ok {
		n, err := strconv.Atoi(val)
		if err == nil {
			return n
		}
		fmt.Printf("warning: invalid integer for %s: %s\n", key, val)
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return fallback
}
