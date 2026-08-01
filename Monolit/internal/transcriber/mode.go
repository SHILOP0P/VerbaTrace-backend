package transcriber

import (
	"context"

	"verbatrace/monolit/internal/models"
)

type ModeAware interface {
	ProviderForMode(mode models.TranscriptionMode) string
	TranscribeForMode(ctx context.Context, file models.File, mode models.TranscriptionMode) (models.TranscriptionResult, error)
}
