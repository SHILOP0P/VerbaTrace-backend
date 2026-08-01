package analysis_instruction

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	id, err := parseInstructionUUID(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInstructionUUID, "invalid instruction uuid")
		return
	}

	instruction, err := h.service.Get(r.Context(), id, userID)
	if err != nil {
		writeInstructionError(w, err, response.CodeAnalysisInstructionNotFound, "failed to get instruction")
		return
	}

	resp, err := converter.AnalysisInstructionModelToAPI(instruction)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInstruction, "failed to convert instruction")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}
