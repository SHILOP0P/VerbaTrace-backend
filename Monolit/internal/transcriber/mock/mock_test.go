package mock

import (
	"context"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"
)

func TestTranscriber(t *testing.T) {
	transcriber := New()
	if transcriber.Provider() != "mock" {
		t.Fatalf("provider = %q", transcriber.Provider())
	}
	result, err := transcriber.Transcribe(context.Background(), models.File{OriginalFilename: "call.wav"})
	if err != nil || !strings.Contains(result.Text, "call.wav") || result.Language == nil || *result.Language != "ru" {
		t.Fatalf("result = %+v, err=%v", result, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := transcriber.Transcribe(ctx, models.File{}); err == nil {
		t.Fatal("expected canceled context error")
	}
}
