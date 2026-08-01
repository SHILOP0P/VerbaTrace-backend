package converter

import (
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"
)

func AnalysisInstructionModelToAPI(instruction models.AnalysisInstruction) (dto.AnalysisInstruction, error) {
	return dto.AnalysisInstruction{
		ID:                instruction.ID.String(),
		Scope:             string(instruction.Scope),
		UserUUID:          nullUUIDToStringPtr(instruction.UserUUID),
		CompanyUUID:       nullUUIDToStringPtr(instruction.CompanyUUID),
		DepartmentUUID:    nullUUIDToStringPtr(instruction.DepartmentUUID),
		Title:             instruction.Title,
		OriginalFilename:  instruction.OriginalFilename,
		DownloadURL:       "/api/v1/instructions/" + instruction.ID.String() + "/download",
		MimeType:          instruction.MimeType,
		SizeBytes:         instruction.SizeBytes,
		ContentSHA256:     instruction.ContentSHA256,
		SortOrder:         instruction.SortOrder,
		IsActive:          instruction.IsActive,
		CreatedByUserUUID: instruction.CreatedByUserUUID.String(),
		CreatedAt:         instruction.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         instruction.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func AnalysisInstructionModelsToAPI(instructions []models.AnalysisInstruction) ([]dto.AnalysisInstruction, error) {
	result := make([]dto.AnalysisInstruction, len(instructions))
	for i, instruction := range instructions {
		result[i], _ = AnalysisInstructionModelToAPI(instruction)
	}

	return result, nil
}
