package report

import (
	"fmt"
	"io"
	"net/http"
	"net/url"

	"verbatrace/monolit/internal/API/response"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
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

	file, err := h.service.GetFile(r.Context(), reportUUID, userID)
	if err != nil {
		writeReportError(w, err, response.CodeFailedToDownloadReport)
		return
	}
	defer func() { _ = file.Content.Close() }()

	w.Header().Set("Content-Type", file.Report.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", file.Report.SizeBytes))
	w.Header().Set("Content-Disposition", contentDisposition(file.Report.FileName))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file.Content)
}

func contentDisposition(fileName string) string {
	escaped := url.PathEscape(fileName)
	return fmt.Sprintf("attachment; filename*=UTF-8''%s", escaped)
}
