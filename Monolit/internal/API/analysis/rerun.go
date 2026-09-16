package analysis

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

// RequestRerun lets an employee ask for a second analysis of a company call.
func (h *Handler) RequestRerun(w http.ResponseWriter, r *http.Request) {
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

	var request dto.CreateAnalysisRerunRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&request)
	}

	created, err := h.service.RequestRerun(r.Context(), models.CreateAnalysisRerunRequestInput{
		CallUUID: callUUID,
		UserUUID: userID,
		Reason:   request.Reason,
	})
	if err != nil {
		writeRerunError(w, err)
		return
	}

	_ = response.WriteJSON(w, http.StatusCreated, converter.AnalysisRerunRequestModelToAPI(created))
}

// DecideRerun approves or rejects the ask; approval starts the analysis.
func (h *Handler) DecideRerun(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	requestUUID, err := uuid.Parse(chi.URLParam(r, "request_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInput, "invalid request uuid")
		return
	}

	var body dto.DecideAnalysisRerunRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	decided, err := h.service.DecideRerun(r.Context(), models.DecideAnalysisRerunRequestInput{
		RequestUUID: requestUUID,
		UserUUID:    userID,
		Approve:     chi.URLParam(r, "decision") == "approve",
		Comment:     body.Comment,
	})
	if err != nil {
		writeRerunError(w, err)
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.AnalysisRerunRequestModelToAPI(decided))
}

// ListRerunRequests shows the queue to the people who decide on it.
func (h *Handler) ListRerunRequests(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyUUID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	items, err := h.service.ListRerunRequests(r.Context(), models.ListAnalysisRerunRequestsInput{
		CompanyUUID: companyUUID,
		UserUUID:    userID,
		Status:      models.AnalysisRerunRequestStatus(r.URL.Query().Get("status")),
	})
	if err != nil {
		writeRerunError(w, err)
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.AnalysisRerunRequestsModelToAPI(items))
}

func writeRerunError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrCallNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
	case errors.Is(err, models.ErrAnalysisRerunRequestNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeAnalysisRerunRequestNotFound, "analysis rerun request not found")
	case errors.Is(err, models.ErrAnalysisRerunRequestPending):
		response.WriteError(w, http.StatusConflict, response.CodeAnalysisRerunRequestPending, "Запрос на повторный анализ уже отправлен")
	case errors.Is(err, models.ErrAnalysisRerunForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeAnalysisRerunForbidden, "Перезапустить анализ может лидер отдела, заместитель или владелец")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrInvalidAnalysisInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInput, "invalid analysis input")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToAnalyzeCall, "failed to handle analysis rerun request")
	}
}
