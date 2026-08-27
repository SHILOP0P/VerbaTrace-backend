package scaner

import (
	repoModel "verbatrace/monolit/internal/repository/models"
)

type rowScanner interface {
	Scan(dest ...any) error
}

func ScanCall(row rowScanner) (repoModel.Call, error) {
	var call repoModel.Call

	err := row.Scan(
		&call.ID,
		&call.Title,
		&call.Status,
		&call.AudioPath,
		&call.ASRCachePath,
		&call.OriginalFilename,
		&call.MimeType,
		&call.SizeBytes,
		&call.DurationSeconds,
		&call.UploadedByUserUUID,
		&call.CompanyUUID,
		&call.DepartmentUUID,
		&call.VisibilityScope,
		&call.SkipCustomInstructions,
		&call.IsTest,
		&call.CreatedAt,
	)
	if err != nil {
		return repoModel.Call{}, err
	}

	return call, nil
}
