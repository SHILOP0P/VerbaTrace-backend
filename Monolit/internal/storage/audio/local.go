package audio

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"verbatrace/monolit/internal/models"
)

func (l *LocalStorage) Save(ctx context.Context, input models.SaveInput) (models.SavedFile, error) {
	if input.Content == nil {
		return models.SavedFile{}, fmt.Errorf("audio content is empty")
	}

	ext := strings.ToLower(filepath.Ext(input.OriginalFilename))
	if ext == "" {
		return models.SavedFile{}, fmt.Errorf("audio extention is empty")
	}

	if err := os.MkdirAll(l.baseDir, 0755); err != nil {
		return models.SavedFile{}, fmt.Errorf("creating audio directory failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return models.SavedFile{}, ctx.Err()
	default:
	}

	filename := fmt.Sprintf("%s%s", input.CallID.String(), ext)
	fullPath := filepath.Join(l.baseDir, filename)

	dst, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return models.SavedFile{}, fmt.Errorf("create audio file failed: %w", err)
	}
	defer func() { _ = dst.Close() }()

	syzeBytes, err := io.Copy(dst, input.Content)
	if err != nil {
		return models.SavedFile{}, fmt.Errorf("save audio file failed: %w", err)
	}

	return models.SavedFile{
		Path:             filename,
		OriginalFilename: input.OriginalFilename,
		MimeType:         input.MimeType,
		SizeBytes:        syzeBytes,
	}, nil
}

func (l *LocalStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	return l.openFile(ctx, path)
}

func (l *LocalStorage) OpenReadSeeker(ctx context.Context, path string) (models.ReadSeekCloser, error) {
	return l.openFile(ctx, path)
}

func (l *LocalStorage) openFile(ctx context.Context, path string) (*os.File, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	fullPath, err := l.safePath(path)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %w", models.ErrAudioFileNotFound, err)
		}

		return nil, fmt.Errorf("open audio file failed: %w", err)
	}

	return file, nil
}

func (l *LocalStorage) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath, err := l.safePath(path)
	if err != nil {
		return err
	}

	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete audio file failed: %w", err)
	}
	return nil
}

func (l *LocalStorage) safePath(path string) (string, error) {
	return safeLocalPath(l.baseDir, path)
}

func safeLocalPath(baseDir string, path string) (string, error) {
	normalizedPath := filepath.FromSlash(strings.ReplaceAll(path, "\\", "/"))
	cleanPath := filepath.Clean(normalizedPath)

	if cleanPath == "." || cleanPath == ".." || filepath.IsAbs(cleanPath) {
		return "", models.ErrInvalidAudioPath
	}

	if strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) {
		return "", models.ErrInvalidAudioPath
	}

	return filepath.Join(baseDir, cleanPath), nil
}
