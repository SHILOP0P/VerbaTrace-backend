package processing

import (
	"context"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"
	"verbatrace/monolit/internal/storage"
	"verbatrace/monolit/internal/transcriber"

	"github.com/google/uuid"
)

type AnalysisProcessor interface {
	ProcessAnalyzeCall(ctx context.Context, callID uuid.UUID) error
	MarkAnalyzeCallFailed(ctx context.Context, callID uuid.UUID, cause error) error
}

type Service struct {
	callRepository           repository.CallRepository
	transcriptionRepository  repository.TranscriptionRepository
	processingJobRepository  repository.ProcessingJobRepository
	audioStorage             storage.AudioStorage
	transcriber              transcriber.Transcriber
	analysisProcessor        AnalysisProcessor
	processingJobMaxAttempts int
	log                      logger.Logger
}

func NewService(
	callRepository repository.CallRepository,
	transcriptionRepository repository.TranscriptionRepository,
	processingJobRepository repository.ProcessingJobRepository,
	audioStorage storage.AudioStorage,
	transcriber transcriber.Transcriber,
	log logger.Logger,
) *Service {
	if log == nil {
		log = logger.NewNop()
	}

	return &Service{
		callRepository:           callRepository,
		transcriptionRepository:  transcriptionRepository,
		processingJobRepository:  processingJobRepository,
		audioStorage:             audioStorage,
		transcriber:              transcriber,
		processingJobMaxAttempts: models.DefaultProcessingJobMaxAttempts,
		log:                      log,
	}
}

func (s *Service) SetAnalysisProcessor(processor AnalysisProcessor) {
	s.analysisProcessor = processor
}

func (s *Service) SetProcessingJobMaxAttempts(maxAttempts int) {
	if maxAttempts <= 0 {
		maxAttempts = models.DefaultProcessingJobMaxAttempts
	}

	s.processingJobMaxAttempts = maxAttempts
}
