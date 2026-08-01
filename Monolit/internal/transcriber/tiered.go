package transcriber

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/transcriber/assemblyai"
)

type tieredTranscriber struct {
	standard   Transcriber
	diarized   Transcriber
	identified Transcriber
}

func newTieredTranscriber(assemblyAIAPIKey string) (Transcriber, error) {
	standard, err := assemblyai.New(assemblyAIAPIKey, false, false)
	if err != nil {
		return nil, err
	}
	diarized, err := assemblyai.New(assemblyAIAPIKey, true, false)
	if err != nil {
		return nil, err
	}
	identified, err := assemblyai.New(assemblyAIAPIKey, true, true)
	if err != nil {
		return nil, err
	}
	return &tieredTranscriber{standard: standard, diarized: diarized, identified: identified}, nil
}

func (t *tieredTranscriber) Provider() string { return t.standard.Provider() }

func (t *tieredTranscriber) Transcribe(ctx context.Context, file models.File) (models.TranscriptionResult, error) {
	return t.standard.Transcribe(ctx, file)
}

func (t *tieredTranscriber) ProviderForMode(mode models.TranscriptionMode) string {
	if mode == models.TranscriptionModeDiarized {
		return t.diarized.Provider()
	}
	if mode == models.TranscriptionModeIdentified {
		return t.identified.Provider()
	}
	return t.standard.Provider()
}

func (t *tieredTranscriber) TranscribeForMode(ctx context.Context, file models.File, mode models.TranscriptionMode) (models.TranscriptionResult, error) {
	switch mode {
	case "", models.TranscriptionModeStandard:
		return t.standard.Transcribe(ctx, file)
	case models.TranscriptionModeDiarized:
		return t.diarized.Transcribe(ctx, file)
	case models.TranscriptionModeIdentified:
		return t.identified.Transcribe(ctx, file)
	default:
		return models.TranscriptionResult{}, fmt.Errorf("unsupported transcription mode: %s", mode)
	}
}
