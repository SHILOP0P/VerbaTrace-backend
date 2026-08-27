package processing

import (
	"errors"
	"fmt"
	"testing"

	"verbatrace/monolit/internal/models"
)

func TestIsPermanentProcessingError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "invalid job type",
			err:  models.ErrInvalidProcessingJobType,
			want: true,
		},
		{
			name: "wrapped call not found",
			err:  fmt.Errorf("get call for processing: %w", models.ErrCallNotFound),
			want: true,
		},
		{
			name: "invalid status transition",
			err:  fmt.Errorf("validate call status: %w", models.ErrInvalidCallStatusTransition),
			want: true,
		},
		{
			name: "invalid status",
			err:  fmt.Errorf("validate call status: %w", models.ErrInvalidCallStatus),
			want: true,
		},
		{
			name: "missing audio",
			err:  fmt.Errorf("open audio: %w", models.ErrAudioFileNotFound),
			want: true,
		},
		{
			name: "invalid audio path",
			err:  fmt.Errorf("open audio: %w", models.ErrInvalidAudioPath),
			want: true,
		},
		{
			name: "unsupported audio type",
			err:  fmt.Errorf("transcribe audio: %w", models.ErrUnsupportedAudioType),
			want: true,
		},
		{
			name: "transcriber not configured",
			err:  models.ErrTranscriberNotConfigured,
			want: true,
		},
		{
			name: "analyzer not configured",
			err:  models.ErrAnalyzerNotConfigured,
			want: true,
		},
		{
			name: "invalid analysis status",
			err:  fmt.Errorf("analyze call: %w", models.ErrInvalidAnalysisStatus),
			want: true,
		},
		{
			name: "insufficient credits",
			err:  fmt.Errorf("reserve analysis credits: %w", models.ErrInsufficientCredits),
			want: true,
		},
		{
			name: "application budget exceeded",
			err:  fmt.Errorf("reserve analysis credits: %w", models.ErrApplicationBudgetExceeded),
			want: true,
		},
		{
			name: "temporary provider error",
			err:  errors.New("provider timeout"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPermanentProcessingError(tt.err); got != tt.want {
				t.Fatalf("isPermanentProcessingError() = %v, want %v", got, tt.want)
			}
		})
	}
}
