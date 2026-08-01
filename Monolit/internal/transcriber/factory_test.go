package transcriber

import (
	"context"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"
)

type testConfig struct{ provider, assemblyAIAPIKey string }

func (c testConfig) Provider() string         { return c.provider }
func (c testConfig) AssemblyAIAPIKey() string { return c.assemblyAIAPIKey }

func TestNewFromConfigAndMockTranscriber(t *testing.T) {
	for _, provider := range []string{"", "mock", " MOCK "} {
		got, err := NewFromConfig(testConfig{provider: provider})
		if err != nil || got.Provider() != "mock" {
			t.Fatalf("provider %q: transcriber=%v err=%v", provider, got, err)
		}
		result, err := got.Transcribe(context.Background(), models.File{OriginalFilename: "call.wav"})
		if err != nil || !strings.Contains(result.Text, "call.wav") {
			t.Fatalf("unexpected result: %+v err=%v", result, err)
		}
	}

	if _, err := NewFromConfig(testConfig{provider: "openai"}); err == nil {
		t.Fatal("expected not implemented error")
	}
	if _, err := NewFromConfig(testConfig{provider: "unknown"}); err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if _, err := NewFromConfig(testConfig{provider: "assemblyai"}); err == nil {
		t.Fatal("expected invalid AssemblyAI config error")
	}

	got, _ := NewFromConfig(testConfig{provider: "mock"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := got.Transcribe(ctx, models.File{}); err == nil {
		t.Fatal("expected canceled context error")
	}
	tiered, err := NewFromConfig(testConfig{provider: "assemblyai", assemblyAIAPIKey: "assembly-key"})
	if err != nil || tiered.Provider() != "assemblyai" {
		t.Fatalf("tiered provider = %T, %v", tiered, err)
	}
}
