package config

import "time"

type HTTPConfig interface {
	Address() string
	ReadTimeout() time.Duration
}

type PostgresConfig interface {
	URI() string
	DatabaseName() string
	MigrationDir() string
}

type UploadConfig interface {
	Path() string
	FFmpegPath() string
	FFProbePath() string
}

type LoggerConfig interface {
	Level() string
	AsJSON() bool
}

type AuthConfig interface {
	PasswordPepper() string
	JWTSecret() string
	AccessTokenTTL() time.Duration
	RefreshTokenSecret() string
	RefreshTokenTTL() time.Duration
	SessionTrustAge() time.Duration
}

type WorkerConfig interface {
	Enabled() bool
	PollInterval() time.Duration
	Limit() int
	RetryDelay() time.Duration
	StaleAfter() time.Duration
	MaxAttempts() int
}

type TranscriberConfig interface {
	Provider() string
	AssemblyAIAPIKey() string
}

type AnalyzerConfig interface {
	Provider() string
	APIKey() string
	Model() string
}
