package scaner

import repoModel "verbatrace/monolit/internal/repository/models"

func ScanAnalysisInstruction(row rowScanner) (repoModel.AnalysisInstruction, error) {
	var instruction repoModel.AnalysisInstruction

	err := row.Scan(
		&instruction.ID,
		&instruction.Scope,
		&instruction.UserUUID,
		&instruction.CompanyUUID,
		&instruction.DepartmentUUID,
		&instruction.Title,
		&instruction.OriginalFilename,
		&instruction.FilePath,
		&instruction.MimeType,
		&instruction.SizeBytes,
		&instruction.ContentSHA256,
		&instruction.SortOrder,
		&instruction.IsActive,
		&instruction.CreatedByUserUUID,
		&instruction.CreatedAt,
		&instruction.UpdatedAt,
	)
	if err != nil {
		return repoModel.AnalysisInstruction{}, err
	}

	return instruction, nil
}
