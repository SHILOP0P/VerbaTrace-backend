package processing

import (
	"errors"

	"verbatrace/monolit/internal/models"
)

func isPermanentProcessingError(err error) bool {
	return errors.Is(err, models.ErrInvalidProcessingJobType) ||
		errors.Is(err, models.ErrCallNotFound) ||
		errors.Is(err, models.ErrInvalidCallStatus) ||
		errors.Is(err, models.ErrInvalidCallStatusTransition) ||
		errors.Is(err, models.ErrAudioFileNotFound) ||
		errors.Is(err, models.ErrInvalidAudioPath) ||
		errors.Is(err, models.ErrUnsupportedAudioType) ||
		errors.Is(err, models.ErrTranscriberNotConfigured) ||
		errors.Is(err, models.ErrAnalyzerNotConfigured) ||
		errors.Is(err, models.ErrInvalidAnalysisStatus)
}
