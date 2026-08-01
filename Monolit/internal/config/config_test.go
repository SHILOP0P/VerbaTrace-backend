package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndAppConfig(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	values := map[string]string{
		"HTTP_HOST": "127.0.0.1", "HTTP_PORT": "8080", "HTTP_READ_TIMEOUT": "5s",
		"POSTGRES_HOST": "localhost", "POSTGRES_PORT": "5432", "POSTGRES_DB": "verbatrace",
		"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "password", "POSTGRES_SSL_MODE": "disable",
		"MIGRATION_DIRECTORY": "migrations", "UPLOAD_PATH": "uploads", "PASSWORD_PEPPER": "pepper",
		"JWT_SECRET": "jwt-secret", "JWT_ACCESS_TOKEN_TTL": "15m",
		"REFRESH_TOKEN_SECRET": "refresh-secret", "REFRESH_TOKEN_TTL": "24h",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}

	if err := Load(envFile); err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg := AppConfig()
	if cfg == nil || cfg.HTTPConfig.Address() != "127.0.0.1:8080" || cfg.Postgres.DatabaseName() != "verbatrace" ||
		cfg.Upload.Path() != "uploads" || cfg.Auth.JWTSecret() != "jwt-secret" {
		t.Fatalf("unexpected app config: %+v", cfg)
	}
}

func TestLoadErrorsAndNewConfig(t *testing.T) {
	if NewConfig() == nil {
		t.Fatal("NewConfig returned nil")
	}

	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HTTP_HOST", "localhost")
	t.Setenv("HTTP_PORT", "8080")
	t.Setenv("HTTP_READ_TIMEOUT", "invalid")
	if err := Load(envFile); err == nil {
		t.Fatal("expected invalid config error")
	}
}

func TestLoadAllowsMissingEnvFileWhenEnvironmentIsSet(t *testing.T) {
	values := map[string]string{
		"HTTP_HOST": "127.0.0.1", "HTTP_PORT": "8080", "HTTP_READ_TIMEOUT": "5s",
		"POSTGRES_HOST": "localhost", "POSTGRES_PORT": "5432", "POSTGRES_DB": "verbatrace",
		"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "password", "POSTGRES_SSL_MODE": "disable",
		"MIGRATION_DIRECTORY": "migrations", "UPLOAD_PATH": "uploads", "PASSWORD_PEPPER": "pepper",
		"JWT_SECRET": "jwt-secret", "JWT_ACCESS_TOKEN_TTL": "15m",
		"REFRESH_TOKEN_SECRET": "refresh-secret", "REFRESH_TOKEN_TTL": "24h",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}

	if err := Load(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatalf("Load with missing env file and explicit environment: %v", err)
	}
}

func TestLoadLaterValidationErrors(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		"HTTP_HOST": "localhost", "HTTP_PORT": "8080", "HTTP_READ_TIMEOUT": "5s",
		"POSTGRES_HOST": "localhost", "POSTGRES_PORT": "5432", "POSTGRES_DB": "verbatrace",
		"POSTGRES_USER": "postgres", "POSTGRES_PASSWORD": "password", "POSTGRES_SSL_MODE": "disable",
		"MIGRATION_DIRECTORY": "migrations", "UPLOAD_PATH": "uploads", "PASSWORD_PEPPER": "pepper",
		"JWT_SECRET": "jwt-secret", "JWT_ACCESS_TOKEN_TTL": "15m",
		"REFRESH_TOKEN_SECRET": "refresh-secret", "REFRESH_TOKEN_TTL": "24h",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}

	t.Setenv("LOG_AS_JSON", "invalid")
	if err := Load(envFile); err == nil {
		t.Fatal("expected logger config error")
	}

	t.Setenv("LOG_AS_JSON", "false")
	t.Setenv("WORKER_POLL_INTERVAL", "invalid")
	if err := Load(envFile); err == nil {
		t.Fatal("expected worker config error")
	}
}
