package report

import (
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) ListByCallUUID(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	callUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}

	reports, err := h.service.ListByCallUUID(r.Context(), callUUID, userID)
	if err != nil {
		writeReportError(w, err, response.CodeFailedToListReports)
		return
	}

	resp, err := converter.ReportsModelToAPI(reports)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertReport, "failed to convert reports")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}
