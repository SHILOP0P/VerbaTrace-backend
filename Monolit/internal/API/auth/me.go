package auth

import (
	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	model "verbatrace/monolit/internal/models"

	"errors"
	"net/http"
)

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	user, err := h.service.Me(r.Context(), userID)
	if err != nil {
		if errors.Is(err, model.ErrUserNotFound) {
			response.WriteError(w, http.StatusNotFound, response.CodeUserNotFound, "user not found")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetUser, "failed to get user")
		return
	}

	userResponse, err := converter.UserModelToAPI(user)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertUser, "failed to convert user")
		return
	}

	resp := dto.UserResponse{
		ID:          userResponse.ID,
		Email:       userResponse.Email,
		FullName:    userResponse.FullName,
		FullSurname: userResponse.FullSurname,
		Username:    userResponse.Username,
		Role:        userResponse.Role,
		Headline:    userResponse.Headline,
		Phone:       userResponse.Phone,
		Timezone:    userResponse.Timezone,
		AvatarURL:   userResponse.AvatarURL,
		CreatedAt:   userResponse.CreatedAt,
	}

	if err := response.WriteJSON(w, http.StatusOK, resp); err != nil {
		return
	}
}
