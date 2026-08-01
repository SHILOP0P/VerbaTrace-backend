package analysis_instruction

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"
	repositoryMocks "verbatrace/monolit/internal/repository/mocks"
	instructionMocks "verbatrace/monolit/internal/service/analysis_instruction/mocks"
	storageMocks "verbatrace/monolit/internal/storage/mocks"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

func TestPersonalInstructionLifecycle(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := repositoryMocks.NewAnalysisInstructionRepository(t)
	storage := storageMocks.NewInstructionStorage(t)
	service := NewService(repo, repositoryMocks.NewCompanyRepository(t), repositoryMocks.NewDepartmentRepository(t), storage, nil)

	repo.EXPECT().CountActive(mock.Anything, mock.Anything).Return(0, nil).Once()
	storage.EXPECT().Save(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, input models.SaveInstructionInput) (models.SavedInstructionFile, error) {
			data, _ := io.ReadAll(input.Content)
			return models.SavedInstructionFile{
				Path: "guide.md", MimeType: "text/markdown", SizeBytes: int64(len(data)),
			}, nil
		},
	).Once()
	repo.EXPECT().Create(mock.Anything, mock.Anything).RunAndReturn(
		func(_ context.Context, item models.AnalysisInstruction) (models.AnalysisInstruction, error) {
			return item, nil
		},
	).Once()
	created, err := service.Create(ctx, models.CreateAnalysisInstructionInput{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: userID,
		OriginalFilename: "guide.md", Content: strings.NewReader("guide"), CreatedByUserUUID: userID,
	})
	if err != nil || created.Title != "guide" || !created.UserUUID.Valid {
		t.Fatalf("Create = %+v, %v", created, err)
	}

	repo.EXPECT().List(mock.Anything, mock.Anything).Return([]models.AnalysisInstruction{created}, nil).Once()
	items, err := service.List(ctx, models.ListAnalysisInstructionsInput{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: userID,
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("List = %+v, %v", items, err)
	}

	repo.EXPECT().GetByUUID(mock.Anything, created.ID).Return(created, nil).Once()
	storage.EXPECT().Open(mock.Anything, "guide.md").
		Return(io.NopCloser(strings.NewReader("guide")), nil).Once()
	file, err := service.GetFile(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	data, _ := io.ReadAll(file.Content)
	_ = file.Content.Close()
	if string(data) != "guide" {
		t.Fatalf("content = %q", data)
	}

	repo.EXPECT().GetByUUID(mock.Anything, created.ID).Return(created, nil).Once()
	repo.EXPECT().Deactivate(mock.Anything, created.ID).Return(nil).Once()
	if err := service.Delete(ctx, created.ID, userID); err != nil {
		t.Fatalf("Delete error=%v", err)
	}
}

func TestValidationAndLimits(t *testing.T) {
	userID := uuid.New()
	valid := models.CreateAnalysisInstructionInput{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: userID,
		OriginalFilename: "guide.md", Content: strings.NewReader("guide"), CreatedByUserUUID: userID,
	}
	if err := validateCreateInput(valid); err != nil {
		t.Fatal(err)
	}
	for _, input := range []models.CreateAnalysisInstructionInput{
		{},
		{CreatedByUserUUID: userID, Content: strings.NewReader("x")},
		{CreatedByUserUUID: userID, Content: strings.NewReader("x"), OriginalFilename: "x.txt"},
		{CreatedByUserUUID: userID, Content: strings.NewReader("x"), OriginalFilename: "x.md", Scope: "bad"},
	} {
		if validateCreateInput(input) == nil {
			t.Fatalf("invalid create input accepted: %+v", input)
		}
	}

	if instructionLimit(models.AnalysisInstructionScopePersonal) != models.DefaultPersonalInstructionLimit ||
		instructionLimit(models.AnalysisInstructionScopeCompany) != models.CompanyInstructionLimit ||
		instructionLimit(models.AnalysisInstructionScopeDepartment) != models.DepartmentInstructionLimit ||
		instructionLimit("bad") != 0 {
		t.Fatal("instruction limits mismatch")
	}
	if _, err := (&Service{}).ownerFilter(models.CreateAnalysisInstructionInput{Scope: "bad"}); err == nil {
		t.Fatal("expected owner filter error")
	}
	if ownerFilterToUser(valid).UUID != userID || ownerFilterToUser(models.CreateAnalysisInstructionInput{Scope: models.AnalysisInstructionScopeCompany}).Valid {
		t.Fatal("ownerFilterToUser mismatch")
	}

	for _, input := range []models.ListAnalysisInstructionsInput{
		{},
		{UserUUID: userID, Scope: "bad"},
		{UserUUID: userID, Scope: models.AnalysisInstructionScopeCompany},
		{UserUUID: userID, Scope: models.AnalysisInstructionScopeDepartment},
	} {
		if validateListInput(input) == nil {
			t.Fatalf("invalid list input accepted: %+v", input)
		}
	}
}

func TestAuthorizationPaths(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	companyRepo := repositoryMocks.NewCompanyRepository(t)
	departmentRepo := repositoryMocks.NewDepartmentRepository(t)
	service := NewService(
		repositoryMocks.NewAnalysisInstructionRepository(t),
		companyRepo,
		departmentRepo,
		storageMocks.NewInstructionStorage(t),
		nil,
	)

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
	if err := service.authorizeCreate(ctx, models.CreateAnalysisInstructionInput{
		Scope: models.AnalysisInstructionScopeCompany, CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true}, CreatedByUserUUID: userID,
	}); err != nil {
		t.Fatal(err)
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()
	departmentRepo.EXPECT().GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{Role: models.DepartmentMemberRoleLeader}, nil).Once()
	if err := service.authorizeDepartmentManage(ctx, companyID, departmentID, userID); err != nil {
		t.Fatal(err)
	}

	if err := service.authorizeDelete(ctx, models.AnalysisInstruction{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}, userID); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("delete authorization error = %v", err)
	}
}

func TestListReadDeleteAuthorizationWithMockery(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	repo := repositoryMocks.NewAnalysisInstructionRepository(t)
	companyRepo := repositoryMocks.NewCompanyRepository(t)
	departmentRepo := repositoryMocks.NewDepartmentRepository(t)
	storage := storageMocks.NewInstructionStorage(t)
	service := NewService(repo, companyRepo, departmentRepo, storage, nil)

	companyInput := models.ListAnalysisInstructionsInput{
		Scope: models.AnalysisInstructionScopeCompany, UserUUID: userID,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
	}
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleEmployee}, nil).Once()
	departmentRepo.EXPECT().ListVisibleCompanyDepartments(mock.Anything, companyID, userID).
		Return([]models.Department{{ID: departmentID, CompanyUUID: companyID}}, nil).Once()
	repo.EXPECT().List(mock.Anything, companyInput).Return(nil, nil).Once()
	if _, err := service.List(ctx, companyInput); err != nil {
		t.Fatal(err)
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleEmployee}, nil).Once()
	departmentRepo.EXPECT().ListVisibleCompanyDepartments(mock.Anything, companyID, userID).
		Return(nil, nil).Once()
	if _, err := service.List(ctx, companyInput); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("unassigned company member list authorization error = %v", err)
	}

	departmentInput := models.ListAnalysisInstructionsInput{
		Scope: models.AnalysisInstructionScopeDepartment, UserUUID: userID,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
	}
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()
	departmentRepo.EXPECT().GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{}, nil).Once()
	repo.EXPECT().List(mock.Anything, departmentInput).Return(nil, nil).Once()
	if _, err := service.List(ctx, departmentInput); err != nil {
		t.Fatal(err)
	}

	companyInstruction := models.AnalysisInstruction{
		ID: uuid.New(), Scope: models.AnalysisInstructionScopeCompany,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true}, FilePath: "company.md",
	}
	repo.EXPECT().GetByUUID(mock.Anything, companyInstruction.ID).Return(companyInstruction, nil).Times(3)
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleEmployee}, nil).Once()
	departmentRepo.EXPECT().ListVisibleCompanyDepartments(mock.Anything, companyID, userID).
		Return([]models.Department{{ID: departmentID, CompanyUUID: companyID}}, nil).Once()
	storage.EXPECT().Open(mock.Anything, "company.md").
		Return(io.NopCloser(strings.NewReader("company")), nil).Once()
	file, err := service.GetFile(ctx, companyInstruction.ID, userID)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Content.Close()
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleEmployee}, nil).Once()
	departmentRepo.EXPECT().ListVisibleCompanyDepartments(mock.Anything, companyID, userID).
		Return(nil, nil).Once()
	if _, err := service.GetFile(ctx, companyInstruction.ID, userID); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("unassigned company member read authorization error = %v", err)
	}
	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
	repo.EXPECT().Deactivate(mock.Anything, companyInstruction.ID).Return(nil).Once()
	if err := service.Delete(ctx, companyInstruction.ID, userID); err != nil {
		t.Fatal(err)
	}
}

func TestBillingLimitScopesWithMockery(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	repo := repositoryMocks.NewAnalysisInstructionRepository(t)
	service := NewService(
		repo,
		repositoryMocks.NewCompanyRepository(t),
		repositoryMocks.NewDepartmentRepository(t),
		storageMocks.NewInstructionStorage(t),
		nil,
	)
	billing := instructionMocks.NewBillingLimiter(t)
	service.SetBillingLimiter(billing)

	tests := []struct {
		input models.CreateAnalysisInstructionInput
		setup func()
	}{
		{
			input: models.CreateAnalysisInstructionInput{
				Scope: models.AnalysisInstructionScopePersonal, UserUUID: userID,
			},
			setup: func() { billing.EXPECT().CanCreatePersonalInstruction(mock.Anything, userID).Return(nil).Once() },
		},
		{
			input: models.CreateAnalysisInstructionInput{
				Scope:       models.AnalysisInstructionScopeCompany,
				CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true},
			},
			setup: func() { billing.EXPECT().CanCreateCompanyInstruction(mock.Anything, companyID).Return(nil).Once() },
		},
		{
			input: models.CreateAnalysisInstructionInput{
				Scope:          models.AnalysisInstructionScopeDepartment,
				CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
				DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
			},
			setup: func() {
				billing.EXPECT().CanCreateDepartmentInstruction(mock.Anything, companyID, departmentID).Return(nil).Once()
			},
		},
	}
	for _, tt := range tests {
		tt.setup()
		repo.EXPECT().CountActive(mock.Anything, mock.Anything).Return(1, nil).Once()
		if count, err := service.checkBillingLimit(ctx, tt.input, models.ListAnalysisInstructionsInput{}); err != nil || count != 1 {
			t.Fatalf("checkBillingLimit = %d, %v", count, err)
		}
	}
}

func TestServiceInputValidation(t *testing.T) {
	service := NewService(
		repositoryMocks.NewAnalysisInstructionRepository(t),
		repositoryMocks.NewCompanyRepository(t),
		repositoryMocks.NewDepartmentRepository(t),
		storageMocks.NewInstructionStorage(t),
		nil,
	)
	if err := service.Delete(context.Background(), uuid.Nil, uuid.New()); !errors.Is(err, models.ErrInvalidAnalysisInstructionInput) {
		t.Fatalf("delete validation = %v", err)
	}
	if _, err := service.GetFile(context.Background(), uuid.Nil, uuid.New()); !errors.Is(err, models.ErrInvalidAnalysisInstructionInput) {
		t.Fatalf("get file validation = %v", err)
	}
	if _, err := service.List(context.Background(), models.ListAnalysisInstructionsInput{}); !errors.Is(err, models.ErrInvalidAnalysisInstructionInput) {
		t.Fatalf("list validation = %v", err)
	}
}

func TestCreateCompanyAndDepartmentInstructionsWithMockery(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()

	tests := []models.CreateAnalysisInstructionInput{
		{
			Scope:            models.AnalysisInstructionScopeCompany,
			CompanyUUID:      uuid.NullUUID{UUID: companyID, Valid: true},
			OriginalFilename: "company.md", Content: strings.NewReader("company"),
			CreatedByUserUUID: userID,
		},
		{
			Scope:            models.AnalysisInstructionScopeDepartment,
			CompanyUUID:      uuid.NullUUID{UUID: companyID, Valid: true},
			DepartmentUUID:   uuid.NullUUID{UUID: departmentID, Valid: true},
			OriginalFilename: "department.md", Content: strings.NewReader("department"),
			CreatedByUserUUID: userID,
		},
	}

	for _, input := range tests {
		repo := repositoryMocks.NewAnalysisInstructionRepository(t)
		companyRepo := repositoryMocks.NewCompanyRepository(t)
		departmentRepo := repositoryMocks.NewDepartmentRepository(t)
		storage := storageMocks.NewInstructionStorage(t)
		service := NewService(repo, companyRepo, departmentRepo, storage, nil)

		if input.Scope == models.AnalysisInstructionScopeCompany {
			companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
				Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
		} else {
			companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
				Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
		}
		repo.EXPECT().CountActive(mock.Anything, mock.Anything).Return(0, nil).Once()
		storage.EXPECT().Save(mock.Anything, mock.Anything).Return(models.SavedInstructionFile{
			Path: input.OriginalFilename, MimeType: "text/markdown", SizeBytes: 10,
		}, nil).Once()
		repo.EXPECT().Create(mock.Anything, mock.Anything).RunAndReturn(
			func(_ context.Context, item models.AnalysisInstruction) (models.AnalysisInstruction, error) {
				return item, nil
			},
		).Once()

		if _, err := service.Create(ctx, input); err != nil {
			t.Fatalf("Create scope %q: %v", input.Scope, err)
		}
	}
}

func TestPermissionErrorsAndFallbackLimitWithMockery(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	repo := repositoryMocks.NewAnalysisInstructionRepository(t)
	companyRepo := repositoryMocks.NewCompanyRepository(t)
	departmentRepo := repositoryMocks.NewDepartmentRepository(t)
	service := NewService(repo, companyRepo, departmentRepo, storageMocks.NewInstructionStorage(t), nil)

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleEmployee}, nil).Once()
	if err := service.authorizeCreate(ctx, models.CreateAnalysisInstructionInput{
		Scope:       models.AnalysisInstructionScopeCompany,
		CompanyUUID: uuid.NullUUID{UUID: companyID, Valid: true}, CreatedByUserUUID: userID,
	}); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("company create permission = %v", err)
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()
	departmentRepo.EXPECT().GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{Role: models.DepartmentMemberRoleEmployee}, nil).Once()
	if err := service.authorizeDepartmentManage(ctx, companyID, departmentID, userID); !errors.Is(err, models.ErrForbidden) {
		t.Fatalf("department manage permission = %v", err)
	}

	repo.EXPECT().CountActive(mock.Anything, mock.Anything).
		Return(models.DefaultPersonalInstructionLimit, nil).Once()
	if _, err := service.checkBillingLimit(ctx, models.CreateAnalysisInstructionInput{
		Scope: models.AnalysisInstructionScopePersonal,
	}, models.ListAnalysisInstructionsInput{}); !errors.Is(err, models.ErrInstructionLimitExceeded) {
		t.Fatalf("fallback limit error = %v", err)
	}
}

func TestDepartmentReadAndDeletePermissionsWithMockery(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	companyID := uuid.New()
	departmentID := uuid.New()
	companyRepo := repositoryMocks.NewCompanyRepository(t)
	departmentRepo := repositoryMocks.NewDepartmentRepository(t)
	service := NewService(
		repositoryMocks.NewAnalysisInstructionRepository(t),
		companyRepo,
		departmentRepo,
		storageMocks.NewInstructionStorage(t),
		nil,
	)
	instruction := models.AnalysisInstruction{
		Scope:          models.AnalysisInstructionScopeDepartment,
		CompanyUUID:    uuid.NullUUID{UUID: companyID, Valid: true},
		DepartmentUUID: uuid.NullUUID{UUID: departmentID, Valid: true},
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
	if err := service.authorizeRead(ctx, instruction, userID); err != nil {
		t.Fatal(err)
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{}, models.ErrCompanyNotFound).Once()
	departmentRepo.EXPECT().GetDepartmentMember(mock.Anything, companyID, departmentID, userID).
		Return(models.DepartmentMember{Role: models.DepartmentMemberRoleEmployee}, nil).Once()
	if err := service.authorizeRead(ctx, instruction, userID); err != nil {
		t.Fatal(err)
	}

	companyRepo.EXPECT().GetCompanyMember(mock.Anything, companyID, userID).
		Return(models.CompanyMember{Role: models.CompanyMemberRoleManager}, nil).Once()
	if err := service.authorizeDelete(ctx, instruction, userID); err != nil {
		t.Fatal(err)
	}

	if err := service.authorizeRead(ctx, models.AnalysisInstruction{Scope: "bad"}, userID); !errors.Is(err, models.ErrInvalidAnalysisInstructionInput) {
		t.Fatalf("invalid read scope = %v", err)
	}
	if err := service.authorizeDelete(ctx, models.AnalysisInstruction{Scope: "bad"}, userID); !errors.Is(err, models.ErrInvalidAnalysisInstructionInput) {
		t.Fatalf("invalid delete scope = %v", err)
	}
}

func TestCreateRepositoryFailureDeletesSavedFile(t *testing.T) {
	userID := uuid.New()
	repo := repositoryMocks.NewAnalysisInstructionRepository(t)
	storage := storageMocks.NewInstructionStorage(t)
	service := NewService(
		repo,
		repositoryMocks.NewCompanyRepository(t),
		repositoryMocks.NewDepartmentRepository(t),
		storage,
		nil,
	)
	repo.EXPECT().CountActive(mock.Anything, mock.Anything).Return(0, nil).Once()
	storage.EXPECT().Save(mock.Anything, mock.Anything).Return(models.SavedInstructionFile{
		Path: "guide.md", MimeType: "text/markdown",
	}, nil).Once()
	repo.EXPECT().Create(mock.Anything, mock.Anything).Return(models.AnalysisInstruction{}, errors.New("db")).Once()
	storage.EXPECT().Delete(mock.Anything, "guide.md").Return(nil).Once()
	if _, err := service.Create(context.Background(), models.CreateAnalysisInstructionInput{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: userID,
		OriginalFilename: "guide.md", Content: strings.NewReader("guide"), CreatedByUserUUID: userID,
	}); err == nil {
		t.Fatal("expected repository error")
	}
}

func TestUpdateReplaceAndReorderManagement(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	instructionID := uuid.New()
	repo := repositoryMocks.NewAnalysisInstructionRepository(t)
	storage := storageMocks.NewInstructionStorage(t)
	service := NewService(
		repo,
		repositoryMocks.NewCompanyRepository(t),
		repositoryMocks.NewDepartmentRepository(t),
		storage,
		nil,
	)

	inactive := models.AnalysisInstruction{
		ID: instructionID, Scope: models.AnalysisInstructionScopePersonal,
		UserUUID: uuid.NullUUID{UUID: userID, Valid: true}, IsActive: false,
		OriginalFilename: "old.md", FilePath: "old.md",
	}
	active := true
	title := "Updated title"
	sortOrder := 7
	repo.On("GetByUUIDIncludingInactive", mock.Anything, instructionID).Return(inactive, nil).Once()
	repo.EXPECT().CountActive(mock.Anything, mock.MatchedBy(func(input models.ListAnalysisInstructionsInput) bool {
		return input.Scope == models.AnalysisInstructionScopePersonal && input.UserUUID == userID
	})).Return(0, nil).Once()
	repo.On("Update", mock.Anything, mock.MatchedBy(func(input models.UpdateAnalysisInstructionRepositoryInput) bool {
		return input.ID == instructionID && input.Title != nil && *input.Title == title &&
			input.IsActive != nil && *input.IsActive && input.SortOrder != nil && *input.SortOrder == sortOrder
	})).Return(models.AnalysisInstruction{ID: instructionID, Title: title, IsActive: true, SortOrder: sortOrder}, nil).Once()
	updated, err := service.Update(ctx, models.UpdateAnalysisInstructionInput{
		ID: instructionID, UserUUID: userID, Title: &title, IsActive: &active, SortOrder: &sortOrder,
	})
	if err != nil || updated.Title != title || !updated.IsActive {
		t.Fatalf("Update = %+v, %v", updated, err)
	}

	repo.On("GetByUUIDIncludingInactive", mock.Anything, instructionID).Return(inactive, nil).Once()
	storage.EXPECT().Save(mock.Anything, mock.MatchedBy(func(input models.SaveInstructionInput) bool {
		return input.InstructionUUID == instructionID && input.OriginalFilename == "new.md"
	})).Return(models.SavedInstructionFile{
		Path: "new.md", MimeType: "text/plain", SizeBytes: 11, ContentSHA256: "hash",
	}, nil).Once()
	repo.On("Update", mock.Anything, mock.MatchedBy(func(input models.UpdateAnalysisInstructionRepositoryInput) bool {
		return input.OriginalFilename != nil && *input.OriginalFilename == "new.md" &&
			input.FilePath != nil && *input.FilePath == "new.md" &&
			input.SizeBytes != nil && *input.SizeBytes == 11 &&
			input.ContentSHA256 != nil && *input.ContentSHA256 == "hash"
	})).Return(models.AnalysisInstruction{ID: instructionID, OriginalFilename: "new.md", SizeBytes: 11, ContentSHA256: "hash"}, nil).Once()
	replaced, err := service.ReplaceFile(ctx, models.ReplaceAnalysisInstructionFileInput{
		ID: instructionID, UserUUID: userID, OriginalFilename: "new.md", MimeType: "text/plain", Content: strings.NewReader("hello"),
	})
	if err != nil || replaced.OriginalFilename != "new.md" || replaced.ContentSHA256 != "hash" {
		t.Fatalf("ReplaceFile = %+v, %v", replaced, err)
	}

	repo.On("GetByUUIDIncludingInactive", mock.Anything, instructionID).Return(inactive, nil).Once()
	repo.On("Reorder", mock.Anything, []models.ReorderAnalysisInstructionItem{{ID: instructionID, SortOrder: 20}}).Return(nil).Once()
	if err := service.Reorder(ctx, models.ReorderAnalysisInstructionsInput{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: userID,
		Items: []models.ReorderAnalysisInstructionItem{{ID: instructionID, SortOrder: 20}},
	}); err != nil {
		t.Fatalf("Reorder: %v", err)
	}

	other := inactive
	other.UserUUID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	repo.On("GetByUUIDIncludingInactive", mock.Anything, instructionID).Return(other, nil).Once()
	if err := service.Reorder(ctx, models.ReorderAnalysisInstructionsInput{
		Scope: models.AnalysisInstructionScopePersonal, UserUUID: userID,
		Items: []models.ReorderAnalysisInstructionItem{{ID: instructionID, SortOrder: 30}},
	}); !errors.Is(err, models.ErrInvalidAnalysisInstructionInput) {
		t.Fatalf("cross-scope reorder error = %v", err)
	}
}
