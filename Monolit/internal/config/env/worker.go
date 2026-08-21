package env

import (
	"time"

	"github.com/caarlos0/env/v11"
)

type workerEnvConfig struct {
	Enabled                      bool          `env:"WORKER_ENABLED" envDefault:"true"`
	PollInterval                 time.Duration `env:"WORKER_POLL_INTERVAL" envDefault:"2s"`
	Limit                        int           `env:"WORKER_LIMIT" envDefault:"1"`
	RetryDelay                   time.Duration `env:"WORKER_RETRY_DELAY" envDefault:"1m"`
	StaleAfter                   time.Duration `env:"WORKER_STALE_AFTER" envDefault:"30m"`
	MaxAttempts                  int           `env:"WORKER_MAX_ATTEMPTS" envDefault:"5"`
	CallRetentionInterval        time.Duration `env:"CALL_RETENTION_INTERVAL" envDefault:"24h"`
	CallRetentionBatch           int           `env:"CALL_RETENTION_BATCH" envDefault:"100"`
	InstructionRetentionInterval time.Duration `env:"INSTRUCTION_RETENTION_INTERVAL" envDefault:"25h"`
	InstructionRetentionBatch    int           `env:"INSTRUCTION_RETENTION_BATCH" envDefault:"50"`
}

type workerConfig struct {
	raw workerEnvConfig
}

func NewWorkerConfig() (*workerConfig, error) {
	var raw workerEnvConfig
	if err := env.Parse(&raw); err != nil {
		return nil, err
	}
	return &workerConfig{raw: raw}, nil
}

func (cfg *workerConfig) Enabled() bool {
	return cfg.raw.Enabled
}

func (cfg *workerConfig) PollInterval() time.Duration {
	return cfg.raw.PollInterval
}

func (cfg *workerConfig) Limit() int {
	return cfg.raw.Limit
}

func (cfg *workerConfig) RetryDelay() time.Duration {
	return cfg.raw.RetryDelay
}

func (cfg *workerConfig) StaleAfter() time.Duration {
	return cfg.raw.StaleAfter
}

func (cfg *workerConfig) MaxAttempts() int {
	return cfg.raw.MaxAttempts
}

func (cfg *workerConfig) CallRetentionInterval() time.Duration { return cfg.raw.CallRetentionInterval }
func (cfg *workerConfig) CallRetentionBatch() int              { return cfg.raw.CallRetentionBatch }
func (cfg *workerConfig) InstructionRetentionInterval() time.Duration {
	return cfg.raw.InstructionRetentionInterval
}
func (cfg *workerConfig) InstructionRetentionBatch() int { return cfg.raw.InstructionRetentionBatch }
