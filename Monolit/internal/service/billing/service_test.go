package billing

import (
	"context"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCanUploadPersonalCallWhenMinutesExceeded(t *testing.T) {
	userID := uuid.New()
	subscriptionID := uuid.New()
	repository := &fakeRepository{
		personalSubscription: models.Subscription{
			ID: subscriptionID,
			Plan: models.Plan{
				MonthlyMinutesLimit: 10,
			},
		},
		usedMinutes: 9,
	}

	service := NewService(repository)

	err := service.CanUploadPersonalCall(context.Background(), userID, 61)

	require.ErrorIs(t, err, models.ErrMonthlyMinutesLimitExceeded)
}

func TestPersonalStartCannotCreateMoreThanTwoActivePersonalInstructions(t *testing.T) {
	repository := &fakeRepository{
		personalSubscription: models.Subscription{
			ID: uuid.New(),
			Plan: models.Plan{
				ActiveInstructionLimit: 2,
			},
		},
		instructionsCount: 2,
	}

	service := NewService(repository)

	err := service.CanCreatePersonalInstruction(context.Background(), uuid.New())

	require.ErrorIs(t, err, models.ErrInstructionLimitExceeded)
}

func TestCanCreateDepartmentWhenLimitExceeded(t *testing.T) {
	companyID := uuid.New()
	limit := 5
	repository := &fakeRepository{
		businessSubscription: models.Subscription{
			ID: uuid.New(),
			Plan: models.Plan{
				DepartmentsPerCompanyLimit: &limit,
			},
		},
		departmentsCount: 5,
	}

	service := NewService(repository)

	err := service.CanCreateDepartment(context.Background(), companyID)

	require.ErrorIs(t, err, models.ErrDepartmentLimitExceeded)
}

func TestBusinessPlusCannotCreateMoreThanSevenActiveDepartmentInstructions(t *testing.T) {
	limit := 7
	repository := &fakeRepository{
		businessSubscription: models.Subscription{
			ID: uuid.New(),
			Plan: models.Plan{
				InstructionsPerDepartmentLimit: &limit,
			},
		},
		instructionsCount: 7,
	}

	service := NewService(repository)

	err := service.CanCreateDepartmentInstruction(context.Background(), uuid.New(), uuid.New())

	require.ErrorIs(t, err, models.ErrInstructionLimitExceeded)
}

func TestCanCreateCompanyDoesNotRequireBusinessSubscription(t *testing.T) {
	service := NewService(&fakeRepository{
		businessErr: models.ErrSubscriptionNotFound,
	})

	err := service.CanCreateCompany(context.Background(), uuid.New())

	require.NoError(t, err)
}

func TestAPIAccessAvailableOnlyOnBusinessPro(t *testing.T) {
	repository := &fakeRepository{
		businessSubscription: models.Subscription{
			ID: uuid.New(),
			Plan: models.Plan{
				APIAccessEnabled: false,
			},
		},
	}

	service := NewService(repository)

	err := service.CanAccessAPI(context.Background(), uuid.New())

	require.ErrorIs(t, err, models.ErrAPIAccessDenied)

	repository.businessSubscription.Plan.APIAccessEnabled = true

	err = service.CanAccessAPI(context.Background(), uuid.New())

	require.NoError(t, err)
}

func TestSubscriptionRequiredWhenActiveSubscriptionIsMissing(t *testing.T) {
	repository := &fakeRepository{
		personalErr: models.ErrSubscriptionNotFound,
	}

	service := NewService(repository)

	err := service.CanCreatePersonalInstruction(context.Background(), uuid.New())

	require.ErrorIs(t, err, models.ErrSubscriptionRequired)
}

func TestManagerBusinessSubscriptionRaisesPersonalLevel(t *testing.T) {
	userID := uuid.New()
	subscriptionID := uuid.New()
	repository := &fakeRepository{
		personalSubscription: models.Subscription{
			ID: subscriptionID,
			Plan: models.Plan{
				Code:                models.PlanCodePersonalStart,
				Type:                models.PlanTypePersonal,
				MonthlyMinutesLimit: 120,
			},
		},
		managerBusinessSubscription: models.Subscription{
			ID: uuid.New(),
			Plan: models.Plan{
				Code: models.PlanCodeBusinessStart,
				Type: models.PlanTypeBusiness,
			},
		},
		plans: map[models.PlanCode]models.Plan{
			models.PlanCodePersonalPlus: {
				Code:                models.PlanCodePersonalPlus,
				Type:                models.PlanTypePersonal,
				MonthlyMinutesLimit: 600,
			},
		},
		usedMinutes: 120,
	}

	service := NewService(repository)

	err := service.CanUploadPersonalCall(context.Background(), userID, 60)

	require.NoError(t, err)
}

func TestGetPersonalSubscriptionUsageCalculatesPeriodAndRemaining(t *testing.T) {
	userID := uuid.New()
	subscriptionID := uuid.New()
	repository := &fakeRepository{
		personalSubscription: models.Subscription{
			ID: subscriptionID,
			Plan: models.Plan{
				MonthlyMinutesLimit: 7200,
			},
		},
		usedMinutes: 86,
	}
	service := NewService(repository)

	period := time.Date(2026, 7, 19, 12, 0, 0, 0, time.FixedZone("MSK", 3*60*60))
	usage, err := service.GetPersonalSubscriptionUsage(context.Background(), models.GetPersonalSubscriptionUsageInput{
		UserUUID:    userID,
		PeriodStart: &period,
	})

	require.NoError(t, err)
	require.Equal(t, subscriptionID, repository.countUsedSubscriptionID)
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), repository.countUsedPeriodStart)
	require.Equal(t, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), usage.PeriodStart)
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), usage.PeriodEnd)
	require.Equal(t, 86, usage.UsedMinutes)
	require.Equal(t, 7200, usage.LimitMinutes)
	require.Equal(t, 7114, usage.RemainingMinutes)
	require.Equal(t, 1.19, usage.Percent)
}

func TestGetPersonalSubscriptionUsageUsesCurrentMonthByDefault(t *testing.T) {
	repository := &fakeRepository{
		personalSubscription: models.Subscription{
			ID: uuid.New(),
			Plan: models.Plan{
				MonthlyMinutesLimit: 3,
			},
		},
		usedMinutes: 4,
	}
	service := NewService(repository)
	service.now = func() time.Time {
		return time.Date(2026, 8, 25, 15, 0, 0, 0, time.UTC)
	}

	usage, err := service.GetPersonalSubscriptionUsage(context.Background(), models.GetPersonalSubscriptionUsageInput{
		UserUUID: uuid.New(),
	})

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), usage.PeriodStart)
	require.Equal(t, 0, usage.RemainingMinutes)
	require.Equal(t, 133.33, usage.Percent)
}

func TestGetCompanySubscriptionUsageIncludesCounters(t *testing.T) {
	companyID := uuid.New()
	managerID := uuid.New()
	membersLimit := 15
	departmentsLimit := 5
	instructionsLimit := 10
	repository := &fakeRepository{
		businessSubscription: models.Subscription{
			ID: uuid.New(),
			Plan: models.Plan{
				MonthlyMinutesLimit:            1000,
				MembersPerCompanyLimit:         &membersLimit,
				DepartmentsPerCompanyLimit:     &departmentsLimit,
				InstructionsPerDepartmentLimit: &instructionsLimit,
			},
		},
		membersCount:      8,
		departmentsCount:  2,
		instructionsCount: 4,
	}
	service := NewService(repository)
	service.SetCompanyRepository(&fakeCompanyRepository{
		member: models.CompanyMember{
			CompanyUUID: companyID,
			UserUUID:    managerID,
			Role:        models.CompanyMemberRoleManager,
			Status:      models.MembershipStatusActive,
		},
	})

	usage, err := service.GetCompanySubscriptionUsage(context.Background(), models.GetCompanySubscriptionUsageInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
	})

	require.NoError(t, err)
	require.NotNil(t, usage.MembersLimit)
	require.NotNil(t, usage.MembersUsed)
	require.NotNil(t, usage.DepartmentsLimit)
	require.NotNil(t, usage.DepartmentsUsed)
	require.NotNil(t, usage.ActiveInstructionsLimit)
	require.NotNil(t, usage.ActiveInstructionsUsed)
	require.Equal(t, 15, *usage.MembersLimit)
	require.Equal(t, 8, *usage.MembersUsed)
	require.Equal(t, 5, *usage.DepartmentsLimit)
	require.Equal(t, 2, *usage.DepartmentsUsed)
	require.Equal(t, 10, *usage.ActiveInstructionsLimit)
	require.Equal(t, 4, *usage.ActiveInstructionsUsed)
}

func TestGetCompanySubscriptionAllowsActiveEmployeeToReadPlan(t *testing.T) {
	companyID := uuid.New()
	employeeID := uuid.New()
	expected := models.Subscription{ID: uuid.New(), Plan: models.Plan{Code: models.PlanCodeBusinessStart}}
	service := NewService(&fakeRepository{businessSubscription: expected})
	service.SetCompanyRepository(&fakeCompanyRepository{member: models.CompanyMember{
		CompanyUUID: companyID,
		UserUUID:    employeeID,
		Role:        models.CompanyMemberRoleEmployee,
		Status:      models.MembershipStatusActive,
	}})

	actual, err := service.GetCompanySubscription(context.Background(), models.GetCompanySubscriptionInput{
		CompanyUUID: companyID,
		RequestUser: employeeID,
	})

	require.NoError(t, err)
	require.Equal(t, expected.ID, actual.ID)
	require.Equal(t, models.PlanCodeBusinessStart, actual.Plan.Code)
}

type fakeRepository struct {
	personalSubscription        models.Subscription
	businessSubscription        models.Subscription
	managerBusinessSubscription models.Subscription
	plan                        models.Plan
	plans                       map[models.PlanCode]models.Plan
	personalErr                 error
	businessErr                 error
	managerBusinessErr          error
	planErr                     error
	usedMinutes                 int
	companiesCount              int
	departmentsCount            int
	membersCount                int
	instructionsCount           int
	cancelCompanyID             uuid.UUID
	cancelSubscription          models.Subscription
	cancelErr                   error
	countUsedSubscriptionID     uuid.UUID
	countUsedPeriodStart        time.Time
}

func (f *fakeRepository) GetPlanByCode(_ context.Context, code models.PlanCode) (models.Plan, error) {
	if f.planErr != nil {
		return models.Plan{}, f.planErr
	}
	if f.plans != nil {
		if plan, ok := f.plans[code]; ok {
			return plan, nil
		}
	}
	if f.plan.Code == "" {
		switch code {
		case models.PlanCodePersonalStart, models.PlanCodePersonalPlus, models.PlanCodePersonalPro:
			return models.Plan{Code: code, Type: models.PlanTypePersonal}, nil
		default:
			return models.Plan{Code: code, Type: models.PlanTypeBusiness}, nil
		}
	}
	return f.plan, nil
}

func (f *fakeRepository) ListPlans(context.Context) ([]models.Plan, error) {
	return nil, nil
}

func (f *fakeRepository) GetActivePersonalSubscription(context.Context, uuid.UUID) (models.Subscription, error) {
	if f.personalErr != nil {
		return models.Subscription{}, f.personalErr
	}
	return f.personalSubscription, nil
}

func (f *fakeRepository) GetActiveBusinessSubscription(context.Context, uuid.UUID) (models.Subscription, error) {
	if f.businessErr != nil {
		return models.Subscription{}, f.businessErr
	}
	return f.businessSubscription, nil
}

func (f *fakeRepository) GetBestActiveBusinessSubscriptionForManager(context.Context, uuid.UUID) (models.Subscription, error) {
	if f.managerBusinessErr != nil {
		return models.Subscription{}, f.managerBusinessErr
	}
	if f.managerBusinessSubscription.ID == uuid.Nil {
		return models.Subscription{}, models.ErrSubscriptionNotFound
	}
	return f.managerBusinessSubscription, nil
}

func (f *fakeRepository) UpsertSubscription(context.Context, models.UpsertSubscriptionInput) (models.Subscription, error) {
	return models.Subscription{}, nil
}

func (f *fakeRepository) CancelCompanySubscription(_ context.Context, companyID uuid.UUID, _ time.Time) (models.Subscription, error) {
	f.cancelCompanyID = companyID
	if f.cancelErr != nil {
		return models.Subscription{}, f.cancelErr
	}
	return f.cancelSubscription, nil
}

func (f *fakeRepository) CountUsedMinutes(_ context.Context, subscriptionID uuid.UUID, periodStart time.Time) (int, error) {
	f.countUsedSubscriptionID = subscriptionID
	f.countUsedPeriodStart = periodStart
	return f.usedMinutes, nil
}

func (f *fakeRepository) AddUsageMinutes(context.Context, uuid.UUID, time.Time, int) (models.UsageCounter, error) {
	return models.UsageCounter{}, nil
}

func (f *fakeRepository) CountOwnerCompanies(context.Context, uuid.UUID) (int, error) {
	return f.companiesCount, nil
}

func (f *fakeRepository) CountCompanyDepartments(context.Context, uuid.UUID) (int, error) {
	return f.departmentsCount, nil
}

func (f *fakeRepository) CountCompanyMembers(context.Context, uuid.UUID) (int, error) {
	return f.membersCount, nil
}

func (f *fakeRepository) CountActiveInstructions(context.Context, models.ListAnalysisInstructionsInput) (int, error) {
	return f.instructionsCount, nil
}

type fakeCompanyRepository struct {
	member            models.CompanyMember
	err               error
	creditVisible     bool
	visibilityUpdated bool
}

func (f *fakeCompanyRepository) GetCompanyMember(context.Context, uuid.UUID, uuid.UUID) (models.CompanyMember, error) {
	if f.err != nil {
		return models.CompanyMember{}, f.err
	}
	return f.member, nil
}

func (f *fakeCompanyRepository) GetCompanyCreditUsageVisibility(context.Context, uuid.UUID) (bool, error) {
	return f.creditVisible, f.err
}

func (f *fakeCompanyRepository) UpdateCompanyCreditUsageVisibility(_ context.Context, _ uuid.UUID, visible bool) error {
	if f.err != nil {
		return f.err
	}
	f.creditVisible = visible
	f.visibilityUpdated = true
	return nil
}

func TestCompanyManagerCanUpdateCreditVisibility(t *testing.T) {
	companyID := uuid.New()
	managerID := uuid.New()
	companyRepository := &fakeCompanyRepository{member: models.CompanyMember{Role: models.CompanyMemberRoleManager}}
	service := NewService(&fakeRepository{})
	service.SetCompanyRepository(companyRepository)

	result, err := service.UpdateCompanyCreditVisibility(context.Background(), models.UpdateCompanyCreditVisibilityInput{
		CompanyUUID: companyID,
		RequestUser: managerID,
		Visible:     true,
	})

	require.NoError(t, err)
	require.True(t, result.VisibleToMembers)
	require.True(t, result.CanManageVisibility)
	require.True(t, companyRepository.visibilityUpdated)
}

func TestCompanyEmployeeCannotUpdateCreditVisibility(t *testing.T) {
	companyRepository := &fakeCompanyRepository{member: models.CompanyMember{Role: models.CompanyMemberRoleEmployee}}
	service := NewService(&fakeRepository{})
	service.SetCompanyRepository(companyRepository)

	_, err := service.UpdateCompanyCreditVisibility(context.Background(), models.UpdateCompanyCreditVisibilityInput{
		CompanyUUID: uuid.New(),
		RequestUser: uuid.New(),
		Visible:     false,
	})

	require.ErrorIs(t, err, models.ErrForbidden)
	require.False(t, companyRepository.visibilityUpdated)
}
