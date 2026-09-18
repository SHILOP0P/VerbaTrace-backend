package analyzer

import (
	"context"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"
)

type testConfig struct{ provider, apiKey, model string }

func (c testConfig) Provider() string { return c.provider }
func (c testConfig) APIKey() string   { return c.apiKey }
func (c testConfig) Model() string    { return c.model }

func TestNewFromConfig(t *testing.T) {
	for _, provider := range []string{"", "mock", " MOCK "} {
		got, err := NewFromConfig(testConfig{provider: provider, model: "model"})
		if err != nil || got.Provider() != "mock" {
			t.Fatalf("provider %q: analyzer=%v err=%v", provider, got, err)
		}
	}

	staged, err := NewFromConfig(testConfig{provider: " mock_staged "})
	if err != nil || staged.Provider() != "mock_staged" {
		t.Fatalf("mock_staged: analyzer=%v err=%v", staged, err)
	}
	if _, progressive := staged.(interface{ AnalysisSchema() map[string]any }); !progressive {
		t.Fatal("mock_staged must run the staged pipeline")
	}

	if _, err := NewFromConfig(testConfig{provider: "openai"}); err == nil {
		t.Fatal("expected not implemented error")
	}
	if _, err := NewFromConfig(testConfig{provider: "unknown"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if _, err := NewFromConfig(testConfig{provider: "openrouter"}); err == nil {
		t.Fatal("expected invalid OpenRouter config error")
	}
}

func TestMockAnalyzer(t *testing.T) {
	got, err := NewFromConfig(testConfig{provider: "mock", model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := got.Analyze(context.Background(), models.AnalysisRequest{
		Transcription: "hello",
		Instructions:  []models.AnalysisInstructionContent{{Content: "one"}},
	})
	if err != nil || result.Model == nil || *result.Model != "test-model" || !strings.Contains(string(result.ResultJSON), "instruction_count") {
		t.Fatalf("unexpected result: %+v err=%v", result, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := got.Analyze(ctx, models.AnalysisRequest{}); err == nil {
		t.Fatal("expected canceled context error")
	}
}
