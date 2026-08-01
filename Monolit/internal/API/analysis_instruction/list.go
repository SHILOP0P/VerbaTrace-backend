package analysis_instruction

import (
	"net/http"
	"strconv"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	scope, _, companyUUID, departmentUUID, err := parseInstructionPlacement(
		r.URL.Query().Get("scope"),
		userID,
		r.URL.Query().Get("company_uuid"),
		r.URL.Query().Get("department_uuid"),
	)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInstructionInput, "invalid instruction placement")
		return
	}

	includeInactive, err := parseOptionalBool(r.URL.Query().Get("include_inactive"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInstructionInput, "invalid include_inactive")
		return
	}
	limit, err := parseOptionalNonNegativeInt(r.URL.Query().Get("limit"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInstructionInput, "invalid limit")
		return
	}
	offset, err := parseOptionalNonNegativeInt(r.URL.Query().Get("offset"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInstructionInput, "invalid offset")
		return
	}

	instructions, err := h.service.List(r.Context(), models.ListAnalysisInstructionsInput{
		Scope:           scope,
		UserUUID:        userID,
		CompanyUUID:     companyUUID,
		DepartmentUUID:  departmentUUID,
		IncludeInactive: includeInactive,
		Query:           r.URL.Query().Get("q"),
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		writeInstructionError(w, err, response.CodeFailedToListInstructions, "failed to list instructions")
		return
	}

	resp, err := converter.AnalysisInstructionModelsToAPI(instructions)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInstruction, "failed to convert instructions")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func parseInstructionUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, models.ErrInvalidAnalysisInstructionInput
	}

	return id, nil
}

func parseOptionalBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	return strconv.ParseBool(value)
}

func parseOptionalNonNegativeInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, models.ErrInvalidAnalysisInstructionInput
	}
	return parsed, nil
}
