package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
	billingMocks "verbatrace/monolit/internal/service/billing/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestReadAndFeatureMethodsWithMockery(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	subscriptionID := uuid.New()
	limit := 10
	subscription := models.Subscription{
		ID: subscriptionID,
		Plan: models.Plan{
			AnalysisLevel: models.AnalysisLevelPro, MonthlyMinutesLimit: 100,
			ExportEnabled: true, TeamAnalyticsEnabled: true, APIAccessEnabled: true,
			MembersPerCompanyLimit: &limit, DepartmentsPerCompanyLimit: &limit,
		},
	}
	repo := billingMocks.NewRepository(t)
	service := NewService(repo)

	repo.EXPECT().GetActivePersonalSubscription(mock.Anything, userID).Return(subscription, nil).Times(4)
	repo.EXPECT().GetBestActiveBusinessSubscriptionForManager(mock.Anything, userID).
		Return(models.Subscription{}, models.ErrSubscriptionNotFound).Times(4)
	if level, err := service.AnalysisLevelForUser(ctx, userID); err != nil || level != models.AnalysisLevelPro {
		t.Fatalf("user level = %q, %v", level, err)
	}
	if got, err := service.GetPersonalSubscription(ctx, userID); err != nil || got.ID != subscriptionID {
		t.Fatalf("personal subscription = %+v, %v", got, err)
	}
	repo.EXPECT().CountUsedMinutes(mock.Anything, subscriptionID, mock.Anything).Return(0, nil).Once()
	if err := service.CanUploadPersonalCall(ctx, userID, 60); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().AddUsageMinutes(mock.Anything, subscriptionID, mock.Anything, 1).
		Return(models.UsageCounter{}, nil).Once()
	if err := service.AddPersonalUsageMinutes(ctx, userID, 60); err != nil {
		t.Fatal(err)
	}

	repo.EXPECT().GetActiveBusinessSubscription(mock.Anything, companyID).Return(subscription, nil).Times(8)
	if level, err := service.AnalysisLevelForCompany(ctx, companyID); err != nil || level != models.AnalysisLevelPro {
		t.Fatalf("company level = %q, %v", level, err)
	}
	if err := service.CanUseCompany(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	if err := service.CanAccessAPI(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	if err := service.CanExportReports(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	if err := service.CanAccessTeamAnalytics(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().CountCompanyMembers(mock.Anything, companyID).Return(1, nil).Once()
	if err := service.CanAddCompanyMember(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().CountUsedMinutes(mock.Anything, subscriptionID, mock.Anything).Return(0, nil).Once()
	if err := service.CanUploadBusinessCall(ctx, companyID, 60); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().AddUsageMinutes(mock.Anything, subscriptionID, mock.Anything, 1).
		Return(models.UsageCounter{}, nil).Once()
	if err := service.AddBusinessUsageMinutes(ctx, companyID, 60); err != nil {
		t.Fatal(err)
	}

	if err := service.CanUseCompany(ctx, uuid.Nil); !errors.Is(err, models.ErrInvalidBillingInput) {
		t.Fatalf("nil company error = %v", err)
	}
	if minutesFromSeconds(0) != 0 || minutesFromSeconds(1) != 1 || minutesFromSeconds(61) != 2 {
		t.Fatal("minutes conversion mismatch")
	}
}

func TestSubscriptionAndPlanMethodsWithMockery(t *testing.T) {
	ctx := context.Background()
	companyID := uuid.New()
	managerID := uuid.New()
	now := time.Now().UTC()
	repo := billingMocks.NewRepository(t)
	companyRepo := billingMocks.NewCompanyRepository(t)
	service := NewService(repo)
	service.now = func() time.Time { return now }
	service.SetCompanyRepository(companyRepo)

	repo.EXPECT().ListPlans(mock.Anything).Return([]models.Plan{{Code: models.PlanCodePersonalStart}}, nil).Once()
	if plans, err := service.ListPlans(ctx); err != nil || len(plans) != 1 {
		t.Fatalf("plans = %+v, %v", plans, err)
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, managerID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleManager, Status: models.MembershipStatusActive}, nil).Twice()
	repo.EXPECT().GetActiveBusinessSubscription(mock.Anything, companyID).
		Return(models.Subscription{ID: uuid.New()}, nil).Once()
	if _, err := service.GetCompanySubscription(ctx, models.GetCompanySubscriptionInput{
		CompanyUUID: companyID, RequestUser: managerID,
	}); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().CancelCompanySubscription(mock.Anything, companyID, now).
		Return(models.Subscription{}, nil).Once()
	if _, err := service.CancelCompanySubscription(ctx, models.CancelCompanySubscriptionInput{
		CompanyUUID: companyID, RequestUser: managerID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CancelCompanySubscription(ctx, models.CancelCompanySubscriptionInput{}); !errors.Is(err, models.ErrInvalidBillingInput) {
		t.Fatalf("invalid cancel error = %v", err)
	}
}

func TestInstructionAndOrganizationLimitsWithMockery(t *testing.T) {
	ctx := context.Background()
	companyID := uuid.New()
	departmentID := uuid.New()
	limit := 5
	subscription := models.Subscription{ID: uuid.New(), Plan: models.Plan{
		InstructionsPerDepartmentLimit: &limit,
		DepartmentsPerCompanyLimit:     &limit,
		MembersPerCompanyLimit:         &limit,
	}}
	repo := billingMocks.NewRepository(t)
	service := NewService(repo)

	repo.EXPECT().GetActiveBusinessSubscription(mock.Anything, companyID).Return(subscription, nil).Times(4)
	repo.EXPECT().CountActiveInstructions(mock.Anything, mock.MatchedBy(func(input models.ListAnalysisInstructionsInput) bool {
		return input.Scope == models.AnalysisInstructionScopeCompany
	})).Return(1, nil).Once()
	if err := service.CanCreateCompanyInstruction(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().CountActiveInstructions(mock.Anything, mock.MatchedBy(func(input models.ListAnalysisInstructionsInput) bool {
		return input.Scope == models.AnalysisInstructionScopeDepartment && input.DepartmentUUID.UUID == departmentID
	})).Return(1, nil).Once()
	if err := service.CanCreateDepartmentInstruction(ctx, companyID, departmentID); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().CountCompanyDepartments(mock.Anything, companyID).Return(1, nil).Once()
	if err := service.CanCreateDepartment(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	repo.EXPECT().CountCompanyMembers(mock.Anything, companyID).Return(limit, nil).Once()
	if err := service.CanAddCompanyMember(ctx, companyID); !errors.Is(err, models.ErrMemberLimitExceeded) {
		t.Fatalf("member limit error = %v", err)
	}
}

func TestFeatureDenialsAndZeroUsageWithMockery(t *testing.T) {
	ctx := context.Background()
	companyID := uuid.New()
	subscription := models.Subscription{ID: uuid.New(), Plan: models.Plan{}}
	repo := billingMocks.NewRepository(t)
	service := NewService(repo)

	repo.EXPECT().GetActiveBusinessSubscription(mock.Anything, companyID).Return(subscription, nil).Times(3)
	if err := service.CanAccessAPI(ctx, companyID); !errors.Is(err, models.ErrAPIAccessDenied) {
		t.Fatalf("API denial = %v", err)
	}
	if err := service.CanExportReports(ctx, companyID); !errors.Is(err, models.ErrExportAccessDenied) {
		t.Fatalf("export denial = %v", err)
	}
	if err := service.CanAccessTeamAnalytics(ctx, companyID); !errors.Is(err, models.ErrTeamAnalyticsAccessDenied) {
		t.Fatalf("analytics denial = %v", err)
	}

	if err := service.canUseMinutes(ctx, subscription, 0); err != nil {
		t.Fatal(err)
	}
	if err := service.addUsageMinutes(ctx, subscription.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetPersonalSubscription(ctx, uuid.Nil); !errors.Is(err, models.ErrInvalidBillingInput) {
		t.Fatalf("invalid personal subscription = %v", err)
	}
	if _, err := service.GetCompanySubscription(ctx, models.GetCompanySubscriptionInput{}); !errors.Is(err, models.ErrInvalidBillingInput) {
		t.Fatalf("invalid company subscription = %v", err)
	}
}

func TestBillingPureHelpers(t *testing.T) {
	if managerPersonalBenefitPlanCode(models.PlanCodeBusinessStart) != models.PlanCodePersonalPlus ||
		managerPersonalBenefitPlanCode(models.PlanCodeBusinessPro) != models.PlanCodePersonalPro ||
		managerPersonalBenefitPlanCode("unknown") != "" {
		t.Fatal("manager benefit mapping mismatch")
	}
	if personalPlanRank(models.PlanCodePersonalStart) != 1 ||
		personalPlanRank(models.PlanCodePersonalPlus) != 2 ||
		personalPlanRank(models.PlanCodePersonalPro) != 3 ||
		personalPlanRank("unknown") != 0 {
		t.Fatal("personal plan rank mismatch")
	}
	if _, err := normalizeSubscriptionError(models.Subscription{}, models.ErrSubscriptionNotFound); !errors.Is(err, models.ErrSubscriptionRequired) {
		t.Fatalf("normalized error = %v", err)
	}
}
