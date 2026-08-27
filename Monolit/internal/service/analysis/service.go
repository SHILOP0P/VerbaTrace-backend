package analysis

import (
	"context"

	"verbatrace/monolit/internal/analyzer"
	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"
	"verbatrace/monolit/internal/storage"

	"github.com/google/uuid"
)

type PersonalizationReader interface {
	ContextForCall(ctx context.Context, call models.Call) ([]string, error)
}

type FolderInstructionReader interface {
	ListInstructionsForCall(ctx context.Context, callID uuid.UUID) ([]models.AnalysisInstruction, error)
}

type CreditMeter interface {
	ReserveAnalysis(context.Context, models.Call, uuid.UUID, string, int64) (uuid.UUID, error)
	SettleAnalysis(context.Context, uuid.UUID, *models.ProviderUsage) error
	MarkCreditOperationReconciling(context.Context, uuid.UUID, string) error
	IsSandboxMockCall(context.Context, uuid.UUID) (bool, error)
}

type attemptRepository interface {
	CreateAttempt(context.Context, uuid.UUID, uuid.UUID) (models.CallAnalysisAttempt, error)
	ActiveAttempt(context.Context, uuid.UUID) (models.CallAnalysisAttempt, error)
	MarkAttempt(context.Context, uuid.UUID, string, error) error
	CurrentTranscriptionRevision(context.Context, uuid.UUID) (int, error)
}

type instructionSnapshotRepository interface {
	SaveInstructionSnapshots(context.Context, uuid.UUID, []models.AnalysisInstructionContent) error
	ListInstructionSnapshots(context.Context, uuid.UUID) ([]models.AppliedInstruction, error)
	GetInstructionSnapshot(context.Context, uuid.UUID, uuid.UUID) (models.AppliedInstruction, error)
	InstructionSnapshotCallUUID(context.Context, uuid.UUID) (uuid.UUID, error)
}

type Service struct {
	callRepository           repo.CallRepository
	transcriptionRepository  repo.TranscriptionRepository
	instructionRepository    repo.AnalysisInstructionRepository
	analysisRepository       repo.AnalysisRepository
	processingJobRepository  repo.ProcessingJobRepository
	instructionStorage       storage.InstructionStorage
	analyzer                 analyzer.Analyzer
	sandboxAnalyzer          analyzer.Analyzer
	processingJobMaxAttempts int
	log                      logger.Logger
	personalizationReader    PersonalizationReader
	folderInstructionReader  FolderInstructionReader
	creditMeter              CreditMeter
}

func (s *Service) SetCreditMeter(meter CreditMeter)              { s.creditMeter = meter }
func (s *Service) SetSandboxAnalyzer(provider analyzer.Analyzer) { s.sandboxAnalyzer = provider }

func (s *Service) SetPersonalizationReader(reader PersonalizationReader) {
	s.personalizationReader = reader
}
func (s *Service) SetFolderInstructionReader(reader FolderInstructionReader) {
	s.folderInstructionReader = reader
}

func NewService(
	callRepository repo.CallRepository,
	transcriptionRepository repo.TranscriptionRepository,
	instructionRepository repo.AnalysisInstructionRepository,
	analysisRepository repo.AnalysisRepository,
	instructionStorage storage.InstructionStorage,
	analyzer analyzer.Analyzer,
	log logger.Logger,
) *Service {
	if log == nil {
		log = logger.NewNop()
	}

	return &Service{
		callRepository:           callRepository,
		transcriptionRepository:  transcriptionRepository,
		instructionRepository:    instructionRepository,
		analysisRepository:       analysisRepository,
		instructionStorage:       instructionStorage,
		analyzer:                 analyzer,
		processingJobMaxAttempts: models.DefaultProcessingJobMaxAttempts,
		log:                      log,
	}
}

func (s *Service) SetProcessingJobRepository(repository repo.ProcessingJobRepository) {
	s.processingJobRepository = repository
}

func (s *Service) SetProcessingJobMaxAttempts(maxAttempts int) {
	if maxAttempts <= 0 {
		maxAttempts = models.DefaultProcessingJobMaxAttempts
	}

	s.processingJobMaxAttempts = maxAttempts
}
