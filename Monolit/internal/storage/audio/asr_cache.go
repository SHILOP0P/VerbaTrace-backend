package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnsureASRCache creates a mono 16 kHz Opus OGG cache if it is absent. A
// non-empty existing cache is reused, so retries never invoke ffmpeg again.
func (l *LocalStorage) EnsureASRCache(ctx context.Context, sourcePath string, cachePath string) (bool, error) {
	source, err := l.safePath(sourcePath)
	if err != nil {
		return false, err
	}
	cache, err := l.safePath(cachePath)
	if err != nil {
		return false, err
	}

	if info, statErr := os.Stat(cache); statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return true, nil
	} else if statErr == nil {
		if err = os.Remove(cache); err != nil {
			return false, fmt.Errorf("remove invalid ASR cache: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, fmt.Errorf("stat ASR cache: %w", statErr)
	}

	if err = os.MkdirAll(filepath.Dir(cache), 0755); err != nil {
		return false, fmt.Errorf("create ASR cache directory: %w", err)
	}

	ffmpegPath := strings.TrimSpace(os.Getenv("FFMPEG_PATH"))
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	temporary := cache + ".partial"
	defer func() { _ = os.Remove(temporary) }()

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-i", source,
		"-vn", "-ac", "1", "-ar", "16000",
		"-c:a", "libopus", "-b:a", "24k", "-vbr", "on",
		"-f", "ogg", "-y", temporary,
	)
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return false, fmt.Errorf("build ASR cache: %s", message)
	}

	info, err := os.Stat(temporary)
	if err != nil || info.Size() == 0 {
		if err != nil {
			return false, fmt.Errorf("stat generated ASR cache: %w", err)
		}
		return false, errors.New("generated ASR cache is empty")
	}
	if err = os.Rename(temporary, cache); err != nil {
		return false, fmt.Errorf("publish ASR cache: %w", err)
	}
	return false, nil
}
