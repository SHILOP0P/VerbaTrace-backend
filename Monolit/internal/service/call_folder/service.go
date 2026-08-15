package call_folder

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type Service struct {
	repository            repository.CallFolderRepository
	callRepository        callRepository
	companyRepository     companyRepository
	departmentRepository  departmentRepository
	instructionRepository instructionRepository
	instructionLinks      instructionLinks
}

type callRepository interface {
	GetByUUID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.Call, error)
}

type companyRepository interface {
	GetCompanyMember(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) (models.CompanyMember, error)
}

type departmentRepository interface {
	GetDepartmentMember(ctx context.Context, companyID uuid.UUID, departmentID uuid.UUID, userID uuid.UUID) (models.DepartmentMember, error)
	ListVisibleCompanyDepartments(ctx context.Context, companyID uuid.UUID, userID uuid.UUID) ([]models.Department, error)
}

type instructionRepository interface {
	GetByUUID(context.Context, uuid.UUID) (models.AnalysisInstruction, error)
}

type instructionLinks interface {
	ReplaceInstructions(context.Context, uuid.UUID, []uuid.UUID, uuid.UUID) error
	ListInstructions(context.Context, uuid.UUID) ([]models.AnalysisInstruction, error)
}

func NewService(folderRepository repository.CallFolderRepository, callRepository callRepository, companyRepository companyRepository, departmentRepository departmentRepository) *Service {
	return &Service{
		repository:           folderRepository,
		callRepository:       callRepository,
		companyRepository:    companyRepository,
		departmentRepository: departmentRepository,
	}
}

func (s *Service) SetInstructionRepositories(instructions instructionRepository, links instructionLinks) {
	s.instructionRepository = instructions
	s.instructionLinks = links
}

func (s *Service) Create(ctx context.Context, input models.CreateCallFolderInput) (models.CallFolder, error) {
	if err := normalizeCreateInput(&input); err != nil {
		return models.CallFolder{}, err
	}
	if err := s.authorizeManage(ctx, input.Scope, input.UserID, input.CompanyUUID, input.DepartmentUUID); err != nil {
		return models.CallFolder{}, err
	}

	folder := models.CallFolder{
		ID:                uuid.New(),
		Scope:             input.Scope,
		CompanyUUID:       input.CompanyUUID,
		DepartmentUUID:    input.DepartmentUUID,
		Name:              input.Name,
		Description:       input.Description,
		Color:             input.Color,
		CreatedByUserUUID: input.UserID,
	}
	if input.Scope == models.CallFolderScopePersonal {
		folder.UserUUID = uuid.NullUUID{UUID: input.UserID, Valid: true}
	}
	created, err := s.repository.Create(ctx, folder)
	if err != nil {
		return models.CallFolder{}, err
	}
	if len(input.InstructionIDs) > 0 {
		if err = s.ReplaceInstructions(ctx, input.UserID, created.ID, input.InstructionIDs); err != nil {
			return models.CallFolder{}, err
		}
		created.Instructions, err = s.instructionLinks.ListInstructions(ctx, created.ID)
	}
	return created, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID, userID uuid.UUID) (models.CallFolder, error) {
	folder, err := s.repository.GetByUUID(ctx, id)
	if err != nil {
		return models.CallFolder{}, err
	}
	if err := s.authorizeRead(ctx, folder, userID); err != nil {
		return models.CallFolder{}, maskForbiddenAsNotFound(err)
	}
	if s.instructionLinks != nil {
		folder.Instructions, err = s.instructionLinks.ListInstructions(ctx, folder.ID)
		if err != nil {
			return models.CallFolder{}, err
		}
	}
	return folder, nil
}

func (s *Service) List(ctx context.Context, input models.ListCallFoldersInput) (models.ListCallFoldersResult, error) {
	if err := normalizeListInput(&input); err != nil {
		return models.ListCallFoldersResult{}, err
	}
	result, err := s.repository.List(ctx, input)
	if err != nil {
		return models.ListCallFoldersResult{}, err
	}
	if s.instructionLinks != nil {
		for i := range result.Items {
			result.Items[i].Instructions, err = s.instructionLinks.ListInstructions(ctx, result.Items[i].ID)
			if err != nil {
				return models.ListCallFoldersResult{}, err
			}
		}
	}
	return result, nil
}

func (s *Service) Update(ctx context.Context, input models.UpdateCallFolderInput) (models.CallFolder, error) {
	folder, err := s.repository.GetByUUID(ctx, input.FolderUUID)
	if err != nil {
		return models.CallFolder{}, err
	}
	if err := s.authorizeManageFolder(ctx, folder, input.UserID); err != nil {
		return models.CallFolder{}, err
	}
	if err := normalizeUpdateInput(&input); err != nil {
		return models.CallFolder{}, err
	}
	updated, err := s.repository.Update(ctx, input)
	if err != nil {
		return models.CallFolder{}, err
	}
	if s.instructionLinks != nil {
		updated.Instructions, err = s.instructionLinks.ListInstructions(ctx, updated.ID)
	}
	return updated, err
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	folder, err := s.repository.GetByUUID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.authorizeManageFolder(ctx, folder, userID); err != nil {
		return err
	}
	return s.repository.SoftDelete(ctx, id)
}

func (s *Service) AssignCall(ctx context.Context, input models.AssignCallToFolderInput) error {
	folder, err := s.repository.GetByUUID(ctx, input.FolderUUID)
	if err != nil {
		return err
	}
	if err := s.authorizeManageFolder(ctx, folder, input.UserID); err != nil {
		return err
	}
	call, err := s.callRepository.GetByUUID(ctx, input.CallUUID, input.UserID)
	if err != nil {
		return err
	}
	if !callMatchesFolder(call, folder) {
		return models.ErrCallFolderScopeMismatch
	}
	return s.repository.AssignCall(ctx, input)
}

func (s *Service) RemoveCall(ctx context.Context, input models.RemoveCallFromFolderInput) error {
	folder, err := s.repository.GetByUUID(ctx, input.FolderUUID)
	if err != nil {
		return err
	}
	if err := s.authorizeManageFolder(ctx, folder, input.UserID); err != nil {
		return err
	}
	return s.repository.RemoveCall(ctx, input)
}

func (s *Service) ListFolderCalls(ctx context.Context, input models.ListFolderCallsInput) (models.ListCallsResult, error) {
	folder, err := s.repository.GetByUUID(ctx, input.FolderUUID)
	if err != nil {
		return models.ListCallsResult{}, err
	}
	if err := s.authorizeRead(ctx, folder, input.UserID); err != nil {
		return models.ListCallsResult{}, maskForbiddenAsNotFound(err)
	}
	if input.Limit <= 0 {
		input.Limit = defaultListLimit
	}
	if input.Limit > maxListLimit || input.Offset < 0 {
		return models.ListCallsResult{}, models.ErrInvalidCallFolderInput
	}
	return s.repository.ListFolderCalls(ctx, input)
}

func (s *Service) GrantAccess(ctx context.Context, input models.GrantCallFolderAccessInput) (models.CallFolderAccess, error) {
	folder, err := s.repository.GetByUUID(ctx, input.FolderUUID)
	if err != nil {
		return models.CallFolderAccess{}, err
	}
	if err := s.authorizeManageAccess(ctx, folder, input.UserID, input.TargetUserUUID); err != nil {
		return models.CallFolderAccess{}, err
	}
	return s.repository.GrantAccess(ctx, input)
}

func (s *Service) RevokeAccess(ctx context.Context, input models.RevokeCallFolderAccessInput) error {
	folder, err := s.repository.GetByUUID(ctx, input.FolderUUID)
	if err != nil {
		return err
	}
	if err := s.authorizeManageAccess(ctx, folder, input.UserID, input.TargetUserUUID); err != nil {
		return err
	}
	return s.repository.RevokeAccess(ctx, input.FolderUUID, input.TargetUserUUID)
}

func (s *Service) ListAccesses(ctx context.Context, folderID uuid.UUID, userID uuid.UUID) ([]models.CallFolderAccess, error) {
	folder, err := s.repository.GetByUUID(ctx, folderID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeManageFolder(ctx, folder, userID); err != nil {
		return nil, err
	}
	return s.repository.ListAccesses(ctx, folderID)
}

func (s *Service) ReplaceInstructions(ctx context.Context, userID uuid.UUID, folderID uuid.UUID, instructionIDs []uuid.UUID) error {
	if s.instructionRepository == nil || s.instructionLinks == nil {
		return models.ErrInvalidCallFolderInput
	}
	folder, err := s.repository.GetByUUID(ctx, folderID)
	if err != nil {
		return err
	}
	if err = s.authorizeManageFolder(ctx, folder, userID); err != nil {
		return err
	}
	seen := make(map[uuid.UUID]struct{}, len(instructionIDs))
	for _, instructionID := range instructionIDs {
		if instructionID == uuid.Nil {
			return models.ErrInvalidCallFolderInput
		}
		if _, exists := seen[instructionID]; exists {
			continue
		}
		seen[instructionID] = struct{}{}
		instruction, getErr := s.instructionRepository.GetByUUID(ctx, instructionID)
		if getErr != nil || !instruction.IsActive || !instructionMatchesFolder(instruction, folder) {
			return models.ErrInvalidCallFolderInput
		}
	}
	unique := make([]uuid.UUID, 0, len(seen))
	for _, instructionID := range instructionIDs {
		if _, exists := seen[instructionID]; exists {
			unique = append(unique, instructionID)
			delete(seen, instructionID)
		}
	}
	return s.instructionLinks.ReplaceInstructions(ctx, folderID, unique, userID)
}

func instructionMatchesFolder(instruction models.AnalysisInstruction, folder models.CallFolder) bool {
	switch folder.Scope {
	case models.CallFolderScopePersonal:
		return instruction.Scope == models.AnalysisInstructionScopePersonal &&
			instruction.UserUUID.Valid && folder.UserUUID.Valid &&
			instruction.UserUUID.UUID == folder.UserUUID.UUID
	case models.CallFolderScopeCompany:
		return instruction.Scope == models.AnalysisInstructionScopeCompany &&
			instruction.CompanyUUID.Valid && folder.CompanyUUID.Valid &&
			instruction.CompanyUUID.UUID == folder.CompanyUUID.UUID
	case models.CallFolderScopeDepartment:
		if !instruction.CompanyUUID.Valid || !folder.CompanyUUID.Valid || instruction.CompanyUUID.UUID != folder.CompanyUUID.UUID {
			return false
		}
		return instruction.Scope == models.AnalysisInstructionScopeCompany ||
			(instruction.Scope == models.AnalysisInstructionScopeDepartment &&
				instruction.DepartmentUUID.Valid && folder.DepartmentUUID.Valid &&
				instruction.DepartmentUUID.UUID == folder.DepartmentUUID.UUID)
	default:
		return false
	}
}

func (s *Service) authorizeManageFolder(ctx context.Context, folder models.CallFolder, userID uuid.UUID) error {
	if folder.Scope == models.CallFolderScopePersonal {
		if folder.UserUUID.Valid && folder.UserUUID.UUID == userID {
			return nil
		}
		return models.ErrForbidden
	}
	return s.authorizeManage(ctx, folder.Scope, userID, folder.CompanyUUID, folder.DepartmentUUID)
}

func (s *Service) authorizeManageAccess(ctx context.Context, folder models.CallFolder, userID uuid.UUID, targetUserID uuid.UUID) error {
	if targetUserID == uuid.Nil || targetUserID == userID || folder.Scope == models.CallFolderScopePersonal {
		return models.ErrForbidden
	}
	if err := s.authorizeManageFolder(ctx, folder, userID); err != nil {
		return err
	}
	companyMember, err := s.companyRepository.GetCompanyMember(ctx, folder.CompanyUUID.UUID, targetUserID)
	if err != nil || companyMember.Status != models.MembershipStatusActive {
		return models.ErrForbidden
	}
	if folder.Scope == models.CallFolderScopeCompany {
		return nil
	}
	departmentMember, err := s.departmentRepository.GetDepartmentMember(ctx, folder.CompanyUUID.UUID, folder.DepartmentUUID.UUID, targetUserID)
	if err != nil || departmentMember.Status != models.MembershipStatusActive || departmentMember.Role != models.DepartmentMemberRoleEmployee {
		// A department leader has access to their department by role. The manager cannot alter it.
		return models.ErrForbidden
	}
	return nil
}

func (s *Service) authorizeManage(ctx context.Context, scope models.CallFolderScope, userID uuid.UUID, companyID uuid.NullUUID, departmentID uuid.NullUUID) error {
	switch scope {
	case models.CallFolderScopePersonal:
		return nil
	case models.CallFolderScopeCompany:
		member, err := s.companyRepository.GetCompanyMember(ctx, companyID.UUID, userID)
		if err != nil {
			return models.ErrForbidden
		}
		if member.Role != models.CompanyMemberRoleManager {
			return models.ErrForbidden
		}
		return nil
	case models.CallFolderScopeDepartment:
		if member, err := s.companyRepository.GetCompanyMember(ctx, companyID.UUID, userID); err == nil && member.Role == models.CompanyMemberRoleManager {
			return nil
		}
		member, err := s.departmentRepository.GetDepartmentMember(ctx, companyID.UUID, departmentID.UUID, userID)
		if err != nil {
			return models.ErrForbidden
		}
		if member.Role != models.DepartmentMemberRoleLeader {
			return models.ErrForbidden
		}
		return nil
	default:
		return models.ErrInvalidCallFolderInput
	}
}

func (s *Service) authorizeRead(ctx context.Context, folder models.CallFolder, userID uuid.UUID) error {
	if _, err := s.repository.GetVisibleByUUID(ctx, folder.ID, userID); err != nil {
		return models.ErrForbidden
	}
	return nil
}

func callMatchesFolder(call models.Call, folder models.CallFolder) bool {
	switch folder.Scope {
	case models.CallFolderScopePersonal:
		return call.VisibilityScope == models.CallVisibilityScopePersonal &&
			call.UploadedByUserUUID.Valid &&
			folder.UserUUID.Valid &&
			call.UploadedByUserUUID.UUID == folder.UserUUID.UUID &&
			!call.CompanyUUID.Valid &&
			!call.DepartmentUUID.Valid
	case models.CallFolderScopeCompany:
		return call.CompanyUUID.Valid && folder.CompanyUUID.Valid && call.CompanyUUID.UUID == folder.CompanyUUID.UUID
	case models.CallFolderScopeDepartment:
		return call.CompanyUUID.Valid && call.DepartmentUUID.Valid &&
			folder.CompanyUUID.Valid && folder.DepartmentUUID.Valid &&
			call.CompanyUUID.UUID == folder.CompanyUUID.UUID &&
			call.DepartmentUUID.UUID == folder.DepartmentUUID.UUID
	default:
		return false
	}
}

func normalizeCreateInput(input *models.CreateCallFolderInput) error {
	if !validScopePlacement(input.Scope, input.CompanyUUID, input.DepartmentUUID) {
		return models.ErrInvalidCallFolderInput
	}
	name, err := normalizeName(input.Name)
	if err != nil {
		return err
	}
	input.Name = name
	return normalizeTextFields(input.Description, input.Color)
}

func normalizeUpdateInput(input *models.UpdateCallFolderInput) error {
	if input.Name != nil {
		name, err := normalizeName(*input.Name)
		if err != nil {
			return err
		}
		input.Name = &name
	}
	return normalizeTextFields(input.Description, input.Color)
}

func normalizeListInput(input *models.ListCallFoldersInput) error {
	allVisible := input.Scope == "" && !input.CompanyUUID.Valid && !input.DepartmentUUID.Valid
	if !allVisible && !validScopePlacement(input.Scope, input.CompanyUUID, input.DepartmentUUID) {
		return models.ErrInvalidCallFolderInput
	}
	input.Q = strings.TrimSpace(input.Q)
	if input.Limit <= 0 {
		input.Limit = defaultListLimit
	}
	if input.Limit > maxListLimit || input.Offset < 0 {
		return models.ErrInvalidCallFolderInput
	}
	return nil
}

func validScopePlacement(scope models.CallFolderScope, companyID uuid.NullUUID, departmentID uuid.NullUUID) bool {
	switch scope {
	case models.CallFolderScopePersonal:
		return !companyID.Valid && !departmentID.Valid
	case models.CallFolderScopeCompany:
		return companyID.Valid && !departmentID.Valid
	case models.CallFolderScopeDepartment:
		return companyID.Valid && departmentID.Valid
	default:
		return false
	}
}

func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return "", models.ErrInvalidCallFolderInput
	}
	return name, nil
}

func normalizeTextFields(description *string, color *string) error {
	if description != nil {
		value := strings.TrimSpace(*description)
		if utf8.RuneCountInString(value) > 1000 {
			return models.ErrInvalidCallFolderInput
		}
		*description = value
	}
	if color != nil {
		value := strings.TrimSpace(*color)
		if value != "" && !colorPattern.MatchString(value) {
			return models.ErrInvalidCallFolderInput
		}
		*color = value
	}
	return nil
}

func maskForbiddenAsNotFound(err error) error {
	if errors.Is(err, models.ErrForbidden) {
		return models.ErrCallFolderNotFound
	}
	return err
}
