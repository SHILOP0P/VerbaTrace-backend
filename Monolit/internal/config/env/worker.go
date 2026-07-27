package env

import (
	"time"

	"github.com/caarlos0/env/v11"
)

type workerEnvConfig struct {
	Enabled      bool          `env:"WORKER_ENABLED" envDefault:"true"`
	PollInterval time.Duration `env:"WORKER_POLL_INTERVAL" envDefault:"2s"`
	Limit        int           `env:"WORKER_LIMIT" envDefault:"1"`
	RetryDelay   time.Duration `env:"WORKER_RETRY_DELAY" envDefault:"1m"`
	StaleAfter   time.Duration `env:"WORKER_STALE_AFTER" envDefault:"30m"`
	MaxAttempts  int           `env:"WORKER_MAX_ATTEMPTS" envDefault:"5"`
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
