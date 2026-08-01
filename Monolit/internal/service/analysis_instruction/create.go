package analysis_instruction

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) Create(ctx context.Context, input models.CreateAnalysisInstructionInput) (models.AnalysisInstruction, error) {
	if err := validateCreateInput(input); err != nil {
		return models.AnalysisInstruction{}, err
	}

	ownerFilter, err := s.ownerFilter(input)
	if err != nil {
		return models.AnalysisInstruction{}, err
	}

	if err := s.authorizeCreate(ctx, input); err != nil {
		return models.AnalysisInstruction{}, err
	}

	count, err := s.checkBillingLimit(ctx, input, ownerFilter)
	if err != nil {
		return models.AnalysisInstruction{}, err
	}

	instructionID, err := uuid.NewV7()
	if err != nil {
		return models.AnalysisInstruction{}, fmt.Errorf("generate analysis instruction uuid: %w", err)
	}

	savedFile, err := s.instructionStorage.Save(ctx, models.SaveInstructionInput{
		InstructionUUID:  instructionID,
		Scope:            input.Scope,
		UserUUID:         ownerFilterToUser(input),
		CompanyUUID:      input.CompanyUUID,
		DepartmentUUID:   input.DepartmentUUID,
		OriginalFilename: input.OriginalFilename,
		Content:          input.Content,
		MimeType:         input.MimeType,
	})
	if err != nil {
		return models.AnalysisInstruction{}, err
	}

	now := time.Now().UTC()
	instruction := models.AnalysisInstruction{
		ID:                instructionID,
		Scope:             input.Scope,
		UserUUID:          ownerFilterToUser(input),
		CompanyUUID:       input.CompanyUUID,
		DepartmentUUID:    input.DepartmentUUID,
		Title:             strings.TrimSpace(input.Title),
		OriginalFilename:  input.OriginalFilename,
		FilePath:          savedFile.Path,
		MimeType:          savedFile.MimeType,
		SizeBytes:         savedFile.SizeBytes,
		ContentSHA256:     savedFile.ContentSHA256,
		SortOrder:         count,
		IsActive:          true,
		CreatedByUserUUID: input.CreatedByUserUUID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if instruction.Title == "" {
		instruction.Title = strings.TrimSuffix(input.OriginalFilename, filepath.Ext(input.OriginalFilename))
	}

	created, err := s.repository.Create(ctx, instruction)
	if err != nil {
		_ = s.instructionStorage.Delete(context.Background(), savedFile.Path)
		return models.AnalysisInstruction{}, err
	}

	return created, nil
}

func validateCreateInput(input models.CreateAnalysisInstructionInput) error {
	if input.CreatedByUserUUID == uuid.Nil || input.Content == nil {
		return models.ErrInvalidAnalysisInstructionInput
	}

	if strings.TrimSpace(input.OriginalFilename) == "" {
		return models.ErrInvalidAnalysisInstructionInput
	}

	if strings.ToLower(filepath.Ext(input.OriginalFilename)) != ".md" {
		return models.ErrUnsupportedInstructionType
	}

	switch input.Scope {
	case models.AnalysisInstructionScopePersonal:
		if input.UserUUID == uuid.Nil || input.UserUUID != input.CreatedByUserUUID || input.CompanyUUID.Valid || input.DepartmentUUID.Valid {
			return models.ErrInvalidAnalysisInstructionInput
		}
	case models.AnalysisInstructionScopeCompany:
		if input.UserUUID != uuid.Nil || !input.CompanyUUID.Valid || input.DepartmentUUID.Valid {
			return models.ErrInvalidAnalysisInstructionInput
		}
	case models.AnalysisInstructionScopeDepartment:
		if input.UserUUID != uuid.Nil || !input.CompanyUUID.Valid || !input.DepartmentUUID.Valid {
			return models.ErrInvalidAnalysisInstructionInput
		}
	default:
		return models.ErrInvalidAnalysisInstructionInput
	}

	return nil
}

func instructionLimit(scope models.AnalysisInstructionScope) int {
	switch scope {
	case models.AnalysisInstructionScopePersonal:
		return models.DefaultPersonalInstructionLimit
	case models.AnalysisInstructionScopeCompany:
		return models.CompanyInstructionLimit
	case models.AnalysisInstructionScopeDepartment:
		return models.DepartmentInstructionLimit
	default:
		return 0
	}
}

func (s *Service) ownerFilter(input models.CreateAnalysisInstructionInput) (models.ListAnalysisInstructionsInput, error) {
	switch input.Scope {
	case models.AnalysisInstructionScopePersonal:
		return models.ListAnalysisInstructionsInput{
			Scope:    input.Scope,
			UserUUID: input.UserUUID,
		}, nil
	case models.AnalysisInstructionScopeCompany:
		return models.ListAnalysisInstructionsInput{
			Scope:       input.Scope,
			CompanyUUID: input.CompanyUUID,
		}, nil
	case models.AnalysisInstructionScopeDepartment:
		return models.ListAnalysisInstructionsInput{
			Scope:          input.Scope,
			CompanyUUID:    input.CompanyUUID,
			DepartmentUUID: input.DepartmentUUID,
		}, nil
	default:
		return models.ListAnalysisInstructionsInput{}, models.ErrInvalidAnalysisInstructionInput
	}
}

func ownerFilterToUser(input models.CreateAnalysisInstructionInput) uuid.NullUUID {
	if input.Scope != models.AnalysisInstructionScopePersonal {
		return uuid.NullUUID{}
	}

	return uuid.NullUUID{UUID: input.UserUUID, Valid: true}
}
