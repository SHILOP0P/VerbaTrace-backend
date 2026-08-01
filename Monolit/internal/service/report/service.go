package report

import (
	"context"
	"time"

	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"
	"verbatrace/monolit/internal/storage"

	"github.com/google/uuid"
)

const defaultReportRetention = 7 * 24 * time.Hour

type BillingLimiter interface {
	CanExportReports(ctx context.Context, companyID uuid.UUID) error
	GetPersonalSubscription(ctx context.Context, userID uuid.UUID) (models.Subscription, error)
}

type Service struct {
	callRepository          repo.CallRepository
	analysisRepository      repo.AnalysisRepository
	transcriptionRepository repo.TranscriptionRepository
	reportRepository        repo.ReportRepository
	reportStorage           storage.ReportStorage
	billingLimiter          BillingLimiter
	now                     func() time.Time
	retention               time.Duration
}

func NewService(
	callRepository repo.CallRepository,
	analysisRepository repo.AnalysisRepository,
	transcriptionRepository repo.TranscriptionRepository,
	reportRepository repo.ReportRepository,
	reportStorage storage.ReportStorage,
) *Service {
	return &Service{
		callRepository:          callRepository,
		analysisRepository:      analysisRepository,
		transcriptionRepository: transcriptionRepository,
		reportRepository:        reportRepository,
		reportStorage:           reportStorage,
		now:                     func() time.Time { return time.Now().UTC() },
		retention:               defaultReportRetention,
	}
}

func (s *Service) SetBillingLimiter(limiter BillingLimiter) {
	s.billingLimiter = limiter
}

func (s *Service) SetRetention(retention time.Duration) {
	if retention > 0 {
		s.retention = retention
	}
}
