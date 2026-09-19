package config

import "time"

type HTTPConfig interface {
	Address() string
	ReadTimeout() time.Duration
	// IsTrustedProxy says whether an address belongs to the deployment's own
	// front layer and may therefore speak for the real client.
	IsTrustedProxy(address string) bool
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
	CallRetentionInterval() time.Duration
	CallRetentionBatch() int
	InstructionRetentionInterval() time.Duration
	InstructionRetentionBatch() int
}

type TranscriberConfig interface {
	Provider() string
	AssemblyAIAPIKey() string
}

type NotifyConfig interface {
	Sender() string
	PublicAppURL() string
}

type AnalyzerConfig interface {
	Provider() string
	APIKey() string
	Model() string
}
