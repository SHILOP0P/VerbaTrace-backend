package converter

import (
	model "verbatrace/monolit/internal/models"
	repoModel "verbatrace/monolit/internal/repository/models"
)

func RepoCallToModel(repoCall repoModel.Call) (model.Call, error) {
	return model.Call{
		ID:                     repoCall.ID,
		Title:                  repoCall.Title,
		Status:                 model.CallStatus(repoCall.Status),
		AudioPath:              repoCall.AudioPath,
		ASRCachePath:           repoCall.ASRCachePath,
		OriginalFilename:       repoCall.OriginalFilename,
		MimeType:               repoCall.MimeType,
		SizeBytes:              repoCall.SizeBytes,
		DurationSeconds:        repoCall.DurationSeconds,
		UploadedByUserUUID:     repoCall.UploadedByUserUUID,
		CompanyUUID:            repoCall.CompanyUUID,
		DepartmentUUID:         repoCall.DepartmentUUID,
		VisibilityScope:        model.CallVisibilityScope(repoCall.VisibilityScope),
		SkipCustomInstructions: repoCall.SkipCustomInstructions,
		CreatedAt:              repoCall.CreatedAt,
	}, nil
}

func RepoCallsToModels(repoCalls []repoModel.Call) ([]model.Call, error) {
	result := make([]model.Call, len(repoCalls))
	for i, call := range repoCalls {
		result[i], _ = RepoCallToModel(call)
	}
	return result, nil
}

func ModelCallToRepoCall(modelCall model.Call) (repoCall repoModel.Call, err error) {
	return repoModel.Call{
		ID:                     modelCall.ID,
		Title:                  modelCall.Title,
		Status:                 string(modelCall.Status),
		AudioPath:              modelCall.AudioPath,
		ASRCachePath:           modelCall.ASRCachePath,
		OriginalFilename:       modelCall.OriginalFilename,
		MimeType:               modelCall.MimeType,
		SizeBytes:              modelCall.SizeBytes,
		DurationSeconds:        modelCall.DurationSeconds,
		UploadedByUserUUID:     modelCall.UploadedByUserUUID,
		CompanyUUID:            modelCall.CompanyUUID,
		DepartmentUUID:         modelCall.DepartmentUUID,
		VisibilityScope:        string(modelCall.VisibilityScope),
		SkipCustomInstructions: modelCall.SkipCustomInstructions,
		CreatedAt:              modelCall.CreatedAt,
	}, nil
}
