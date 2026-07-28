package call

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"calllens/monolit/internal/API/dto"
	"calllens/monolit/internal/API/response"
	"calllens/monolit/internal/converter"
	model "calllens/monolit/internal/models"

	"github.com/google/uuid"
)

type speakerHintRequest struct {
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Note     string `json:"note"`
}

type diarizationRoleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *CallHandler) Create(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 500 << 20 // Temporary local-test limit: 500 MiB.

	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidMultipartForm, "failed to parse multipart form")
		return
	}

	file, fileHeader, err := r.FormFile("media")
	if err != nil {
		file, fileHeader, err = r.FormFile("audio")
	}
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeAudioFileRequired, "audio or video file is required")
		return
	}
	defer func() { _ = file.Close() }()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = titleFromFilename(fileHeader.Filename)
	}

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeAudioFileReadFailed, "failed to read file")
		return
	}

	detectedMimeType := normalizeDetectedMediaMimeType(fileHeader.Filename, http.DetectContentType(buffer[:n]))
	fileContent := io.MultiReader(bytes.NewReader(buffer[:n]), file)

	req := dto.CreateCallRequest{
		Title:                  title,
		Media:                  fileHeader,
		CompanyUUID:            r.FormValue("company_uuid"),
		DepartmentUUID:         r.FormValue("department_uuid"),
		FolderUUID:             r.FormValue("folder_uuid"),
		SkipCustomInstructions: parseSkipCustomInstructions(r.FormValue("use_custom_instructions"), r.FormValue("skip_custom_instructions")),
	}

	companyUUID, departmentUUID, visibilityScope, err := parseCallPlacement(req.CompanyUUID, req.DepartmentUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallPlacement, "invalid call placement")
		return
	}
	folderUUID, err := parseOptionalUUID(strings.TrimSpace(req.FolderUUID))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid folder uuid")
		return
	}
	speakerHints, err := parseSpeakerHints(r.FormValue("speaker_hints"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid speaker hints")
		return
	}
	diarizationRoles, err := parseDiarizationRoles(r.FormValue("diarization_roles"), speakerHints)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid diarization roles")
		return
	}

	ext := filepath.Ext(fileHeader.Filename)
	if ext == "" {
		response.WriteError(w, http.StatusBadRequest, response.CodeAudioFileExtensionRequired, "audio file extension is required")
		return
	}

	originalFilename := req.Media.Filename
	//mimeType := req.Audio.Header.Get("Content-Type")
	sizeBytes := req.Media.Size

	input := model.CreateCallInput{
		Title:                  title,
		OriginalFilename:       originalFilename,
		MimeType:               detectedMimeType,
		SizeBytes:              sizeBytes,
		Content:                fileContent,
		UploadedByUserUUID:     userID,
		CompanyUUID:            companyUUID,
		DepartmentUUID:         departmentUUID,
		VisibilityScope:        visibilityScope,
		SkipCustomInstructions: req.SkipCustomInstructions,
		FolderUUID:             folderUUID,
		SpeakerHints:           speakerHints,
		DiarizationRoles:       diarizationRoles,
	}

	createdCall, err := h.service.CreateCall(r.Context(), input)
	if err != nil {
		if errors.Is(err, model.ErrCallConvert) {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToProcessCall, "failed to process call")
			return
		} else if errors.Is(err, model.ErrCallNotFound) {
			response.WriteError(w, http.StatusInternalServerError, response.CodeCallNotFound, "call not found")
			return
		} else if errors.Is(err, model.ErrUnsupportedAudioType) {
			response.WriteError(w, http.StatusBadRequest, response.CodeUnsupportedAudioType, "unsupported audio type")
			return
		} else if errors.Is(err, model.ErrInvalidCallOwner) {
			response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
			return
		} else if errors.Is(err, model.ErrInvalidCallPlacement) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallPlacement, "invalid call placement")
			return
		} else if errors.Is(err, model.ErrForbidden) {
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
			return
		} else if errors.Is(err, model.ErrAudioProbeNotFound) {
			response.WriteError(w, http.StatusInternalServerError, response.CodeAudioProbeNotFound, "audio metadata analyzer is not configured")
			return
		} else if errors.Is(err, model.ErrAudioFileUnreadable) {
			response.WriteError(w, http.StatusInternalServerError, response.CodeAudioFileUnreadable, "audio file cannot be read")
			return
		} else if errors.Is(err, model.ErrSubscriptionRequired) {
			response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
			return
		} else if errors.Is(err, model.ErrMonthlyMinutesLimitExceeded) {
			response.WriteError(w, http.StatusBadRequest, response.CodeMonthlyMinutesLimitExceeded, "monthly minutes limit exceeded")
			return
		} else {
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToCreateCall, "failed to create call")
			return
		}
	}
	createdCall.SpeakerHints = speakerHints
	createdCall.DiarizationRoles = diarizationRoles

	resp, err := converter.CallModelToAPI(createdCall)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToCreateCall, "failed to create call")
		return
	}

	if err := response.WriteJSON(w, http.StatusCreated, resp); err != nil {
		return
	}
}

func parseDiarizationRoles(value string, hints []model.SpeakerHint) ([]model.DiarizationRole, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	var requests []diarizationRoleRequest
	if err := json.Unmarshal([]byte(value), &requests); err != nil || len(requests) > 10 || len(requests)+len(hints) > 10 {
		return nil, model.ErrInvalidCallPlacement
	}
	labels := make(map[string]struct{}, len(hints)+len(requests))
	for _, hint := range hints {
		labels[strings.ToLower(strings.TrimSpace(hint.Name))] = struct{}{}
	}
	result := make([]model.DiarizationRole, 0, len(requests))
	for _, item := range requests {
		name := strings.TrimSpace(item.Name)
		description := strings.TrimSpace(item.Description)
		key := strings.ToLower(name)
		if name == "" || len([]rune(name)) > 80 || len([]rune(description)) > 300 {
			return nil, model.ErrInvalidCallPlacement
		}
		if _, exists := labels[key]; exists {
			return nil, model.ErrInvalidCallPlacement
		}
		labels[key] = struct{}{}
		result = append(result, model.DiarizationRole{Name: name, Description: description})
	}
	return result, nil
}

func parseSpeakerHints(value string) ([]model.SpeakerHint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	var requests []speakerHintRequest
	if err := json.Unmarshal([]byte(value), &requests); err != nil || len(requests) > 10 {
		return nil, model.ErrInvalidCallPlacement
	}
	result := make([]model.SpeakerHint, 0, len(requests))
	seen := make(map[uuid.UUID]struct{}, len(requests))
	for _, item := range requests {
		id, err := uuid.Parse(strings.TrimSpace(item.UserID))
		if err != nil || strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 120 || len([]rune(item.Note)) > 160 {
			return nil, model.ErrInvalidCallPlacement
		}
		if _, ok := seen[id]; ok {
			return nil, model.ErrInvalidCallPlacement
		}
		seen[id] = struct{}{}
		role := strings.TrimSpace(item.Role)
		if role != "self" && role != "manager" && role != "client" && role != "other" {
			return nil, model.ErrInvalidCallPlacement
		}
		result = append(result, model.SpeakerHint{UserID: id, Name: strings.TrimSpace(item.Name), Username: strings.TrimSpace(item.Username), Role: role, Note: strings.TrimSpace(item.Note)})
	}
	return result, nil
}

func titleFromFilename(filename string) string {
	base := strings.TrimSpace(filepath.Base(filename))
	name := strings.TrimSpace(strings.TrimSuffix(base, filepath.Ext(base)))
	if name == "" || name == "." {
		return "Звонок"
	}
	return name
}

func normalizeDetectedMediaMimeType(filename string, detected string) string {
	detected = strings.ToLower(strings.TrimSpace(strings.Split(detected, ";")[0]))
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".ogg" && detected == "application/ogg" {
		return "audio/ogg"
	}
	if detected != "application/octet-stream" {
		return detected
	}

	switch ext {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	default:
		return detected
	}
}

func parseSkipCustomInstructions(useCustomInstructions string, skipCustomInstructions string) bool {
	if value, ok := parseOptionalBool(skipCustomInstructions); ok {
		return value
	}
	if value, ok := parseOptionalBool(useCustomInstructions); ok {
		return !value
	}
	return false
}

func parseOptionalBool(value string) (bool, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, false
	}
	return parsed, true
}

func parseCallPlacement(companyUUIDValue string, departmentUUIDValue string) (uuid.NullUUID, uuid.NullUUID, model.CallVisibilityScope, error) {
	companyUUIDValue = strings.TrimSpace(companyUUIDValue)
	departmentUUIDValue = strings.TrimSpace(departmentUUIDValue)

	if companyUUIDValue == "" && departmentUUIDValue == "" {
		return uuid.NullUUID{}, uuid.NullUUID{}, model.CallVisibilityScopePersonal, nil
	}

	if companyUUIDValue == "" {
		return uuid.NullUUID{}, uuid.NullUUID{}, "", model.ErrInvalidCallPlacement
	}

	companyUUID, err := uuid.Parse(companyUUIDValue)
	if err != nil {
		return uuid.NullUUID{}, uuid.NullUUID{}, "", model.ErrInvalidCallPlacement
	}

	companyNullUUID := uuid.NullUUID{
		UUID:  companyUUID,
		Valid: true,
	}

	if departmentUUIDValue == "" {
		return companyNullUUID, uuid.NullUUID{}, model.CallVisibilityScopeCompany, nil
	}

	departmentUUID, err := uuid.Parse(departmentUUIDValue)
	if err != nil {
		return uuid.NullUUID{}, uuid.NullUUID{}, "", model.ErrInvalidCallPlacement
	}

	departmentNullUUID := uuid.NullUUID{
		UUID:  departmentUUID,
		Valid: true,
	}

	return companyNullUUID, departmentNullUUID, model.CallVisibilityScopeDepartment, nil
}
