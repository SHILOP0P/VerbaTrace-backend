package audio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

func TestLocalStorageLifecycle(t *testing.T) {
	baseDir := t.TempDir()
	storage := NewLocalStorage(baseDir)
	callID := uuid.New()

	saved, err := storage.Save(context.Background(), models.SaveInput{
		CallID:           callID,
		OriginalFilename: "CALL.WAV",
		MimeType:         "audio/wav",
		Content:          strings.NewReader("audio"),
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.Path != callID.String()+".wav" || saved.SizeBytes != 5 {
		t.Fatalf("saved file = %+v", saved)
	}

	content, err := storage.Open(context.Background(), saved.Path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, _ := io.ReadAll(content)
	_ = content.Close()
	if string(data) != "audio" {
		t.Fatalf("content = %q", data)
	}
	windowsStylePath := strings.ReplaceAll(saved.Path, string(filepath.Separator), "\\")
	content, err = storage.Open(context.Background(), windowsStylePath)
	if err != nil {
		t.Fatalf("Open windows-style path: %v", err)
	}
	_ = content.Close()

	seekable, err := storage.OpenReadSeeker(context.Background(), saved.Path)
	if err != nil {
		t.Fatalf("OpenReadSeeker: %v", err)
	}
	if _, err := seekable.Seek(1, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	data, _ = io.ReadAll(seekable)
	_ = seekable.Close()
	if string(data) != "udio" {
		t.Fatalf("seekable content = %q", data)
	}

	if err := storage.Delete(context.Background(), saved.Path); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := storage.Delete(context.Background(), saved.Path); err != nil {
		t.Fatalf("Delete missing file: %v", err)
	}
	if _, err := storage.Open(context.Background(), saved.Path); !errors.Is(err, models.ErrAudioFileNotFound) {
		t.Fatalf("Open missing error = %v", err)
	}
}

func TestLocalStorageValidationAndCancellation(t *testing.T) {
	storage := NewLocalStorage(t.TempDir())
	if _, err := storage.Save(context.Background(), models.SaveInput{}); err == nil {
		t.Fatal("expected empty content error")
	}
	if _, err := storage.Save(context.Background(), models.SaveInput{Content: bytes.NewReader(nil), OriginalFilename: "audio"}); err == nil {
		t.Fatal("expected missing extension error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := storage.Save(ctx, models.SaveInput{CallID: uuid.New(), Content: strings.NewReader("x"), OriginalFilename: "x.mp3"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save canceled error = %v", err)
	}
	if _, err := storage.Open(ctx, "x.mp3"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open canceled error = %v", err)
	}
	if _, err := storage.OpenReadSeeker(ctx, "x.mp3"); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenReadSeeker canceled error = %v", err)
	}
	if err := storage.Delete(ctx, "x.mp3"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete canceled error = %v", err)
	}

	for _, path := range []string{".", "..", filepath.Join("..", "escape.mp3"), filepath.Join(t.TempDir(), "absolute.mp3")} {
		if _, err := storage.safePath(path); !errors.Is(err, models.ErrInvalidAudioPath) {
			t.Fatalf("safePath(%q) error = %v", path, err)
		}
	}
}

func TestEnsureASRCacheCreatesReusesAndDeletesCache(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}

	storage := NewLocalStorage(t.TempDir())
	callID := uuid.New()
	sourcePath := callID.String() + ".wav"
	cachePath := "asr/" + callID.String() + ".ogg"

	command := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.1", "-ar", "16000", "-c:a", "pcm_s16le", storage.baseDir+string(filepath.Separator)+sourcePath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create WAV fixture: %v: %s", err, output)
	}

	reused, err := storage.EnsureASRCache(context.Background(), sourcePath, cachePath)
	if err != nil || reused {
		t.Fatalf("first EnsureASRCache: reused=%t err=%v", reused, err)
	}
	cache, err := storage.Open(context.Background(), cachePath)
	if err != nil {
		t.Fatalf("open generated cache: %v", err)
	}
	data, _ := io.ReadAll(cache)
	_ = cache.Close()
	if len(data) == 0 || string(data[:4]) != "OggS" {
		t.Fatalf("generated cache is not OGG: %q", data[:min(len(data), 4)])
	}

	reused, err = storage.EnsureASRCache(context.Background(), sourcePath, cachePath)
	if err != nil || !reused {
		t.Fatalf("second EnsureASRCache: reused=%t err=%v", reused, err)
	}
	if err = storage.Delete(context.Background(), cachePath); err != nil {
		t.Fatalf("delete ASR cache: %v", err)
	}
	if _, err = storage.Open(context.Background(), cachePath); !errors.Is(err, models.ErrAudioFileNotFound) {
		t.Fatalf("cache still exists after deletion: %v", err)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
