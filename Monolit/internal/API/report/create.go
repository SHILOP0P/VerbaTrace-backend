package report

import (
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
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

	var req dto.CreateReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	report, err := h.service.Create(r.Context(), models.CreateReportInput{
		CallUUID: callUUID,
		UserUUID: userID,
		Format:   models.ReportFormat(req.Format),
	})
	if err != nil {
		writeReportError(w, err, response.CodeFailedToCreateReport)
		return
	}

	resp, err := converter.ReportModelToAPI(report)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertReport, "failed to convert report")
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, resp)
}

func writeReportError(w http.ResponseWriter, err error, fallbackCode string) {
	if errors.Is(err, models.ErrCallNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
		return
	}
	if errors.Is(err, models.ErrAnalysisNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeAnalysisNotFound, "analysis not found")
		return
	}
	if errors.Is(err, models.ErrReportNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeReportNotFound, "report not found")
		return
	}
	if errors.Is(err, models.ErrReportAlreadyExists) {
		response.WriteError(w, http.StatusConflict, response.CodeReportAlreadyExists, "report already exists")
		return
	}
	if errors.Is(err, models.ErrUnsupportedReportFormat) {
		response.WriteError(w, http.StatusBadRequest, response.CodeUnsupportedReportFormat, "unsupported report format")
		return
	}
	if errors.Is(err, models.ErrUnsupportedReportScope) {
		response.WriteError(w, http.StatusBadRequest, response.CodeUnsupportedReportScope, "unsupported report scope")
		return
	}
	if errors.Is(err, models.ErrInvalidReportInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidReportInput, "invalid report input")
		return
	}
	if errors.Is(err, models.ErrReportScopeNotImplemented) {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "report scope is not implemented")
		return
	}
	if errors.Is(err, models.ErrInvalidAnalysisStatus) {
		response.WriteError(w, http.StatusConflict, response.CodeInvalidAnalysisStatus, "analysis is not ready")
		return
	}
	if errors.Is(err, models.ErrSubscriptionRequired) {
		response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
		return
	}
	if errors.Is(err, models.ErrExportAccessDenied) {
		response.WriteError(w, http.StatusForbidden, response.CodeExportAccessDenied, "export access denied")
		return
	}
	if errors.Is(err, models.ErrForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		return
	}
	if errors.Is(err, models.ErrReportNotReady) {
		response.WriteError(w, http.StatusConflict, response.CodeReportNotReady, "report not ready")
		return
	}
	if errors.Is(err, models.ErrReportExpired) {
		response.WriteError(w, http.StatusGone, response.CodeReportExpired, "report expired")
		return
	}
	if errors.Is(err, models.ErrReportFileNotFound) {
		response.WriteError(w, http.StatusGone, response.CodeReportFileNotFound, "report file not found")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, fallbackCode, "report operation failed")
}
