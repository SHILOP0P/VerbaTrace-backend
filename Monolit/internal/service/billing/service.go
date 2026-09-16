package billing

import (
	"context"
	"fmt"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type Repository interface {
	GetPlanByCode(ctx context.Context, code models.PlanCode) (models.Plan, error)
	ListPlans(ctx context.Context) ([]models.Plan, error)
	GetActivePersonalSubscription(ctx context.Context, userID uuid.UUID) (models.Subscription, error)
	GetActiveBusinessSubscription(ctx context.Context, companyID uuid.UUID) (models.Subscription, error)
	GetBestActiveBusinessSubscriptionForManager(ctx context.Context, managerID uuid.UUID) (models.Subscription, error)
	UpsertSubscription(ctx context.Context, input models.UpsertSubscriptionInput) (models.Subscription, error)
	ActivatePersonalSubscription(ctx context.Context, input models.ActivatePersonalSubscriptionInput, startsAt time.Time) (models.Subscription, error)
	ActivateCompanySubscription(ctx context.Context, input models.ActivateCompanySubscriptionInput, startsAt time.Time) (models.Subscription, error)
	CancelCompanySubscription(ctx context.Context, companyID uuid.UUID, canceledAt time.Time) (models.Subscription, error)
	CountUsedMinutes(ctx context.Context, subscriptionID uuid.UUID, periodStart time.Time) (int, error)
	AddUsageMinutes(ctx context.Context, subscriptionID uuid.UUID, periodStart time.Time, minutes int) (models.UsageCounter, error)
	CountOwnerCompanies(ctx context.Context, ownerID uuid.UUID) (int, error)
	CountCompanyDepartments(ctx context.Context, companyID uuid.UUID) (int, error)
	CountCompanyMembers(ctx context.Context, companyID uuid.UUID) (int, error)
	CountActiveInstructions(ctx context.Context, input models.ListAnalysisInstructionsInput) (int, error)
}

type CompanyRepository interface {
	GetCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyMember, error)
}

type CreditRepository interface {
	EnsureCurrentCreditUsage(ctx context.Context, subscription models.Subscription, now time.Time) (models.CreditUsage, error)
}

type Service struct {
	repository           Repository
	companyRepository    CompanyRepository
	departmentRepository DepartmentRepository
	creditRepository     CreditRepository
	creditOperations     creditOperationRepository
	creditLimits         creditLimitRepository
	creditDashboardRepo  creditDashboardRepository
	developerRepository  developerRepository
	now                  func() time.Time
}

// SetCreditRepository wires the credit ledger. The ledger is used through
// several narrow interfaces, and a repository that does not satisfy one of them
// used to panic on the first call that needed it. The check happens here, at
// startup, so a wrong wiring stops the process instead of one request.
func (s *Service) SetCreditRepository(repository CreditRepository) error {
	s.creditRepository = repository

	operations, ok := repository.(creditOperationRepository)
	if !ok {
		return fmt.Errorf("credit repository does not support credit operations")
	}
	limits, ok := repository.(creditLimitRepository)
	if !ok {
		return fmt.Errorf("credit repository does not support credit limits")
	}
	dashboard, ok := repository.(creditDashboardRepository)
	if !ok {
		return fmt.Errorf("credit repository does not support the credit dashboard")
	}
	developer, ok := repository.(developerRepository)
	if !ok {
		return fmt.Errorf("credit repository does not support developer applications")
	}

	s.creditOperations = operations
	s.creditLimits = limits
	s.creditDashboardRepo = dashboard
	s.developerRepository = developer

	return nil
}

func NewService(repository Repository) *Service {
	return &Service{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetCompanyRepository(repository CompanyRepository) {
	s.companyRepository = repository
}
