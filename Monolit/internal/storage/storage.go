package storage

import (
	"context"
	"io"

	"verbatrace/monolit/internal/models"
)

type AudioStorage interface {
	Save(ctx context.Context, input models.SaveInput) (models.SavedFile, error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	OpenReadSeeker(ctx context.Context, path string) (models.ReadSeekCloser, error)
	Delete(ctx context.Context, path string) error
}

// ASRCacheStorage is an optional capability of audio storage. It keeps the
// original media untouched while maintaining a compact, normalized file for
// speech-to-text providers.
type ASRCacheStorage interface {
	EnsureASRCache(ctx context.Context, sourcePath string, cachePath string) (reused bool, err error)
}

type InstructionStorage interface {
	Save(ctx context.Context, input models.SaveInstructionInput) (models.SavedInstructionFile, error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
}

type ReportStorage interface {
	Save(ctx context.Context, input models.SaveReportInput) (models.SavedReportFile, error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
}

type AvatarStorage interface {
	Save(ctx context.Context, input models.SaveUserAvatarInput) (models.SavedUserAvatar, error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
}
