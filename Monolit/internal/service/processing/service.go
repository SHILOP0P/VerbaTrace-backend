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

type CreditMeter interface {
	ReserveTranscription(context.Context, models.Call, models.TranscriptionMode) (uuid.UUID, error)
	SettleTranscription(context.Context, uuid.UUID, models.Call, models.TranscriptionMode) error
	MarkCreditOperationReconciling(context.Context, uuid.UUID, string) error
	IsSandboxMockCall(context.Context, uuid.UUID) (bool, error)
}

type Service struct {
	callRepository           repository.CallRepository
	transcriptionRepository  repository.TranscriptionRepository
	processingJobRepository  repository.ProcessingJobRepository
	audioStorage             storage.AudioStorage
	transcriber              transcriber.Transcriber
	sandboxTranscriber       transcriber.Transcriber
	analysisProcessor        AnalysisProcessor
	processingJobMaxAttempts int
	log                      logger.Logger
	creditMeter              CreditMeter
}

func (s *Service) SetCreditMeter(meter CreditMeter) { s.creditMeter = meter }
func (s *Service) SetSandboxTranscriber(provider transcriber.Transcriber) {
	s.sandboxTranscriber = provider
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
