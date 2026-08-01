package mock

import (
	"context"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"
)

func TestAnalyzer(t *testing.T) {
	analyzer := New("model")
	if analyzer.Provider() != "mock" {
		t.Fatalf("provider = %q", analyzer.Provider())
	}
	result, err := analyzer.Analyze(context.Background(), models.AnalysisRequest{
		Transcription: "hello",
		Instructions:  []models.AnalysisInstructionContent{{Content: "guide"}},
	})
	if err != nil || result.Model == nil || *result.Model != "model" || !strings.Contains(string(result.ResultJSON), "transcription_size") {
		t.Fatalf("result = %+v, err=%v", result, err)
	}

	withoutModel, _ := New("").Analyze(context.Background(), models.AnalysisRequest{})
	if withoutModel.Model != nil {
		t.Fatalf("model = %v", withoutModel.Model)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := analyzer.Analyze(ctx, models.AnalysisRequest{}); err == nil {
		t.Fatal("expected canceled context error")
	}
}
