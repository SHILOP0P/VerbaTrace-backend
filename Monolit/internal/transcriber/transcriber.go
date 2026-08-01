package transcriber

import (
	"context"

	"verbatrace/monolit/internal/models"
)

type Transcriber interface {
	Provider() string
	Transcribe(ctx context.Context, file models.File) (models.TranscriptionResult, error)
}
