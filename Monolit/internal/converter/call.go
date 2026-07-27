package converter

import (
	"strings"
	"time"

	"calllens/monolit/internal/API/dto"
	"calllens/monolit/internal/models"

	"github.com/google/uuid"
)

func CreateAPIToModel(callUUID uuid.UUID, title string, status models.CallStatus,
	audioPath string, originalFilename string, mimeType string,
	sizeBytes int64, now time.Time) (models.Call, error) {
	return models.Call{
		ID:               callUUID,
		Title:            title,
		Status:           status,
		AudioPath:        audioPath,
		ASRCachePath:     "asr/" + callUUID.String() + ".ogg",
		OriginalFilename: originalFilename,
		MimeType:         mimeType,
		DurationSeconds:  0,
		SizeBytes:        sizeBytes,
		VisibilityScope:  models.CallVisibilityScopePersonal,
		CreatedAt:        now,
	}, nil
}

func CallModelToAPI(call models.Call) (dto.CallResponse, error) {
	audioURL := "/api/v1/calls/" + call.ID.String() + "/audio"
	mediaURL := "/api/v1/calls/" + call.ID.String() + "/media"
	mediaKind := "audio"
	if strings.HasPrefix(strings.ToLower(call.MimeType), "video/") {
		mediaKind = "video"
	}
	return dto.CallResponse{
		ID:                    call.ID.String(),
		Title:                 call.Title,
		Status:                string(call.Status),
		OriginalFilename:      call.OriginalFilename,
		MimeType:              call.MimeType,
		SizeBytes:             call.SizeBytes,
		DurationSeconds:       call.DurationSeconds,
		AudioURL:              audioURL,
		MediaURL:              mediaURL,
		MediaKind:             mediaKind,
		UploadedByUserUUID:    nullUUIDToStringPtr(call.UploadedByUserUUID),
		CompanyUUID:           nullUUIDToStringPtr(call.CompanyUUID),
		DepartmentUUID:        nullUUIDToStringPtr(call.DepartmentUUID),
		VisibilityScope:       string(call.VisibilityScope),
		UseCustomInstructions: !call.SkipCustomInstructions,
		CreatedAt:             call.CreatedAt.Format(time.RFC3339),
	}, nil
}

func nullUUIDToStringPtr(id uuid.NullUUID) *string {
	if !id.Valid {
		return nil
	}

	value := id.UUID.String()
	return &value
}

func SavedFileToModel(savedFile models.SavedFile, callUUID uuid.UUID, input models.CreateCallInput, now time.Time) (models.Call, error) {
	return models.Call{
		ID:               callUUID,
		Title:            input.Title,
		Status:           models.CallStatusNew,
		AudioPath:        savedFile.Path,
		ASRCachePath:     "asr/" + callUUID.String() + ".ogg",
		OriginalFilename: input.OriginalFilename,
		MimeType:         input.MimeType,
		SizeBytes:        savedFile.SizeBytes,
		DurationSeconds:  0,
		UploadedByUserUUID: uuid.NullUUID{
			UUID:  input.UploadedByUserUUID,
			Valid: true,
		},
		CompanyUUID:            input.CompanyUUID,
		DepartmentUUID:         input.DepartmentUUID,
		VisibilityScope:        input.VisibilityScope,
		SkipCustomInstructions: input.SkipCustomInstructions,
		CreatedAt:              now,
	}, nil
}
