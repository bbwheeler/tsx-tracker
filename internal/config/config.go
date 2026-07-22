// Package config centralizes all runtime configuration, loaded from
// environment variables so the service (database, refresh cadence, API
// keys, etc.) is fully configurable without code changes.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	// gRPC
	GRPCPort int

	// Database (PostgreSQL)
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// Data provider (Financial Modeling Prep)
	FMPAPIKey  string
	FMPBaseURL string

	// Refresh behaviour
	// RefreshCheckInterval: how often the background refresher wakes up
	// to sync the TSX symbol list and refresh a random subset of companies.
	RefreshCheckInterval time.Duration
	// DailyRefreshCount limits how many company profiles are refreshed
	// per cycle. Companies that have not been refreshed within the
	// RefreshCheckInterval are eligible; a random subset of this size is
	// chosen each cycle.
	DailyRefreshCount int
	// ProfileBatchSize is how many symbols are requested per upstream
	// "profile" API call (FMP supports comma-separated batches).
	ProfileBatchSize int
}

func Load() (*Config, error) {
	cfg := &Config{
		GRPCPort:   getEnvInt("GRPC_PORT", 50051),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnvInt("DB_PORT", 5432),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", "postgres"),
		DBName:     getEnv("DB_NAME", "tsx_tracker"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),

		FMPAPIKey:  getEnv("FMP_API_KEY", ""),
		FMPBaseURL: getEnv("FMP_BASE_URL", "https://financialmodelingprep.com"),

		RefreshCheckInterval: getEnvDuration("REFRESH_CHECK_INTERVAL", 24*time.Hour),
		DailyRefreshCount:    getEnvInt("DAILY_REFRESH_COUNT", 50),
		ProfileBatchSize:     getEnvInt("PROFILE_BATCH_SIZE", 3),
	}

	if cfg.FMPAPIKey == "" {
		return nil, fmt.Errorf("FMP_API_KEY is required (get a free key at https://site.financialmodelingprep.com/)")
	}
	if cfg.GRPCPort < 1 || cfg.GRPCPort > 65535 {
		return nil, fmt.Errorf("GRPC_PORT must be 1-65535, got %d", cfg.GRPCPort)
	}
	if cfg.DBPort < 1 || cfg.DBPort > 65535 {
		return nil, fmt.Errorf("DB_PORT must be 1-65535, got %d", cfg.DBPort)
	}
	if cfg.DailyRefreshCount < 0 {
		return nil, fmt.Errorf("DAILY_REFRESH_COUNT must be >= 0, got %d", cfg.DailyRefreshCount)
	}
	if cfg.ProfileBatchSize < 0 {
		return nil, fmt.Errorf("PROFILE_BATCH_SIZE must be >= 0, got %d", cfg.ProfileBatchSize)
	}
	if cfg.RefreshCheckInterval <= 0 {
		return nil, fmt.Errorf("REFRESH_CHECK_INTERVAL must be > 0, got %s", cfg.RefreshCheckInterval)
	}

	return cfg, nil
}

func (c *Config) PostgresDSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   fmt.Sprintf("%s:%d", c.DBHost, c.DBPort),
		Path:   c.DBName,
	}
	q := u.Query()
	q.Set("sslmode", c.DBSSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
