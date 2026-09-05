package transcriber

import (
	"context"

	"verbatrace/monolit/internal/models"
)

type Transcriber interface {
	Provider() string
	Transcribe(ctx context.Context, file models.File) (models.TranscriptionResult, error)
}

// PrivacyAware is optional so existing providers and deterministic test doubles
// remain compatible. A required privacy policy must never fall back to Transcribe.
type PrivacyAware interface {
	TranscribeRequest(ctx context.Context, request models.TranscriptionRequest) (models.TranscriptionResult, error)
}

// ArtifactCleaner removes a provider-side transcript after VerbaTrace has
// atomically persisted the redacted result. Implementations must be idempotent.
type ArtifactCleaner interface {
	DeleteArtifact(ctx context.Context, providerJobID string) error
}
