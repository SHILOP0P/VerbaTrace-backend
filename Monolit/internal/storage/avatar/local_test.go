package avatar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestSaveAndDeleteAvatar(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	storage := NewLocalStorage(t.TempDir())

	saved, err := storage.Save(ctx, models.SaveUserAvatarInput{
		UserUUID:         userID,
		OriginalFilename: "avatar.png",
		MimeType:         "image/png",
		Content:          strings.NewReader("png"),
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(storage.baseDir, saved.Path)); err != nil {
		t.Fatalf("saved file stat: %v", err)
	}

	if err := storage.Delete(ctx, saved.Path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestSaveRejectsInvalidAvatar(t *testing.T) {
	storage := NewLocalStorage(t.TempDir())

	_, err := storage.Save(context.Background(), models.SaveUserAvatarInput{
		UserUUID:         uuid.New(),
		OriginalFilename: "avatar.txt",
		MimeType:         "text/plain",
		Content:          strings.NewReader("txt"),
	})
	if !errors.Is(err, models.ErrInvalidUserInput) {
		t.Fatalf("Save error = %v, want ErrInvalidUserInput", err)
	}
}
