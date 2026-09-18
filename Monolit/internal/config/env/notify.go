package env

import "github.com/caarlos0/env/v11"

type notifyEnvConfig struct {
	// Sender delivers mail and Telegram messages. Only "mock" exists: it logs
	// the message and counts it as sent.
	Sender string `env:"NOTIFY_SENDER" envDefault:"mock"`
	// PublicAppURL is where links in messages point.
	PublicAppURL string `env:"PUBLIC_APP_URL" envDefault:"http://127.0.0.1:5173"`
}

type notifyConfig struct {
	raw notifyEnvConfig
}

func NewNotifyConfig() (*notifyConfig, error) {
	var raw notifyEnvConfig
	if err := env.Parse(&raw); err != nil {
		return nil, err
	}
	return &notifyConfig{raw: raw}, nil
}

func (cfg *notifyConfig) Sender() string { return cfg.raw.Sender }

func (cfg *notifyConfig) PublicAppURL() string { return cfg.raw.PublicAppURL }
