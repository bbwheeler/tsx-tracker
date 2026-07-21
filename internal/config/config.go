// Package config centralizes all runtime configuration, loaded from
// environment variables so the service (database, refresh cadence, API
// keys, etc.) is fully configurable without code changes.
package config

import (
	"fmt"
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
	// StalenessThreshold: a company's data is considered stale (and must
	// be re-fetched) once it is older than this. The task requires <= 24h.
	StalenessThreshold time.Duration
	// RefreshCheckInterval: how often the background refresher wakes up
	// to look for stale/missing companies.
	RefreshCheckInterval time.Duration
	// MaxCompaniesPerRefreshCycle limits how many company profiles are
	// fetched from the upstream API per refresh tick, so the service stays
	// within a free-tier API rate limit while still cycling through the
	// whole tracked universe within StalenessThreshold.
	MaxCompaniesPerRefreshCycle int
	// ProfileBatchSize is how many symbols are requested per upstream
	// "profile" API call (FMP supports comma-separated batches).
	ProfileBatchSize int
	// MaxTrackedCompanies caps the total number of TSX symbols the service
	// tracks. Free-tier market data APIs cannot support fetching fresh
	// fundamentals for the full ~1,800+ symbol TSX universe every 24h
	// without a paid plan; this keeps the demo/deployment realistic. Set
	// to 0 for "no cap" if you have a paid API plan with higher limits.
	MaxTrackedCompanies int
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

		StalenessThreshold:          getEnvDuration("STALENESS_THRESHOLD", 24*time.Hour),
		RefreshCheckInterval:        getEnvDuration("REFRESH_CHECK_INTERVAL", 30*time.Minute),
		MaxCompaniesPerRefreshCycle: getEnvInt("MAX_COMPANIES_PER_REFRESH_CYCLE", 50),
		ProfileBatchSize:            getEnvInt("PROFILE_BATCH_SIZE", 3),
		MaxTrackedCompanies:         getEnvInt("MAX_TRACKED_COMPANIES", 300),
	}

	if cfg.FMPAPIKey == "" {
		return nil, fmt.Errorf("FMP_API_KEY is required (get a free key at https://site.financialmodelingprep.com/)")
	}
	if cfg.StalenessThreshold > 24*time.Hour {
		return nil, fmt.Errorf("STALENESS_THRESHOLD must be <= 24h, got %s", cfg.StalenessThreshold)
	}

	return cfg, nil
}

func (c *Config) PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
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
