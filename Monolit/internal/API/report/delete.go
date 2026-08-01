package report

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	reportUUID, err := uuid.Parse(chi.URLParam(r, "report_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidReportInput, "invalid report uuid")
		return
	}

	if err := h.service.Delete(r.Context(), reportUUID, userID); err != nil {
		writeReportError(w, err, response.CodeFailedToDeleteReport)
		return
	}

	response.WriteNoContent(w)
}
