package audio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"verbatrace/monolit/internal/models"
)

type FFProbeDurationDetector struct {
	baseDir     string
	ffprobePath string
}

func NewFFProbeDurationDetector(baseDir string, ffprobePath string) *FFProbeDurationDetector {
	if strings.TrimSpace(ffprobePath) == "" {
		ffprobePath = "ffprobe"
	}

	return &FFProbeDurationDetector{
		baseDir:     baseDir,
		ffprobePath: ffprobePath,
	}
}

func (d *FFProbeDurationDetector) DetectDuration(ctx context.Context, path string) (int, error) {
	fullPath, err := safeLocalPath(d.baseDir, path)
	if err != nil {
		return 0, err
	}

	file, err := os.Open(fullPath)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", models.ErrAudioFileUnreadable, err)
	}
	_ = file.Close()

	output, err := exec.CommandContext(
		ctx,
		d.ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1",
		fullPath,
	).CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return 0, fmt.Errorf("%w: %w", models.ErrAudioProbeNotFound, err)
		}

		message := strings.TrimSpace(string(output))
		if message == "" {
			return 0, fmt.Errorf("%w: %w", models.ErrAudioDurationDetect, err)
		}
		return 0, fmt.Errorf("%w: %w: %s", models.ErrAudioDurationDetect, err, message)
	}

	return parseFFProbeDuration(output)
}

func parseFFProbeDuration(output []byte) (int, error) {
	value := strings.TrimSpace(string(output))
	if value == "" {
		return 0, fmt.Errorf("%w: empty ffprobe output", models.ErrAudioDurationDetect)
	}

	duration, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: parse ffprobe output %q: %w", models.ErrAudioDurationDetect, value, err)
	}

	if duration <= 0 {
		return 0, fmt.Errorf("%w: non-positive duration %q", models.ErrAudioDurationDetect, value)
	}

	return int(math.Ceil(duration)), nil
}
