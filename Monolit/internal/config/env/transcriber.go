package env

import "github.com/caarlos0/env/v11"

type transcriberEnvConfig struct {
	Provider         string `env:"TRANSCRIBER_PROVIDER" envDefault:"assemblyai"`
	AssemblyAIAPIKey string `env:"ASSEMBLYAI_API_KEY"`
}

type transcriberConfig struct {
	raw transcriberEnvConfig
}

func NewTranscriberConfig() (*transcriberConfig, error) {
	var raw transcriberEnvConfig
	if err := env.Parse(&raw); err != nil {
		return nil, err
	}
	return &transcriberConfig{raw: raw}, nil
}

func (cfg *transcriberConfig) Provider() string {
	return cfg.raw.Provider
}

func (cfg *transcriberConfig) AssemblyAIAPIKey() string {
	return cfg.raw.AssemblyAIAPIKey
}
