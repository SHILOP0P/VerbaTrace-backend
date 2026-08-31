package converter

import (
	"strings"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/models"

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
		IsTest:                call.IsTest,
		SpeakerHints:          speakerHintsToAPI(call.SpeakerHints),
		DiarizationRoles:      diarizationRolesToAPI(call.DiarizationRoles),
		OccurredAt:            timePtrToRFC3339(call.OccurredAt),
		DisplayTime:           callDisplayTime(call).Format(time.RFC3339),
		TimeSource:            callTimeSource(call),
		SourceProvider:        call.SourceProvider,
		ConnectionUUID:        nullUUIDToStringPtr(call.IntegrationConnectionUUID),
		ExternalCallID:        call.ExternalCallID,
		ImportedAt:            timePtrToRFC3339(call.ImportedAt),
		IngestErrorCode:       call.IngestErrorCode,
		HasAnalysis:           call.HasAnalysis,
		HasActions:            call.HasActions,
		CreatedAt:             call.CreatedAt.Format(time.RFC3339),
	}, nil
}

func timePtrToRFC3339(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func callDisplayTime(call models.Call) time.Time {
	if !call.DisplayTime.IsZero() {
		return call.DisplayTime
	}
	if call.OccurredAt != nil {
		return *call.OccurredAt
	}
	return call.CreatedAt
}

func callTimeSource(call models.Call) string {
	if call.TimeSource != "" {
		return call.TimeSource
	}
	if call.OccurredAt != nil {
		return "source"
	}
	return "upload_fallback"
}

func diarizationRolesToAPI(roles []models.DiarizationRole) []dto.DiarizationRoleResponse {
	if len(roles) == 0 {
		return nil
	}
	result := make([]dto.DiarizationRoleResponse, 0, len(roles))
	for _, role := range roles {
		result = append(result, dto.DiarizationRoleResponse{Name: role.Name, Description: role.Description})
	}
	return result
}

func speakerHintsToAPI(hints []models.SpeakerHint) []dto.SpeakerHintResponse {
	if len(hints) == 0 {
		return nil
	}
	result := make([]dto.SpeakerHintResponse, 0, len(hints))
	for _, hint := range hints {
		result = append(result, dto.SpeakerHintResponse{UserID: hint.UserID.String(), Name: hint.Name, Username: hint.Username, Role: hint.Role, Note: hint.Note})
	}
	return result
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
		SpeakerHints:           input.SpeakerHints,
		DiarizationRoles:       input.DiarizationRoles,
		CreatedAt:              now,
	}, nil
}
