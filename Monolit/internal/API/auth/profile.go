package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
)

func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	var req dto.UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	user, err := h.service.UpdateProfile(r.Context(), models.UpdateUserProfileInput{
		UserUUID:    userID,
		FullName:    req.FullName,
		FullSurname: req.FullSurname,
		Post:        req.Headline,
		Phone:       req.Phone,
		Timezone:    req.Timezone,
	})
	if err != nil {
		writeProfileError(w, err, response.CodeFailedToGetUser, "failed to update profile")
		return
	}

	resp, err := converter.UserModelToAPI(user)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertUser, "failed to convert user")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidMultipartForm, "avatar file is required")
		return
	}
	defer func() { _ = file.Close() }()

	mimeType := header.Header.Get("Content-Type")
	result, err := h.service.UploadAvatar(r.Context(), models.SaveUserAvatarInput{
		UserUUID:         userID,
		OriginalFilename: header.Filename,
		MimeType:         mimeType,
		SizeBytes:        header.Size,
		Content:          file,
	})
	if err != nil {
		writeProfileError(w, err, response.CodeFailedToGetUser, "failed to upload avatar")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, dto.AvatarResponse{
		AvatarURL: result.AvatarURL,
		UpdatedAt: result.UpdatedAt.Format(time.RFC3339),
	})
}

func (h *AuthHandler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	result, err := h.service.DeleteAvatar(r.Context(), userID)
	if err != nil {
		writeProfileError(w, err, response.CodeFailedToGetUser, "failed to delete avatar")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, dto.AvatarResponse{
		AvatarURL: result.AvatarURL,
		UpdatedAt: result.UpdatedAt.Format(time.RFC3339),
	})
}

func (h *AuthHandler) GetAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	file, err := h.service.GetAvatar(r.Context(), userID)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, response.CodeUserNotFound, "avatar not found")
		return
	}
	defer func() { _ = file.Content.Close() }()
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = io.Copy(w, file.Content)
}

func writeProfileError(w http.ResponseWriter, err error, fallbackCode string, fallbackMessage string) {
	if errors.Is(err, models.ErrInvalidUserInput) {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidUserInput, "invalid user input")
		return
	}
	if errors.Is(err, models.ErrUserNotFound) {
		response.WriteError(w, http.StatusNotFound, response.CodeUserNotFound, "user not found")
		return
	}
	response.WriteError(w, http.StatusInternalServerError, fallbackCode, fallbackMessage)
}
