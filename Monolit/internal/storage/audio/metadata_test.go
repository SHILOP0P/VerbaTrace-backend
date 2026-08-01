package audio

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"verbatrace/monolit/internal/models"
)

func TestParseFFProbeDuration(t *testing.T) {
	tests := []struct {
		name    string
		output  []byte
		want    int
		wantErr error
	}{
		{
			name:   "rounds up fractional seconds",
			output: []byte("123.456000\n"),
			want:   124,
		},
		{
			name:   "keeps whole seconds",
			output: []byte("42.000000\n"),
			want:   42,
		},
		{
			name:    "rejects empty output",
			output:  []byte(" \n"),
			wantErr: models.ErrAudioDurationDetect,
		},
		{
			name:    "rejects invalid output",
			output:  []byte("not-a-duration"),
			wantErr: models.ErrAudioDurationDetect,
		},
		{
			name:    "rejects zero duration",
			output:  []byte("0.000000"),
			wantErr: models.ErrAudioDurationDetect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFFProbeDuration(tt.output)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("parseFFProbeDuration() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFFProbeDuration() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseFFProbeDuration() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestFFProbeDurationDetectorDetectDurationMapsMissingProbe(t *testing.T) {
	dir := t.TempDir()
	filename := "call.wav"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("audio"), 0644); err != nil {
		t.Fatalf("failed to write audio file: %v", err)
	}

	detector := NewFFProbeDurationDetector(dir, "ffprobe-verbatrace-test-missing")

	_, err := detector.DetectDuration(context.Background(), filename)
	if !errors.Is(err, models.ErrAudioProbeNotFound) {
		t.Fatalf("DetectDuration() error = %v, want %v", err, models.ErrAudioProbeNotFound)
	}
}

func TestFFProbeDurationDetectorDetectDurationMapsUnreadableFile(t *testing.T) {
	detector := NewFFProbeDurationDetector(t.TempDir(), "ffprobe")

	_, err := detector.DetectDuration(context.Background(), "missing.wav")
	if !errors.Is(err, models.ErrAudioFileUnreadable) {
		t.Fatalf("DetectDuration() error = %v, want %v", err, models.ErrAudioFileUnreadable)
	}
}
