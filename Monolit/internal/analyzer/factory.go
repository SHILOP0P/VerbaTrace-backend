package analyzer

import (
	"fmt"
	"strings"

	mockAnalyzer "verbatrace/monolit/internal/analyzer/mock"
	"verbatrace/monolit/internal/analyzer/mockstaged"
	openrouterAnalyzer "verbatrace/monolit/internal/analyzer/openrouter"
)

type Config interface {
	Provider() string
	APIKey() string
	Model() string
}

func NewFromConfig(cfg Config) (Analyzer, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider()))

	switch provider {
	case "", "mock":
		return mockAnalyzer.New(cfg.Model()), nil
	case mockstaged.ProviderName:
		return mockstaged.New(cfg.Model()), nil
	case "openrouter":
		return openrouterAnalyzer.New(cfg.APIKey(), cfg.Model())
	case "openai":
		return nil, fmt.Errorf("openai analyzer is not implemented yet")
	default:
		return nil, fmt.Errorf("unsupported analyzer provider: %s", cfg.Provider())
	}
}
