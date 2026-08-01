package analysis_instruction

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
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

	if err := h.service.Delete(r.Context(), id, userID); err != nil {
		writeInstructionError(w, err, response.CodeFailedToDeleteInstruction, "failed to delete instruction")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
