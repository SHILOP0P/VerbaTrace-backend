package converter

import (
	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func RepoAnalysisInstructionToModel(repoInstruction repoModel.AnalysisInstruction) (model.AnalysisInstruction, error) {
	return model.AnalysisInstruction{
		ID:                repoInstruction.ID,
		Scope:             model.AnalysisInstructionScope(repoInstruction.Scope),
		UserUUID:          repoInstruction.UserUUID,
		CompanyUUID:       repoInstruction.CompanyUUID,
		DepartmentUUID:    repoInstruction.DepartmentUUID,
		Title:             repoInstruction.Title,
		OriginalFilename:  repoInstruction.OriginalFilename,
		FilePath:          repoInstruction.FilePath,
		MimeType:          repoInstruction.MimeType,
		SizeBytes:         repoInstruction.SizeBytes,
		ContentSHA256:     repoInstruction.ContentSHA256,
		SortOrder:         repoInstruction.SortOrder,
		IsActive:          repoInstruction.IsActive,
		CreatedByUserUUID: repoInstruction.CreatedByUserUUID,
		CreatedAt:         repoInstruction.CreatedAt,
		UpdatedAt:         repoInstruction.UpdatedAt,
	}, nil
}

func RepoAnalysisInstructionsToModels(repoInstructions []repoModel.AnalysisInstruction) ([]model.AnalysisInstruction, error) {
	result := make([]model.AnalysisInstruction, len(repoInstructions))
	for i, instruction := range repoInstructions {
		result[i], _ = RepoAnalysisInstructionToModel(instruction)
	}

	return result, nil
}

func ModelAnalysisInstructionToRepoAnalysisInstruction(modelInstruction model.AnalysisInstruction) (repoModel.AnalysisInstruction, error) {
	return repoModel.AnalysisInstruction{
		ID:                modelInstruction.ID,
		Scope:             string(modelInstruction.Scope),
		UserUUID:          modelInstruction.UserUUID,
		CompanyUUID:       modelInstruction.CompanyUUID,
		DepartmentUUID:    modelInstruction.DepartmentUUID,
		Title:             modelInstruction.Title,
		OriginalFilename:  modelInstruction.OriginalFilename,
		FilePath:          modelInstruction.FilePath,
		MimeType:          modelInstruction.MimeType,
		SizeBytes:         modelInstruction.SizeBytes,
		ContentSHA256:     modelInstruction.ContentSHA256,
		SortOrder:         modelInstruction.SortOrder,
		IsActive:          modelInstruction.IsActive,
		CreatedByUserUUID: modelInstruction.CreatedByUserUUID,
		CreatedAt:         modelInstruction.CreatedAt,
		UpdatedAt:         modelInstruction.UpdatedAt,
	}, nil
}
