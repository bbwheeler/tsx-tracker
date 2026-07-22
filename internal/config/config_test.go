package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("FMP_API_KEY", "test-key-123")
	// Everything else should fall back to defaults.

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051", cfg.GRPCPort)
	}
	if cfg.DBHost != "localhost" {
		t.Errorf("DBHost = %q, want %q", cfg.DBHost, "localhost")
	}
	if cfg.DBPort != 5432 {
		t.Errorf("DBPort = %d, want 5432", cfg.DBPort)
	}
	if cfg.DBUser != "postgres" {
		t.Errorf("DBUser = %q, want %q", cfg.DBUser, "postgres")
	}
	if cfg.DBPassword != "postgres" {
		t.Errorf("DBPassword = %q, want %q", cfg.DBPassword, "postgres")
	}
	if cfg.DBName != "tsx_tracker" {
		t.Errorf("DBName = %q, want %q", cfg.DBName, "tsx_tracker")
	}
	if cfg.DBSSLMode != "disable" {
		t.Errorf("DBSSLMode = %q, want %q", cfg.DBSSLMode, "disable")
	}
	if cfg.FMPAPIKey != "test-key-123" {
		t.Errorf("FMPAPIKey = %q, want %q", cfg.FMPAPIKey, "test-key-123")
	}
	if cfg.FMPBaseURL != "https://financialmodelingprep.com" {
		t.Errorf("FMPBaseURL = %q, want %q", cfg.FMPBaseURL, "https://financialmodelingprep.com")
	}
	if cfg.RefreshCheckInterval != 24*time.Hour {
		t.Errorf("RefreshCheckInterval = %s, want %s", cfg.RefreshCheckInterval, 24*time.Hour)
	}
	if cfg.DailyRefreshCount != 50 {
		t.Errorf("DailyRefreshCount = %d, want 50", cfg.DailyRefreshCount)
	}
	if cfg.ProfileBatchSize != 3 {
		t.Errorf("ProfileBatchSize = %d, want 3", cfg.ProfileBatchSize)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	env := map[string]string{
		"FMP_API_KEY":            "custom-key",
		"GRPC_PORT":              "9999",
		"DB_HOST":                "db.example.com",
		"DB_PORT":                "5433",
		"DB_USER":                "app",
		"DB_PASSWORD":            "secret",
		"DB_NAME":                "mydb",
		"DB_SSLMODE":             "require",
		"FMP_BASE_URL":           "http://localhost:8080",
		"REFRESH_CHECK_INTERVAL": "12h",
		"DAILY_REFRESH_COUNT":    "100",
		"PROFILE_BATCH_SIZE":     "5",
	}
	for k, v := range env {
		t.Setenv(k, v)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GRPCPort != 9999 {
		t.Errorf("GRPCPort = %d, want 9999", cfg.GRPCPort)
	}
	if cfg.DBHost != "db.example.com" {
		t.Errorf("DBHost = %q, want %q", cfg.DBHost, "db.example.com")
	}
	if cfg.DBPort != 5433 {
		t.Errorf("DBPort = %d, want 5433", cfg.DBPort)
	}
	if cfg.DBUser != "app" {
		t.Errorf("DBUser = %q, want %q", cfg.DBUser, "app")
	}
	if cfg.DBPassword != "secret" {
		t.Errorf("DBPassword = %q, want %q", cfg.DBPassword, "secret")
	}
	if cfg.DBName != "mydb" {
		t.Errorf("DBName = %q, want %q", cfg.DBName, "mydb")
	}
	if cfg.DBSSLMode != "require" {
		t.Errorf("DBSSLMode = %q, want %q", cfg.DBSSLMode, "require")
	}
	if cfg.FMPAPIKey != "custom-key" {
		t.Errorf("FMPAPIKey = %q, want %q", cfg.FMPAPIKey, "custom-key")
	}
	if cfg.FMPBaseURL != "http://localhost:8080" {
		t.Errorf("FMPBaseURL = %q, want %q", cfg.FMPBaseURL, "http://localhost:8080")
	}
	if cfg.RefreshCheckInterval != 12*time.Hour {
		t.Errorf("RefreshCheckInterval = %s, want %s", cfg.RefreshCheckInterval, 12*time.Hour)
	}
	if cfg.DailyRefreshCount != 100 {
		t.Errorf("DailyRefreshCount = %d, want 100", cfg.DailyRefreshCount)
	}
	if cfg.ProfileBatchSize != 5 {
		t.Errorf("ProfileBatchSize = %d, want 5", cfg.ProfileBatchSize)
	}
}

func TestLoad_MissingAPIKey(t *testing.T) {
	os.Unsetenv("FMP_API_KEY")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing FMP_API_KEY")
	}
}

func TestLoad_InvalidInt(t *testing.T) {
	t.Setenv("FMP_API_KEY", "key")
	t.Setenv("GRPC_PORT", "not-a-number")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid values should fall back to defaults.
	if cfg.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051 (fallback)", cfg.GRPCPort)
	}
}

func TestLoad_InvalidDuration(t *testing.T) {
	t.Setenv("FMP_API_KEY", "key")
	t.Setenv("REFRESH_CHECK_INTERVAL", "not-a-duration")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RefreshCheckInterval != 24*time.Hour {
		t.Errorf("RefreshCheckInterval = %s, want 24h (fallback)", cfg.RefreshCheckInterval)
	}
}

func TestPostgresDSN(t *testing.T) {
	cfg := &Config{
		DBUser:     "app",
		DBPassword: "s3cret",
		DBHost:     "db.local",
		DBPort:     5433,
		DBName:     "mydb",
		DBSSLMode:  "require",
	}
	want := "postgres://app:s3cret@db.local:5433/mydb?sslmode=require"
	if got := cfg.PostgresDSN(); got != want {
		t.Errorf("PostgresDSN() = %q, want %q", got, want)
	}
}

func TestPostgresDSN_SpecialChars(t *testing.T) {
	cfg := &Config{
		DBUser:     "app",
		DBPassword: "p@ss:word#1",
		DBHost:     "db.local",
		DBPort:     5432,
		DBName:     "mydb",
		DBSSLMode:  "disable",
	}
	got := cfg.PostgresDSN()
	// Password must be URL-encoded so @ : # don't break the DSN.
	if !strings.Contains(got, "app:p%40ss%3Aword%231@db.local") {
		t.Errorf("PostgresDSN() password not properly encoded: %q", got)
	}
}

func TestLoad_InvalidGRPCPort(t *testing.T) {
	t.Setenv("FMP_API_KEY", "key")
	t.Setenv("GRPC_PORT", "99999")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for out-of-range GRPC_PORT")
	}
}

func TestLoad_InvalidDBPort(t *testing.T) {
	t.Setenv("FMP_API_KEY", "key")
	t.Setenv("DB_PORT", "-1")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for negative DB_PORT")
	}
}

func TestLoad_NegativeDailyRefreshCount(t *testing.T) {
	t.Setenv("FMP_API_KEY", "key")
	t.Setenv("DAILY_REFRESH_COUNT", "-5")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for negative DAILY_REFRESH_COUNT")
	}
}

func TestLoad_ProfileBatchSizeZero(t *testing.T) {
	t.Setenv("FMP_API_KEY", "key")
	t.Setenv("PROFILE_BATCH_SIZE", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ProfileBatchSize != 0 {
		t.Errorf("ProfileBatchSize = %d, want 0", cfg.ProfileBatchSize)
	}
}

func TestLoad_DailyRefreshCountZero(t *testing.T) {
	t.Setenv("FMP_API_KEY", "key")
	t.Setenv("DAILY_REFRESH_COUNT", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DailyRefreshCount != 0 {
		t.Errorf("DailyRefreshCount = %d, want 0", cfg.DailyRefreshCount)
	}
}
