package analytics

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) CreateAggregateReport(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	analysisID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAggregateReportInput, "invalid aggregate report input")
		return
	}
	var request dto.CreateAggregateReportRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAggregateReportInput, "invalid aggregate report input")
		return
	}
	report, err := h.service.CreateAggregateReport(r.Context(), models.CreateAggregateReportInput{
		AggregateAnalysisUUID: analysisID,
		UserUUID:              userID,
		Format:                models.ReportFormat(request.Format),
	})
	if err != nil {
		h.writeAggregateReportError(w, err, response.CodeFailedToCreateAggregateReport)
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, aggregateReportToAPI(report))
}

func (h *Handler) ListAggregateReports(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	analysisID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAggregateReportInput, "invalid aggregate report input")
		return
	}
	reports, err := h.service.ListAggregateReports(r.Context(), analysisID, userID)
	if err != nil {
		h.writeAggregateReportError(w, err, response.CodeFailedToListAggregateReports)
		return
	}
	items := make([]dto.AggregateReportResponse, 0, len(reports))
	for _, report := range reports {
		items = append(items, aggregateReportToAPI(report))
	}
	_ = response.WriteJSON(w, http.StatusOK, dto.ListAggregateReportsResponse{Reports: items})
}

func (h *Handler) DownloadAggregateReport(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	reportID, err := uuid.Parse(chi.URLParam(r, "report_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAggregateReportInput, "invalid aggregate report input")
		return
	}
	file, err := h.service.GetAggregateReportFile(r.Context(), reportID, userID)
	if err != nil {
		h.writeAggregateReportError(w, err, response.CodeFailedToDownloadAggregateReport)
		return
	}
	defer func() { _ = file.Content.Close() }()
	w.Header().Set("Content-Type", file.Report.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+file.Report.FileName+`"`)
	if file.Report.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Report.SizeBytes, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file.Content)
}

func (h *Handler) DeleteAggregateReport(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	reportID, err := uuid.Parse(chi.URLParam(r, "report_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAggregateReportInput, "invalid aggregate report input")
		return
	}
	if err := h.service.DeleteAggregateReport(r.Context(), reportID, userID); err != nil {
		h.writeAggregateReportError(w, err, response.CodeFailedToDeleteAggregateReport)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func aggregateReportToAPI(report models.AggregateReportExport) dto.AggregateReportResponse {
	var downloadURL *string
	if report.Status == models.ReportStatusReady {
		value := "/api/v1/analytics/deep-analysis-reports/" + report.ID.String() + "/download"
		downloadURL = &value
	}
	return dto.AggregateReportResponse{
		ID: report.ID.String(), AggregateAnalysisUUID: report.AggregateAnalysisUUID.String(),
		RequestedByUserUUID: report.RequestedByUserUUID.String(), Format: string(report.Format), Status: string(report.Status),
		FileName: report.FileName, ContentType: report.ContentType, SizeBytes: report.SizeBytes, ErrorMessage: report.ErrorMessage,
		DownloadURL: downloadURL, CreatedAt: report.CreatedAt.Format(time.RFC3339Nano),
		UpdatedAt: report.UpdatedAt.Format(time.RFC3339Nano), ExpiresAt: report.ExpiresAt.Format(time.RFC3339Nano),
	}
}

func (h *Handler) writeAggregateReportError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, models.ErrInvalidAggregateReportInput), errors.Is(err, models.ErrUnsupportedReportFormat):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAggregateReportInput, "invalid aggregate report input")
	case errors.Is(err, models.ErrInvalidAnalysisStatus):
		response.WriteError(w, http.StatusConflict, response.CodeInvalidAggregateAnalysisStatus, "invalid aggregate analysis status")
	case errors.Is(err, models.ErrAggregateAnalysisNotFound), errors.Is(err, models.ErrAggregateReportNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeAggregateReportNotFound, "aggregate report not found")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrReportNotReady):
		response.WriteError(w, http.StatusConflict, response.CodeReportNotReady, "report not ready")
	case errors.Is(err, models.ErrReportExpired), errors.Is(err, models.ErrAggregateReportFileNotFound):
		response.WriteError(w, http.StatusGone, response.CodeAggregateReportFileNotFound, "aggregate report file not found")
	default:
		response.WriteError(w, http.StatusBadGateway, fallback, "aggregate report operation failed")
	}
}
