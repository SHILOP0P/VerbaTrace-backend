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
)

func (h *AuthHandler) UpdateUsername(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	var req dto.UpdateUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	user, err := h.service.UpdateUsername(r.Context(), models.UpdateUsernameInput{
		UserUUID: userID,
		Username: req.Username,
	})
	if err != nil {
		writeUsernameError(w, err, response.CodeFailedToGetUser, "failed to update username")
		return
	}

	resp, err := converter.UserModelToAPI(user)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertUser, "failed to convert user")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) LookupUser(w http.ResponseWriter, r *http.Request) {
	user, err := h.service.GetUserByUsername(r.Context(), r.URL.Query().Get("username"))
	if err != nil {
		writeUsernameError(w, err, response.CodeFailedToGetUser, "failed to lookup user")
		return
	}

	resp, err := converter.UserModelToAPI(user)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertUser, "failed to convert user")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func writeUsernameError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	if errors.Is(err, models.ErrInvalidUserInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidUserInput, "invalid username")
		return
	}
	if errors.Is(err, models.ErrUserAlreadyExists) {
		response.WriteError(w, http.StatusConflict, response.CodeUserAlreadyExists, "username already exists")
		return
	}
	if errors.Is(err, models.ErrUserNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeUserNotFound, "user not found")
		return
	}

	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
