package invitation

import (
	"context"
	"errors"
	"testing"
	"time"

	"calllens/monolit/internal/models"
	repositoryMocks "calllens/monolit/internal/repository/mocks"
	invitationMocks "calllens/monolit/internal/service/invitation/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestListAndResolveTargetWithMockery(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	targetID := uuid.New()
	invitationRepo := repositoryMocks.NewInvitationRepository(t)
	userRepo := repositoryMocks.NewUserRepository(t)
	service := NewService(invitationRepo, userRepo, repositoryMocks.NewCompanyRepository(t), repositoryMocks.NewDepartmentRepository(t), nil)

	invitationRepo.EXPECT().ListUserInvitations(mock.Anything, models.ListUserInvitationsInput{
		UserUUID: userID, Status: models.InvitationStatusPending,
	}).Return([]models.MembershipInvitation{{ID: uuid.New()}}, nil).Once()
	items, err := service.ListUserInvitations(ctx, models.ListUserInvitationsInput{UserUUID: userID})
	if err != nil || len(items) != 1 {
		t.Fatalf("ListUserInvitations = %+v, %v", items, err)
	}
	if _, err := service.ListUserInvitations(ctx, models.ListUserInvitationsInput{
		UserUUID: userID, Status: "bad",
	}); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("invalid status error = %v", err)
	}

	userRepo.EXPECT().GetUserByUsername(mock.Anything, "@target_user").
		Return(models.CurrentUser{ID: targetID}, nil).Once()
	got, err := service.resolveTargetUser(ctx, userID, uuid.Nil, "Target User")
	if err != nil || got != targetID {
		t.Fatalf("resolveTargetUser = %v, %v", got, err)
	}
	if _, err := service.resolveTargetUser(ctx, userID, targetID, "target"); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("ambiguous target error = %v", err)
	}
}

func TestDeclineAndCancelWithMockery(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()
	invitationID := uuid.New()
	invitationRepo := repositoryMocks.NewInvitationRepository(t)
	userRepo := repositoryMocks.NewUserRepository(t)
	companyRepo := repositoryMocks.NewCompanyRepository(t)
	departmentRepo := repositoryMocks.NewDepartmentRepository(t)
	service := NewService(invitationRepo, userRepo, companyRepo, departmentRepo, nil)
	service.SetNow(func() time.Time { return now })

	pending := models.MembershipInvitation{
		ID: invitationID, CompanyUUID: companyID, InvitedUserUUID: userID,
		Status: models.InvitationStatusPending, ExpiresAt: now.Add(time.Hour),
	}
	invitationRepo.EXPECT().GetInvitationByUUID(mock.Anything, invitationID).Return(pending, nil).Once()
	invitationRepo.EXPECT().DeclineInvitation(mock.Anything, invitationID, now).
		Return(pending, nil).Once()
	if _, err := service.DeclineInvitation(ctx, models.DeclineInvitationInput{
		InvitationUUID: invitationID, RequestUser: userID,
	}); err != nil {
		t.Fatal(err)
	}

	invitationRepo.EXPECT().GetInvitationByUUID(mock.Anything, invitationID).Return(pending, nil).Once()
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
	invitationRepo.EXPECT().CancelInvitation(mock.Anything, invitationID, now).Return(pending, nil).Once()
	if _, err := service.CancelInvitation(ctx, models.CancelInvitationInput{
		CompanyUUID: companyID, InvitationUUID: invitationID, RequestUser: userID,
	}); err != nil {
		t.Fatal(err)
	}

	departmentInvitation := pending
	departmentInvitation.DepartmentUUID = uuid.NullUUID{UUID: departmentID, Valid: true}
	departmentInvitation.InvitedByUserUUID = userID
	invitationRepo.EXPECT().GetInvitationByUUID(mock.Anything, invitationID).Return(departmentInvitation, nil).Once()
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()
	departmentRepo.EXPECT().GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{Role: models.DepartmentMemberRoleLeader}, nil).Once()
	invitationRepo.EXPECT().CancelInvitation(mock.Anything, invitationID, now).Return(departmentInvitation, nil).Once()
	if _, err := service.CancelInvitation(ctx, models.CancelInvitationInput{
		CompanyUUID: companyID, DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		InvitationUUID: invitationID, RequestUser: userID,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptBillingAndHelpersWithMockery(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	companyID := uuid.New()
	userID := uuid.New()
	invitationID := uuid.New()
	invitationRepo := repositoryMocks.NewInvitationRepository(t)
	companyRepo := repositoryMocks.NewCompanyRepository(t)
	service := NewService(invitationRepo, repositoryMocks.NewUserRepository(t), companyRepo, repositoryMocks.NewDepartmentRepository(t), nil)
	service.SetNow(func() time.Time { return now })
	billing := invitationMocks.NewBillingLimiter(t)
	service.SetBillingLimiter(billing)

	pending := models.MembershipInvitation{
		ID: invitationID, CompanyUUID: companyID, InvitedUserUUID: userID,
		Status: models.InvitationStatusPending, ExpiresAt: now.Add(time.Hour),
	}
	invitationRepo.EXPECT().GetInvitationByUUID(mock.Anything, invitationID).Return(pending, nil).Once()
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()
	billing.EXPECT().CanAddCompanyMember(mock.Anything, companyID).Return(nil).Once()
	invitationRepo.EXPECT().AcceptInvitation(mock.Anything, invitationID, now).Return(pending, nil).Once()
	if _, err := service.AcceptInvitation(ctx, models.AcceptInvitationInput{
		InvitationUUID: invitationID, RequestUser: userID,
	}); err != nil {
		t.Fatal(err)
	}

	billing.EXPECT().CanUseCompany(mock.Anything, companyID).Return(nil).Once()
	if err := service.requireActiveCompanySubscription(ctx, companyID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []models.InvitationStatus{
		"", models.InvitationStatusPending, models.InvitationStatusAccepted,
		models.InvitationStatusDeclined, models.InvitationStatusCanceled, models.InvitationStatusExpired,
	} {
		if !validInvitationStatus(status) {
			t.Fatalf("status %q should be valid", status)
		}
	}
	if validInvitationStatus("bad") {
		t.Fatal("bad status accepted")
	}
}

func TestInvitationValidationBranchesWithMockery(t *testing.T) {
	ctx := context.Background()
	companyID := uuid.New()
	departmentID := uuid.New()
	userID := uuid.New()
	targetID := uuid.New()
	invitationRepo := repositoryMocks.NewInvitationRepository(t)
	userRepo := repositoryMocks.NewUserRepository(t)
	companyRepo := repositoryMocks.NewCompanyRepository(t)
	departmentRepo := repositoryMocks.NewDepartmentRepository(t)
	service := NewService(invitationRepo, userRepo, companyRepo, departmentRepo, nil)
	originalNow := service.now
	service.SetNow(nil)
	if service.now == nil || originalNow == nil {
		t.Fatal("SetNow(nil) changed clock")
	}

	if err := service.ensureTargetUser(ctx, userID, uuid.Nil); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("invalid target error = %v", err)
	}
	userRepo.EXPECT().GetUserByUUID(mock.Anything, targetID).Return(models.CurrentUser{ID: targetID}, nil).Once()
	if err := service.ensureTargetUser(ctx, userID, targetID); err != nil {
		t.Fatal(err)
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, targetID).
		Return(models.CompanyMember{UserUUID: targetID}, nil).Once()
	active, err := service.isActiveCompanyMember(ctx, companyID, targetID)
	if err != nil || !active {
		t.Fatalf("active company member = %v, %v", active, err)
	}
	departmentRepo.EXPECT().GetDepartmentMember(mock.Anything, companyID, departmentID, targetID).
		Return(models.DepartmentMember{}, models.ErrDepartmentNotFound).Once()
	active, err = service.isActiveDepartmentMember(ctx, companyID, departmentID, targetID)
	if err != nil || active {
		t.Fatalf("active department member = %v, %v", active, err)
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleEmployee}, nil).Once()
	if err := service.requireCompanyManager(ctx, companyID, userID); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("manager permission error = %v", err)
	}

	invitation := models.MembershipInvitation{
		CompanyUUID: companyID, DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
		InvitedByUserUUID: targetID,
	}
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()
	departmentRepo.EXPECT().GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{Role: models.DepartmentMemberRoleEmployee}, nil).Once()
	if err := service.requireDepartmentCancelPermission(ctx, invitation, userID); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("department permission error = %v", err)
	}
}

func TestRespondInvalidInputsWithMockery(t *testing.T) {
	service := NewService(
		repositoryMocks.NewInvitationRepository(t),
		repositoryMocks.NewUserRepository(t),
		repositoryMocks.NewCompanyRepository(t),
		repositoryMocks.NewDepartmentRepository(t),
		nil,
	)
	if _, err := service.AcceptInvitation(context.Background(), models.AcceptInvitationInput{}); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("accept invalid error = %v", err)
	}
	if _, err := service.DeclineInvitation(context.Background(), models.DeclineInvitationInput{}); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("decline invalid error = %v", err)
	}
	if _, err := service.CancelInvitation(context.Background(), models.CancelInvitationInput{}); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("cancel invalid error = %v", err)
	}
}

func TestCreateInvitationsFullPathsWithMockery(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	companyID := uuid.New()
	departmentID := uuid.New()
	managerID := uuid.New()
	targetID := uuid.New()

	t.Run("company by username", func(t *testing.T) {
		invitationRepo := repositoryMocks.NewInvitationRepository(t)
		userRepo := repositoryMocks.NewUserRepository(t)
		companyRepo := repositoryMocks.NewCompanyRepository(t)
		service := NewService(invitationRepo, userRepo, companyRepo, repositoryMocks.NewDepartmentRepository(t), nil)
		service.SetNow(func() time.Time { return now })
		billing := invitationMocks.NewBillingLimiter(t)
		service.SetBillingLimiter(billing)

		userRepo.EXPECT().GetUserByUsername(mock.Anything, "@target_user").
			Return(models.CurrentUser{ID: targetID}, nil).Once()
		companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, managerID).
			Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
		billing.EXPECT().CanUseCompany(mock.Anything, companyID).Return(nil).Once()
		companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, targetID).
			Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()
		invitationRepo.EXPECT().CreateInvitation(mock.Anything, mock.MatchedBy(func(value models.MembershipInvitation) bool {
			return value.CompanyUUID == companyID && value.InvitedUserUUID == targetID &&
				value.Status == models.InvitationStatusPending
		})).Return(models.MembershipInvitation{ID: uuid.New()}, nil).Once()

		if _, err := service.CreateCompanyInvitation(ctx, models.CreateCompanyInvitationInput{
			CompanyUUID: companyID, RequestUser: managerID, Username: "Target User",
			Role: models.CompanyMemberRoleEmployee,
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("department by manager", func(t *testing.T) {
		invitationRepo := repositoryMocks.NewInvitationRepository(t)
		userRepo := repositoryMocks.NewUserRepository(t)
		companyRepo := repositoryMocks.NewCompanyRepository(t)
		departmentRepo := repositoryMocks.NewDepartmentRepository(t)
		service := NewService(invitationRepo, userRepo, companyRepo, departmentRepo, nil)
		billing := invitationMocks.NewBillingLimiter(t)
		service.SetBillingLimiter(billing)

		userRepo.EXPECT().GetUserByUUID(mock.Anything, targetID).Return(models.CurrentUser{ID: targetID}, nil).Once()
		companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, managerID).
			Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
		billing.EXPECT().CanUseCompany(mock.Anything, companyID).Return(nil).Once()
		companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, targetID).
			Return(models.CompanyMember{Role: models.CompanyMemberRoleEmployee}, nil).Once()
		departmentRepo.EXPECT().GetDepartmentMember(mock.Anything, companyID, departmentID, targetID).
			Return(models.DepartmentMember{}, models.ErrDepartmentNotFound).Once()
		invitationRepo.EXPECT().CreateInvitation(mock.Anything, mock.Anything).
			Return(models.MembershipInvitation{ID: uuid.New()}, nil).Once()

		if _, err := service.CreateDepartmentInvitation(ctx, models.CreateDepartmentInvitationInput{
			CompanyUUID: companyID, DepartmentUUID: departmentID, RequestUser: managerID,
			UserUUID: targetID, Role: models.DepartmentMemberRoleLeader,
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRespondConflictBranchesWithMockery(t *testing.T) {
	ctx := context.Background()
	companyID := uuid.New()
	userID := uuid.New()
	otherID := uuid.New()
	invitationID := uuid.New()
	invitationRepo := repositoryMocks.NewInvitationRepository(t)
	service := NewService(
		invitationRepo,
		repositoryMocks.NewUserRepository(t),
		repositoryMocks.NewCompanyRepository(t),
		repositoryMocks.NewDepartmentRepository(t),
		nil,
	)

	invitationRepo.EXPECT().GetInvitationByUUID(mock.Anything, invitationID).Return(models.MembershipInvitation{
		ID: invitationID, CompanyUUID: companyID, InvitedUserUUID: otherID,
		Status: models.InvitationStatusPending,
	}, nil).Once()
	if _, err := service.DeclineInvitation(ctx, models.DeclineInvitationInput{
		InvitationUUID: invitationID, RequestUser: userID,
	}); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("decline forbidden error = %v", err)
	}

	invitationRepo.EXPECT().GetInvitationByUUID(mock.Anything, invitationID).Return(models.MembershipInvitation{
		ID: invitationID, CompanyUUID: companyID, InvitedUserUUID: userID,
		Status: models.InvitationStatusAccepted,
	}, nil).Once()
	if _, err := service.DeclineInvitation(ctx, models.DeclineInvitationInput{
		InvitationUUID: invitationID, RequestUser: userID,
	}); !errors.Is(err, models.ErrInvitationNotPending) {
		t.Fatalf("decline status error = %v", err)
	}

	invitationRepo.EXPECT().GetInvitationByUUID(mock.Anything, invitationID).Return(models.MembershipInvitation{
		ID: invitationID, CompanyUUID: otherID, Status: models.InvitationStatusPending,
	}, nil).Once()
	if _, err := service.CancelInvitation(ctx, models.CancelInvitationInput{
		CompanyUUID: companyID, InvitationUUID: invitationID, RequestUser: userID,
	}); !errors.Is(err, models.ErrInvitationNotFound) {
		t.Fatalf("cancel company mismatch = %v", err)
	}

	invitationRepo.EXPECT().GetInvitationByUUID(mock.Anything, invitationID).Return(models.MembershipInvitation{
		ID: invitationID, CompanyUUID: companyID, Status: models.InvitationStatusAccepted,
	}, nil).Once()
	if _, err := service.CancelInvitation(ctx, models.CancelInvitationInput{
		CompanyUUID: companyID, InvitationUUID: invitationID, RequestUser: userID,
	}); !errors.Is(err, models.ErrInvitationNotPending) {
		t.Fatalf("cancel status error = %v", err)
	}
}

func TestCreateInvalidInputs(t *testing.T) {
	service := NewService(
		repositoryMocks.NewInvitationRepository(t),
		repositoryMocks.NewUserRepository(t),
		repositoryMocks.NewCompanyRepository(t),
		repositoryMocks.NewDepartmentRepository(t),
		nil,
	)
	if _, err := service.CreateCompanyInvitation(context.Background(), models.CreateCompanyInvitationInput{}); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("company input error = %v", err)
	}
	if _, err := service.CreateDepartmentInvitation(context.Background(), models.CreateDepartmentInvitationInput{}); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("department input error = %v", err)
	}
	if _, err := service.resolveTargetUser(context.Background(), uuid.New(), uuid.Nil, "x"); !errors.Is(err, models.ErrInvalidInvitationInput) {
		t.Fatalf("invalid username error = %v", err)
	}
}
