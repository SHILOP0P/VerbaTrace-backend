package analysis_instruction

import (
	"encoding/json"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type reorderInstructionRequest struct {
	Scope          string                   `json:"scope"`
	CompanyUUID    string                   `json:"company_uuid"`
	DepartmentUUID string                   `json:"department_uuid"`
	Items          []reorderInstructionItem `json:"items"`
}

type reorderInstructionItem struct {
	ID        string `json:"id"`
	SortOrder int    `json:"sort_order"`
}

func (h *Handler) Reorder(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	var body reorderInstructionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	scope, _, companyUUID, departmentUUID, err := parseInstructionPlacement(body.Scope, userID, body.CompanyUUID, body.DepartmentUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInstructionInput, "invalid instruction placement")
		return
	}

	items := make([]models.ReorderAnalysisInstructionItem, len(body.Items))
	for i, item := range body.Items {
		id, err := uuid.Parse(item.ID)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInstructionUUID, "invalid instruction uuid")
			return
		}
		items[i] = models.ReorderAnalysisInstructionItem{
			ID:        id,
			SortOrder: item.SortOrder,
		}
	}

	if err := h.service.Reorder(r.Context(), models.ReorderAnalysisInstructionsInput{
		Scope:          scope,
		UserUUID:       userID,
		CompanyUUID:    companyUUID,
		DepartmentUUID: departmentUUID,
		Items:          items,
	}); err != nil {
		writeInstructionError(w, err, response.CodeFailedToCreateInstruction, "failed to reorder instructions")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
