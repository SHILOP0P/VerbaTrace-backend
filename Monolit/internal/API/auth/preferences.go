package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (h *AuthHandler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	preferences, err := h.service.GetPreferences(r.Context(), userID)
	if err != nil {
		writePreferencesError(w, err, response.CodeFailedToGetUser, "failed to get preferences")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.PreferencesModelToAPI(preferences))
}

func (h *AuthHandler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	var req dto.UpdatePreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	input := models.UpdateUserPreferencesInput{
		UserUUID:         userID,
		Theme:            req.Theme,
		InvitationsMuted: req.InvitationsMuted,
	}
	if req.ActiveCompanyUUID != nil {
		if *req.ActiveCompanyUUID == "" {
			input.ActiveCompanyUUID = &uuid.NullUUID{}
		} else {
			companyID, err := uuid.Parse(*req.ActiveCompanyUUID)
			if err != nil {
				response.WriteError(w, http.StatusBadRequest, response.CodeInvalidUserInput, "invalid company uuid")
				return
			}
			input.ActiveCompanyUUID = &uuid.NullUUID{UUID: companyID, Valid: true}
		}
	}
	if req.DateRange != nil {
		input.DateRange = &models.PreferencesDateRange{
			From: req.DateRange.From,
			To:   req.DateRange.To,
		}
	}

	preferences, err := h.service.UpdatePreferences(r.Context(), input)
	if err != nil {
		writePreferencesError(w, err, response.CodeFailedToGetUser, "failed to update preferences")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.PreferencesModelToAPI(preferences))
}

func writePreferencesError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	if errors.Is(err, models.ErrInvalidUserInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidUserInput, "invalid preferences")
		return
	}
	if errors.Is(err, models.ErrCompanyNotFound) || errors.Is(err, models.ErrForbidden) {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "company is not visible")
		return
	}
	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
