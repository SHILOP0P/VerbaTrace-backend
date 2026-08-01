package avatar

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

var allowedExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".webp": {},
}

func (l *LocalStorage) Save(ctx context.Context, input models.SaveUserAvatarInput) (models.SavedUserAvatar, error) {
	if input.UserUUID == uuid.Nil || input.Content == nil {
		return models.SavedUserAvatar{}, models.ErrInvalidUserInput
	}
	if !strings.HasPrefix(strings.ToLower(input.MimeType), "image/") {
		return models.SavedUserAvatar{}, models.ErrInvalidUserInput
	}

	ext := strings.ToLower(filepath.Ext(input.OriginalFilename))
	if _, ok := allowedExtensions[ext]; !ok {
		return models.SavedUserAvatar{}, models.ErrInvalidUserInput
	}

	if err := os.MkdirAll(l.baseDir, 0755); err != nil {
		return models.SavedUserAvatar{}, fmt.Errorf("creating avatar directory failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return models.SavedUserAvatar{}, ctx.Err()
	default:
	}

	filename := input.UserUUID.String() + ext
	fullPath := filepath.Join(l.baseDir, filename)

	dst, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return models.SavedUserAvatar{}, fmt.Errorf("create avatar file failed: %w", err)
	}
	defer func() { _ = dst.Close() }()

	sizeBytes, err := io.Copy(dst, input.Content)
	if err != nil {
		return models.SavedUserAvatar{}, fmt.Errorf("save avatar file failed: %w", err)
	}
	if sizeBytes == 0 {
		_ = os.Remove(fullPath)
		return models.SavedUserAvatar{}, models.ErrInvalidUserInput
	}

	return models.SavedUserAvatar{Path: filename, MimeType: input.MimeType, SizeBytes: sizeBytes}, nil
}

func (l *LocalStorage) Delete(ctx context.Context, path string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	fullPath, err := safeLocalPath(l.baseDir, path)
	if err != nil {
		return err
	}

	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete avatar file failed: %w", err)
	}
	return nil
}

func (l *LocalStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	fullPath, err := safeLocalPath(l.baseDir, path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(fullPath)
	if os.IsNotExist(err) {
		return nil, models.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open avatar file failed: %w", err)
	}
	return file, nil
}

func safeLocalPath(baseDir string, path string) (string, error) {
	cleanPath := filepath.Clean(path)
	if cleanPath == "." || cleanPath == ".." || filepath.IsAbs(cleanPath) {
		return "", models.ErrInvalidUserInput
	}
	if strings.HasPrefix(cleanPath, ".."+string(os.PathSeparator)) {
		return "", models.ErrInvalidUserInput
	}
	return filepath.Join(baseDir, cleanPath), nil
}
