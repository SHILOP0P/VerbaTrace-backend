package company

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

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	var req dto.UpdateCompanyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid request body")
		return
	}

	company, err := h.service.UpdateCompany(r.Context(), models.UpdateCompanyInput{
		CompanyUUID: companyID,
		RequestUser: userID,
		Name:        req.Name,
	})
	if err != nil {
		writeCompanyError(w, err, response.CodeFailedToConvertCompany, "failed to update company")
		return
	}

	resp, err := converter.CompanyModelToAPI(company)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateTag(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}
	var req dto.UpdateCompanyTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid request body")
		return
	}
	company, err := h.service.UpdateCompanyTag(r.Context(), models.UpdateCompanyTagInput{CompanyUUID: companyID, RequestUser: userID, Tag: req.Tag})
	if err != nil {
		writeCompanyError(w, err, response.CodeFailedToConvertCompany, "failed to update company tag")
		return
	}
	resp, err := converter.CompanyModelToAPI(company)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCompany, "failed to convert company")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	companyID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company uuid")
		return
	}

	err = h.service.DeleteCompany(r.Context(), models.DeleteCompanyInput{
		CompanyUUID: companyID,
		RequestUser: userID,
	})
	if err != nil {
		writeCompanyError(w, err, response.CodeFailedToConvertCompany, "failed to delete company")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeCompanyError(w http.ResponseWriter, err error, code string, message string) {
	if errors.Is(err, models.ErrInvalidCompanyInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCompanyInput, "invalid company input")
		return
	}
	if errors.Is(err, models.ErrCompanyNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
		return
	}
	if errors.Is(err, models.ErrCompanyTagAlreadyExists) {
		response.WriteError(w, http.StatusConflict, response.CodeCompanyTagAlreadyExists, "company tag already exists")
		return
	}
	if errors.Is(err, models.ErrForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
		return
	}
	if errors.Is(err, models.ErrSubscriptionRequired) {
		response.WriteError(w, http.StatusPaymentRequired, response.CodeSubscriptionRequired, "subscription required")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, code, message)
}
