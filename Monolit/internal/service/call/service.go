package call

import (
	"context"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"
	"verbatrace/monolit/internal/storage"

	"github.com/google/uuid"
)

const defaultProcessingJobMaxAttempts = models.DefaultProcessingJobMaxAttempts

type DurationDetector interface {
	DetectDuration(ctx context.Context, path string) (int, error)
}

type BillingLimiter interface {
	CanUploadPersonalCall(ctx context.Context, userID uuid.UUID, durationSeconds int) error
	CanUploadBusinessCall(ctx context.Context, companyID uuid.UUID, durationSeconds int) error
	AddPersonalUsageMinutes(ctx context.Context, userID uuid.UUID, durationSeconds int) error
	AddBusinessUsageMinutes(ctx context.Context, companyID uuid.UUID, durationSeconds int) error
}

type TranscriptionModeResolver interface {
	ResolveTranscriptionMode(ctx context.Context, userID uuid.UUID, companyID uuid.NullUUID) (models.TranscriptionMode, error)
}

type PrivacyAdmissionResolver interface {
	ResolveCallPrivacy(context.Context, models.Call) (models.CallPrivacyState, error)
}

// CreditReleaser hands back what a cancelled call had reserved. Cancelling work
// that never reached a provider must not leave the money committed.
type CreditReleaser interface {
	ReleaseCallReservations(ctx context.Context, callID uuid.UUID) error
}

type Service struct {
	repository                repo.CallRepository
	transcriptionRepository   repo.TranscriptionRepository
	processingJobRepository   repo.ProcessingJobRepository
	callFolderRepository      repo.CallFolderRepository
	companyRepository         repo.CompanyRepository
	departmentRepository      repo.DepartmentRepository
	audioStorage              storage.AudioStorage
	durationDetector          DurationDetector
	billingLimiter            BillingLimiter
	transcriptionModeResolver TranscriptionModeResolver
	privacyAdmissionResolver  PrivacyAdmissionResolver
	creditReleaser            CreditReleaser
	processingJobMaxAttempts  int
	log                       logger.Logger
}

func NewService(
	repository repo.CallRepository,
	companyRepository repo.CompanyRepository,
	departmentRepository repo.DepartmentRepository,
	audioStorage storage.AudioStorage,
	log logger.Logger,
) *Service {
	if log == nil {
		log = logger.NewNop()
	}

	return &Service{
		repository:               repository,
		companyRepository:        companyRepository,
		departmentRepository:     departmentRepository,
		audioStorage:             audioStorage,
		processingJobMaxAttempts: defaultProcessingJobMaxAttempts,
		log:                      log,
	}
}

// SetCreditReleaser wires the ledger so a cancelled call gives its reservation
// back instead of leaving it to the reconciler.
func (s *Service) SetCreditReleaser(releaser CreditReleaser) {
	s.creditReleaser = releaser
}

func (s *Service) SetTranscriptionRepository(repository repo.TranscriptionRepository) {
	s.transcriptionRepository = repository
}

func (s *Service) SetProcessingJobRepository(repository repo.ProcessingJobRepository) {
	s.processingJobRepository = repository
}

func (s *Service) SetCallFolderRepository(repository repo.CallFolderRepository) {
	s.callFolderRepository = repository
}

func (s *Service) SetProcessingJobMaxAttempts(maxAttempts int) {
	if maxAttempts <= 0 {
		maxAttempts = defaultProcessingJobMaxAttempts
	}

	s.processingJobMaxAttempts = maxAttempts
}

func (s *Service) SetDurationDetector(detector DurationDetector) {
	s.durationDetector = detector
}

func (s *Service) SetBillingLimiter(limiter BillingLimiter) {
	s.billingLimiter = limiter
}

func (s *Service) SetTranscriptionModeResolver(resolver TranscriptionModeResolver) {
	s.transcriptionModeResolver = resolver
}

func (s *Service) SetPrivacyAdmissionResolver(resolver PrivacyAdmissionResolver) {
	s.privacyAdmissionResolver = resolver
}
