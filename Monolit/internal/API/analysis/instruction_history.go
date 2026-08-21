package analysis

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"verbatrace/monolit/internal/API/response"
)

func (h *Handler) ListAppliedInstructions(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	analysisID, err := uuid.Parse(chi.URLParam(r, "analysis_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid_analysis_uuid", "invalid analysis uuid")
		return
	}
	items, err := h.service.ListAppliedInstructions(r.Context(), analysisID, userID)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, "analysis_instructions_not_found", "analysis instructions not found")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (h *Handler) GetAppliedInstruction(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	analysisID, err := uuid.Parse(chi.URLParam(r, "analysis_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid_analysis_uuid", "invalid analysis uuid")
		return
	}
	versionID, err := uuid.Parse(chi.URLParam(r, "version_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid_instruction_version_uuid", "invalid instruction version uuid")
		return
	}
	item, err := h.service.GetAppliedInstruction(r.Context(), analysisID, versionID, userID)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, "analysis_instruction_not_found", "analysis instruction not found")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, item)
}
