package env

import (
	"strings"
	"testing"
	"time"
)

func TestConfigsFromEnvironment(t *testing.T) {
	values := map[string]string{
		"ANALYZER_PROVIDER":      "openrouter",
		"ANALYZER_API_KEY":       "analyzer-key",
		"ANALYZER_MODEL":         "analyzer-model",
		"UPLOAD_PATH":            "uploads",
		"FFMPEG_PATH":            "custom-ffmpeg",
		"FFPROBE_PATH":           "custom-ffprobe",
		"PASSWORD_PEPPER":        "pepper",
		"JWT_SECRET":             "jwt-secret",
		"JWT_ACCESS_TOKEN_TTL":   "15m",
		"REFRESH_TOKEN_SECRET":   "refresh-secret",
		"REFRESH_TOKEN_TTL":      "24h",
		"AUTH_SESSION_TRUST_AGE": "12h",
		"HTTP_HOST":              "127.0.0.1",
		"HTTP_PORT":              "8080",
		"HTTP_READ_TIMEOUT":      "5s",
		"LOG_LEVEL":              "debug",
		"LOG_AS_JSON":            "true",
		"POSTGRES_HOST":          "localhost",
		"POSTGRES_PORT":          "5432",
		"POSTGRES_DB":            "verbatrace",
		"POSTGRES_USER":          "postgres",
		"POSTGRES_PASSWORD":      "password",
		"POSTGRES_SSL_MODE":      "disable",
		"MIGRATION_DIRECTORY":    "migrations",
		"TRANSCRIBER_PROVIDER":   "mock",
		"ASSEMBLYAI_API_KEY":     "assemblyai-key",
		"WORKER_ENABLED":         "false",
		"WORKER_POLL_INTERVAL":   "3s",
		"WORKER_LIMIT":           "7",
		"WORKER_RETRY_DELAY":     "2m",
		"WORKER_STALE_AFTER":     "20m",
		"WORKER_MAX_ATTEMPTS":    "5",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}

	analyzer, err := NewAnalyzerConfig()
	if err != nil || analyzer.Provider() != "openrouter" || analyzer.APIKey() != "analyzer-key" || analyzer.Model() != "analyzer-model" {
		t.Fatalf("analyzer config: %+v err=%v", analyzer, err)
	}
	upload, err := NewUploadConfig()
	if err != nil || upload.Path() != "uploads" || upload.FFmpegPath() != "custom-ffmpeg" || upload.FFProbePath() != "custom-ffprobe" {
		t.Fatalf("upload config: %+v err=%v", upload, err)
	}
	auth, err := NewAuthConfig()
	if err != nil || auth.PasswordPepper() != "pepper" || auth.JWTSecret() != "jwt-secret" ||
		auth.AccessTokenTTL() != 15*time.Minute || auth.RefreshTokenSecret() != "refresh-secret" || auth.RefreshTokenTTL() != 24*time.Hour || auth.SessionTrustAge() != 12*time.Hour {
		t.Fatalf("auth config: %+v err=%v", auth, err)
	}
	httpCfg, err := NewHTTPConfig()
	if err != nil || httpCfg.Address() != "127.0.0.1:8080" || httpCfg.ReadTimeout() != 5*time.Second {
		t.Fatalf("http config: %+v err=%v", httpCfg, err)
	}
	logger, err := NewLoggerConfig()
	if err != nil || logger.Level() != "debug" || !logger.AsJSON() {
		t.Fatalf("logger config: %+v err=%v", logger, err)
	}
	postgres, err := NewPostgresConfig()
	if err != nil || postgres.DatabaseName() != "verbatrace" || postgres.MigrationDir() != "migrations" ||
		!strings.Contains(postgres.URI(), "postgres://postgres:password@localhost:5432/verbatrace?sslmode=disable") {
		t.Fatalf("postgres config: %+v err=%v", postgres, err)
	}
	transcriber, err := NewTranscriberConfig()
	if err != nil || transcriber.Provider() != "mock" || transcriber.AssemblyAIAPIKey() != "assemblyai-key" {
		t.Fatalf("transcriber config: %+v err=%v", transcriber, err)
	}
	worker, err := NewWorkerConfig()
	if err != nil || worker.Enabled() || worker.PollInterval() != 3*time.Second || worker.Limit() != 7 ||
		worker.RetryDelay() != 2*time.Minute || worker.StaleAfter() != 20*time.Minute || worker.MaxAttempts() != 5 {
		t.Fatalf("worker config: %+v err=%v", worker, err)
	}
}

func TestConfigValidationErrors(t *testing.T) {
	t.Setenv("HTTP_HOST", "localhost")
	t.Setenv("HTTP_PORT", "8080")
	t.Setenv("HTTP_READ_TIMEOUT", "invalid")
	if _, err := NewHTTPConfig(); err == nil {
		t.Fatal("expected HTTP config validation error")
	}

	t.Setenv("PASSWORD_PEPPER", "pepper")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("JWT_ACCESS_TOKEN_TTL", "invalid")
	t.Setenv("REFRESH_TOKEN_SECRET", "refresh")
	t.Setenv("REFRESH_TOKEN_TTL", "24h")
	if _, err := NewAuthConfig(); err == nil {
		t.Fatal("expected auth config validation error")
	}

	t.Setenv("WORKER_POLL_INTERVAL", "invalid")
	if _, err := NewWorkerConfig(); err == nil {
		t.Fatal("expected worker config validation error")
	}
}

func TestAuthConfigDecodesBase64Secrets(t *testing.T) {
	t.Setenv("PASSWORD_PEPPER", "base64:cGVwcGVy")
	t.Setenv("JWT_SECRET", "base64:and0LXNlY3JldA==")
	t.Setenv("JWT_ACCESS_TOKEN_TTL", "15m")
	t.Setenv("REFRESH_TOKEN_SECRET", "base64:cmVmcmVzaC1zZWNyZXQ=")
	t.Setenv("REFRESH_TOKEN_TTL", "24h")

	auth, err := NewAuthConfig()
	if err != nil {
		t.Fatalf("NewAuthConfig: %v", err)
	}
	if auth.PasswordPepper() != "pepper" {
		t.Fatalf("PasswordPepper = %q", auth.PasswordPepper())
	}
	if auth.JWTSecret() != "jwt-secret" {
		t.Fatalf("JWTSecret = %q", auth.JWTSecret())
	}
	if auth.RefreshTokenSecret() != "refresh-secret" {
		t.Fatalf("RefreshTokenSecret = %q", auth.RefreshTokenSecret())
	}
}

func TestAuthConfigRejectsInvalidBase64Secret(t *testing.T) {
	t.Setenv("PASSWORD_PEPPER", "base64:not-valid")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("JWT_ACCESS_TOKEN_TTL", "15m")
	t.Setenv("REFRESH_TOKEN_SECRET", "refresh")
	t.Setenv("REFRESH_TOKEN_TTL", "24h")

	if _, err := NewAuthConfig(); err == nil {
		t.Fatal("NewAuthConfig() error = nil, want invalid base64 error")
	}
}
