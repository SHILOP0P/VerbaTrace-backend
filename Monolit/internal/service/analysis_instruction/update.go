package analysis_instruction

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) Update(ctx context.Context, input models.UpdateAnalysisInstructionInput) (models.AnalysisInstruction, error) {
	if input.ID == uuid.Nil || input.UserUUID == uuid.Nil {
		return models.AnalysisInstruction{}, models.ErrInvalidAnalysisInstructionInput
	}
	if input.SortOrder != nil && *input.SortOrder < 0 {
		return models.AnalysisInstruction{}, models.ErrInvalidAnalysisInstructionInput
	}

	instruction, err := s.repository.GetByUUIDIncludingInactive(ctx, input.ID)
	if err != nil {
		return models.AnalysisInstruction{}, err
	}
	if err := s.authorizeEdit(ctx, instruction, input.UserUUID); err != nil {
		return models.AnalysisInstruction{}, err
	}
	if err := s.checkActivation(ctx, instruction, input.IsActive); err != nil {
		return models.AnalysisInstruction{}, err
	}

	var title *string
	if input.Title != nil {
		trimmed := strings.TrimSpace(*input.Title)
		if trimmed == "" {
			return models.AnalysisInstruction{}, models.ErrInvalidAnalysisInstructionInput
		}
		title = &trimmed
	}

	return s.repository.Update(ctx, models.UpdateAnalysisInstructionRepositoryInput{
		ID:        input.ID,
		Title:     title,
		IsActive:  input.IsActive,
		SortOrder: input.SortOrder,
	})
}

func (s *Service) ReplaceFile(ctx context.Context, input models.ReplaceAnalysisInstructionFileInput) (models.AnalysisInstruction, error) {
	if input.ID == uuid.Nil || input.UserUUID == uuid.Nil || input.Content == nil {
		return models.AnalysisInstruction{}, models.ErrInvalidAnalysisInstructionInput
	}
	if strings.TrimSpace(input.OriginalFilename) == "" {
		return models.AnalysisInstruction{}, models.ErrInvalidAnalysisInstructionInput
	}
	if strings.ToLower(filepath.Ext(input.OriginalFilename)) != ".md" {
		return models.AnalysisInstruction{}, models.ErrUnsupportedInstructionType
	}
	if !isSupportedInstructionMime(input.MimeType) {
		return models.AnalysisInstruction{}, models.ErrUnsupportedInstructionType
	}

	instruction, err := s.repository.GetByUUIDIncludingInactive(ctx, input.ID)
	if err != nil {
		return models.AnalysisInstruction{}, err
	}
	if err := s.authorizeEdit(ctx, instruction, input.UserUUID); err != nil {
		return models.AnalysisInstruction{}, err
	}

	savedFile, err := s.instructionStorage.Save(ctx, models.SaveInstructionInput{
		InstructionUUID:  instruction.ID,
		Scope:            instruction.Scope,
		UserUUID:         instruction.UserUUID,
		CompanyUUID:      instruction.CompanyUUID,
		DepartmentUUID:   instruction.DepartmentUUID,
		OriginalFilename: input.OriginalFilename,
		Content:          input.Content,
		MimeType:         input.MimeType,
	})
	if err != nil {
		return models.AnalysisInstruction{}, err
	}

	updated, err := s.repository.Update(ctx, models.UpdateAnalysisInstructionRepositoryInput{
		ID:               instruction.ID,
		OriginalFilename: &input.OriginalFilename,
		FilePath:         &savedFile.Path,
		MimeType:         &savedFile.MimeType,
		SizeBytes:        &savedFile.SizeBytes,
		ContentSHA256:    &savedFile.ContentSHA256,
	})
	if err != nil {
		return models.AnalysisInstruction{}, fmt.Errorf("update replaced instruction file metadata: %w", err)
	}

	return updated, nil
}

func (s *Service) Reorder(ctx context.Context, input models.ReorderAnalysisInstructionsInput) error {
	if err := validateListInput(models.ListAnalysisInstructionsInput{
		Scope:          input.Scope,
		UserUUID:       input.UserUUID,
		CompanyUUID:    input.CompanyUUID,
		DepartmentUUID: input.DepartmentUUID,
	}); err != nil {
		return err
	}
	if len(input.Items) == 0 {
		return models.ErrInvalidAnalysisInstructionInput
	}
	if err := s.authorizeEditScope(ctx, input); err != nil {
		return err
	}

	seen := make(map[uuid.UUID]struct{}, len(input.Items))
	for _, item := range input.Items {
		if item.ID == uuid.Nil {
			return models.ErrInvalidAnalysisInstructionInput
		}
		if item.SortOrder < 0 {
			return models.ErrInvalidAnalysisInstructionInput
		}
		if _, ok := seen[item.ID]; ok {
			return models.ErrInvalidAnalysisInstructionInput
		}
		seen[item.ID] = struct{}{}

		instruction, err := s.repository.GetByUUIDIncludingInactive(ctx, item.ID)
		if err != nil {
			return err
		}
		if !instructionBelongsToScope(instruction, input.Scope, input.CompanyUUID, input.DepartmentUUID, input.UserUUID) {
			return models.ErrInvalidAnalysisInstructionInput
		}
	}

	return s.repository.Reorder(ctx, input.Items)
}

func (s *Service) checkActivation(ctx context.Context, instruction models.AnalysisInstruction, nextActive *bool) error {
	if nextActive == nil || !*nextActive || instruction.IsActive {
		return nil
	}

	filter := ownerFilterFromInstruction(instruction)
	if s.billingLimiter != nil {
		switch instruction.Scope {
		case models.AnalysisInstructionScopePersonal:
			return s.billingLimiter.CanCreatePersonalInstruction(ctx, instruction.UserUUID.UUID)
		case models.AnalysisInstructionScopeCompany:
			return s.billingLimiter.CanCreateCompanyInstruction(ctx, instruction.CompanyUUID.UUID)
		case models.AnalysisInstructionScopeDepartment:
			return s.billingLimiter.CanCreateDepartmentInstruction(ctx, instruction.CompanyUUID.UUID, instruction.DepartmentUUID.UUID)
		default:
			return models.ErrInvalidAnalysisInstructionInput
		}
	}

	count, err := s.repository.CountActive(ctx, filter)
	if err != nil {
		return err
	}
	if count >= instructionLimit(instruction.Scope) {
		return models.ErrInstructionLimitExceeded
	}
	return nil
}

func ownerFilterFromInstruction(instruction models.AnalysisInstruction) models.ListAnalysisInstructionsInput {
	return models.ListAnalysisInstructionsInput{
		Scope:          instruction.Scope,
		UserUUID:       instruction.UserUUID.UUID,
		CompanyUUID:    instruction.CompanyUUID,
		DepartmentUUID: instruction.DepartmentUUID,
	}
}

func instructionBelongsToScope(instruction models.AnalysisInstruction, scope models.AnalysisInstructionScope, companyID uuid.NullUUID, departmentID uuid.NullUUID, userID uuid.UUID) bool {
	if instruction.Scope != scope {
		return false
	}
	switch scope {
	case models.AnalysisInstructionScopePersonal:
		return instruction.UserUUID.Valid && instruction.UserUUID.UUID == userID && !instruction.CompanyUUID.Valid && !instruction.DepartmentUUID.Valid
	case models.AnalysisInstructionScopeCompany:
		return instruction.CompanyUUID.Valid && instruction.CompanyUUID.UUID == companyID.UUID && !instruction.DepartmentUUID.Valid
	case models.AnalysisInstructionScopeDepartment:
		return instruction.CompanyUUID.Valid && instruction.CompanyUUID.UUID == companyID.UUID &&
			instruction.DepartmentUUID.Valid && instruction.DepartmentUUID.UUID == departmentID.UUID
	default:
		return false
	}
}

func isSupportedInstructionMime(mimeType string) bool {
	value := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	return value == "text/markdown" || value == "text/plain"
}
