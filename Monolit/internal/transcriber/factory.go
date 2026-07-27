package transcriber

import (
	"fmt"
	"strings"

	mockTranscriber "calllens/monolit/internal/transcriber/mock"
)

type Config interface {
	Provider() string
	AssemblyAIAPIKey() string
}

func NewFromConfig(cfg Config) (Transcriber, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider()))

	switch provider {
	case "", "mock":
		return mockTranscriber.New(), nil
	case "assemblyai":
		return newTieredTranscriber(cfg.AssemblyAIAPIKey())
	case "openai":
		return nil, fmt.Errorf("openai transcriber is not implemented yet")
	default:
		return nil, fmt.Errorf("unsupported transcriber provider: %s", cfg.Provider())
	}
}
