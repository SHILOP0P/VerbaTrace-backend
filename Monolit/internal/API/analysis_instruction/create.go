package analysis_instruction

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 1 << 20

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

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInstructionFileRequired, "instruction file is required")
		return
	}
	defer func() { _ = file.Close() }()

	if filepath.Ext(fileHeader.Filename) == "" {
		response.WriteError(w, http.StatusBadRequest, response.CodeInstructionFileExtensionRequired, "instruction file extension is required")
		return
	}

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInstructionFileReadFailed, "failed to read file")
		return
	}

	detectedMimeType := http.DetectContentType(buffer[:n])
	fileContent := io.MultiReader(bytes.NewReader(buffer[:n]), file)

	scope, userUUID, companyUUID, departmentUUID, err := parseInstructionPlacement(
		r.FormValue("scope"),
		userID,
		r.FormValue("company_uuid"),
		r.FormValue("department_uuid"),
	)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInstructionInput, "invalid instruction placement")
		return
	}

	input := models.CreateAnalysisInstructionInput{
		Scope:             scope,
		UserUUID:          userUUID,
		CompanyUUID:       companyUUID,
		DepartmentUUID:    departmentUUID,
		Title:             r.FormValue("title"),
		OriginalFilename:  fileHeader.Filename,
		MimeType:          detectedMimeType,
		SizeBytes:         fileHeader.Size,
		Content:           fileContent,
		CreatedByUserUUID: userID,
	}

	created, err := h.service.Create(r.Context(), input)
	if err != nil {
		writeInstructionError(w, err, response.CodeFailedToCreateInstruction, "failed to create instruction")
		return
	}

	resp, err := converter.AnalysisInstructionModelToAPI(created)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInstruction, "failed to convert instruction")
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, resp)
}

func parseInstructionPlacement(scopeValue string, userID uuid.UUID, companyUUIDValue string, departmentUUIDValue string) (models.AnalysisInstructionScope, uuid.UUID, uuid.NullUUID, uuid.NullUUID, error) {
	scope := models.AnalysisInstructionScope(strings.TrimSpace(scopeValue))
	companyUUIDValue = strings.TrimSpace(companyUUIDValue)
	departmentUUIDValue = strings.TrimSpace(departmentUUIDValue)

	switch scope {
	case models.AnalysisInstructionScopePersonal:
		if companyUUIDValue != "" || departmentUUIDValue != "" {
			return "", uuid.Nil, uuid.NullUUID{}, uuid.NullUUID{}, models.ErrInvalidAnalysisInstructionInput
		}
		return scope, userID, uuid.NullUUID{}, uuid.NullUUID{}, nil
	case models.AnalysisInstructionScopeCompany:
		if companyUUIDValue == "" || departmentUUIDValue != "" {
			return "", uuid.Nil, uuid.NullUUID{}, uuid.NullUUID{}, models.ErrInvalidAnalysisInstructionInput
		}
		companyID, err := uuid.Parse(companyUUIDValue)
		if err != nil {
			return "", uuid.Nil, uuid.NullUUID{}, uuid.NullUUID{}, models.ErrInvalidAnalysisInstructionInput
		}
		return scope, uuid.Nil, uuid.NullUUID{UUID: companyID, Valid: true}, uuid.NullUUID{}, nil
	case models.AnalysisInstructionScopeDepartment:
		if companyUUIDValue == "" || departmentUUIDValue == "" {
			return "", uuid.Nil, uuid.NullUUID{}, uuid.NullUUID{}, models.ErrInvalidAnalysisInstructionInput
		}
		companyID, err := uuid.Parse(companyUUIDValue)
		if err != nil {
			return "", uuid.Nil, uuid.NullUUID{}, uuid.NullUUID{}, models.ErrInvalidAnalysisInstructionInput
		}
		departmentID, err := uuid.Parse(departmentUUIDValue)
		if err != nil {
			return "", uuid.Nil, uuid.NullUUID{}, uuid.NullUUID{}, models.ErrInvalidAnalysisInstructionInput
		}
		return scope, uuid.Nil, uuid.NullUUID{UUID: companyID, Valid: true}, uuid.NullUUID{UUID: departmentID, Valid: true}, nil
	default:
		return "", uuid.Nil, uuid.NullUUID{}, uuid.NullUUID{}, models.ErrInvalidAnalysisInstructionInput
	}
}

func writeInstructionError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	switch {
	case errors.Is(err, models.ErrInvalidAnalysisInstructionInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInstructionInput, "invalid instruction input")
	case errors.Is(err, models.ErrUnsupportedInstructionType):
		response.WriteError(w, http.StatusBadRequest, response.CodeUnsupportedInstructionType, "unsupported instruction type")
	case errors.Is(err, models.ErrInstructionLimitExceeded):
		response.WriteError(w, http.StatusBadRequest, response.CodeInstructionLimitExceeded, "instruction limit exceeded")
	case errors.Is(err, models.ErrSubscriptionRequired):
		response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
	case errors.Is(err, models.ErrAnalysisInstructionNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeAnalysisInstructionNotFound, "analysis instruction not found")
	case errors.Is(err, models.ErrCompanyNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
	case errors.Is(err, models.ErrDepartmentNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeDepartmentNotFound, "department not found")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrCompanyFrozen):
		response.WriteError(w, http.StatusConflict, response.CodeCompanyFrozen, "company is frozen")
	default:
		response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
	}
}
