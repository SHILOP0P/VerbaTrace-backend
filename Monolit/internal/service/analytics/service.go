package analytics

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

// TeamAnalyticsGate answers whether a company's plan includes team analytics.
type TeamAnalyticsGate interface {
	CanAccessTeamAnalytics(ctx context.Context, companyID uuid.UUID) error
}

// departmentCompanyReader resolves the company of a department filter, so a
// request filtered by department alone is held to its company's plan.
type departmentCompanyReader interface {
	DepartmentCompany(ctx context.Context, departmentID uuid.UUID) (uuid.UUID, error)
}

type Service struct {
	analyticsRepository  repository.AnalyticsRepository
	callFolderRepository repository.CallFolderRepository
	teamAnalyticsGate    TeamAnalyticsGate
}

func NewService(analyticsRepository repository.AnalyticsRepository) *Service {
	return &Service{analyticsRepository: analyticsRepository}
}

func (s *Service) SetCallFolderRepository(repository repository.CallFolderRepository) {
	s.callFolderRepository = repository
}

func (s *Service) SetTeamAnalyticsGate(gate TeamAnalyticsGate) {
	s.teamAnalyticsGate = gate
}

func (s *Service) GetOverview(ctx context.Context, input models.AnalyticsOverviewInput) (models.AnalyticsOverview, error) {
	if input.FolderUUID.Valid {
		if s.callFolderRepository == nil {
			return models.AnalyticsOverview{}, models.ErrCallFolderNotFound
		}
		if _, err := s.callFolderRepository.GetVisibleByUUID(ctx, input.FolderUUID.UUID, input.UserID); err != nil {
			return models.AnalyticsOverview{}, err
		}
	}
	allowed, err := s.teamAnalyticsAllowed(ctx, input)
	if err != nil {
		return models.AnalyticsOverview{}, err
	}
	overview, err := s.analyticsRepository.GetAnalyticsOverview(ctx, input)
	if err != nil {
		return models.AnalyticsOverview{}, err
	}
	overview.TeamAnalyticsEnabled = allowed
	if !allowed {
		withoutTeamBreakdown(&overview)
	}
	return overview, nil
}

// teamAnalyticsAllowed decides for a request scoped to one company. The
// overview itself stays available on every plan — the owner of the lowest
// business plan still sees the counters and the overall score — so a plan
// without team analytics only loses the breakdowns. A request without a
// company filter mixes personal and company calls and keeps its old behaviour.
func (s *Service) teamAnalyticsAllowed(ctx context.Context, input models.AnalyticsOverviewInput) (bool, error) {
	if s.teamAnalyticsGate == nil {
		return true, nil
	}
	company := input.CompanyUUID
	if !company.Valid && input.DepartmentUUID.Valid {
		reader, ok := s.analyticsRepository.(departmentCompanyReader)
		if !ok {
			return true, nil
		}
		id, err := reader.DepartmentCompany(ctx, input.DepartmentUUID.UUID)
		if err != nil {
			return false, err
		}
		company = uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
	}
	if !company.Valid {
		return true, nil
	}
	err := s.teamAnalyticsGate.CanAccessTeamAnalytics(ctx, company.UUID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, models.ErrTeamAnalyticsAccessDenied),
		errors.Is(err, models.ErrSubscriptionRequired),
		errors.Is(err, models.ErrSubscriptionNotFound):
		return false, nil
	default:
		return false, err
	}
}

func withoutTeamBreakdown(overview *models.AnalyticsOverview) {
	overview.CriteriaSummary = []models.AnalyticsCriterionSummary{}
	overview.TopWeakCriteria = []models.AnalyticsWeakCriterion{}
	overview.TopIssueCodes = []models.AnalyticsCodeCount{}
	overview.BusinessOutcomes = []models.AnalyticsStatusCount{}
	overview.NextStepSummary = models.AnalyticsNextStepSummary{}
	overview.TopTopics = []models.AnalyticsTopicCount{}
	overview.RisksCount = nil
	overview.RecommendationsCount = nil
	overview.Charts.RisksByDay = []models.AnalyticsCountPoint{}
}
