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

type PrivacyContextReader interface {
	AnalysisContext(context.Context, uuid.UUID) (*models.AnalysisRedactionContext, error)
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

// ScorecardPlanner picks the scorecard criteria an analysis is scored on.
type ScorecardPlanner interface {
	PlanForAnalysis(ctx context.Context, instructions []models.AnalysisInstructionContent) (models.ScorecardPlan, error)
}

// instructionTextCache is implemented by the instruction repository. Without it
// the service reads and extracts the current file on every analysis.
type instructionTextCache interface {
	ReadableVersion(context.Context, models.AnalysisInstruction) (models.InstructionVersionText, error)
	SaveVersionText(context.Context, uuid.UUID, string) error
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
	companyRepository        repo.CompanyRepository
	departmentRepository     repo.DepartmentRepository
	instructionStorage       storage.InstructionStorage
	analyzer                 analyzer.Analyzer
	sandboxAnalyzer          analyzer.Analyzer
	processingJobMaxAttempts int
	log                      logger.Logger
	personalizationReader    PersonalizationReader
	folderInstructionReader  FolderInstructionReader
	creditMeter              CreditMeter
	privacyContextReader     PrivacyContextReader
	notifications            NotificationSender
	scorecards               ScorecardPlanner
	facts                    FactsProjector
	growth                   GrowthKeeper
}

func (s *Service) SetScorecardPlanner(planner ScorecardPlanner) { s.scorecards = planner }

// GrowthKeeper tells the summary step about an employee's open growth areas and
// stores what it said about them.
type GrowthKeeper interface {
	ContextFor(ctx context.Context, callID uuid.UUID) (*models.GrowthContext, error)
	Record(ctx context.Context, callID uuid.UUID, outcome models.GrowthOutcome) error
}

func (s *Service) SetGrowth(keeper GrowthKeeper) { s.growth = keeper }

// FactsProjector re-projects the analytics facts of a call once its analysis is
// done.
type FactsProjector interface {
	Refresh(ctx context.Context, callID uuid.UUID)
}

func (s *Service) SetFactsProjector(projector FactsProjector) { s.facts = projector }

// SetMembershipRepositories enables the rerun rules: without them the service
// only knows about personal calls.
func (s *Service) SetMembershipRepositories(company repo.CompanyRepository, department repo.DepartmentRepository) {
	s.companyRepository = company
	s.departmentRepository = department
}

func (s *Service) SetCreditMeter(meter CreditMeter)              { s.creditMeter = meter }
func (s *Service) SetSandboxAnalyzer(provider analyzer.Analyzer) { s.sandboxAnalyzer = provider }

func (s *Service) SetPersonalizationReader(reader PersonalizationReader) {
	s.personalizationReader = reader
}
func (s *Service) SetFolderInstructionReader(reader FolderInstructionReader) {
	s.folderInstructionReader = reader
}
func (s *Service) SetPrivacyContextReader(reader PrivacyContextReader) {
	s.privacyContextReader = reader
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
