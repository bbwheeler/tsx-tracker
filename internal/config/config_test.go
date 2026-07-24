package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
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
	if cfg.RefreshCheckInterval != 24*time.Hour {
		t.Errorf("RefreshCheckInterval = %s, want %s", cfg.RefreshCheckInterval, 24*time.Hour)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	env := map[string]string{
		"GRPC_PORT":              "9999",
		"DB_HOST":                "db.example.com",
		"DB_PORT":                "5433",
		"DB_USER":                "app",
		"DB_PASSWORD":            "secret",
		"DB_NAME":                "mydb",
		"DB_SSLMODE":             "require",
		"REFRESH_CHECK_INTERVAL": "12h",
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
	if cfg.RefreshCheckInterval != 12*time.Hour {
		t.Errorf("RefreshCheckInterval = %s, want %s", cfg.RefreshCheckInterval, 12*time.Hour)
	}
}

func TestLoad_InvalidInt(t *testing.T) {
	t.Setenv("GRPC_PORT", "not-a-number")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GRPCPort != 50051 {
		t.Errorf("GRPCPort = %d, want 50051 (fallback)", cfg.GRPCPort)
	}
}

func TestLoad_InvalidDuration(t *testing.T) {
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
	if !strings.Contains(got, "app:p%40ss%3Aword%231@db.local") {
		t.Errorf("PostgresDSN() password not properly encoded: %q", got)
	}
}

func TestLoad_InvalidGRPCPort(t *testing.T) {
	t.Setenv("GRPC_PORT", "99999")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for out-of-range GRPC_PORT")
	}
}

func TestLoad_InvalidDBPort(t *testing.T) {
	t.Setenv("DB_PORT", "-1")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for negative DB_PORT")
	}
}
