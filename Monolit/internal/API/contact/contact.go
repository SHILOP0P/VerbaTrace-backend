package contact

import (
	"errors"
	"net/http"

	"calllens/monolit/internal/API/dto"
	"calllens/monolit/internal/API/response"
	"calllens/monolit/internal/converter"
	"calllens/monolit/internal/httpserver/middleware"
	"calllens/monolit/internal/models"
	"calllens/monolit/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct{ service service.ContactService }

func NewHandler(service service.ContactService) *Handler { return &Handler{service: service} }

func (h *Handler) SearchContacts(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	users, err := h.service.SearchContacts(r.Context(), userID, r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, usersToAPI(users))
}

func (h *Handler) ListContacts(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	items, err := h.service.ListContacts(r.Context(), userID)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, usersToAPI(items.Users))
}
func (h *Handler) AddContact(w http.ResponseWriter, r *http.Request)    { h.changeContact(w, r, true) }
func (h *Handler) RemoveContact(w http.ResponseWriter, r *http.Request) { h.changeContact(w, r, false) }
func (h *Handler) ListFavoriteCalls(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	items, err := h.service.ListFavoriteCalls(r.Context(), userID)
	if err != nil {
		writeError(w, err)
		return
	}
	result := make([]dto.CallResponse, 0, len(items.Calls))
	for _, call := range items.Calls {
		value, err := converter.CallModelToAPI(call)
		if err != nil {
			writeError(w, err)
			return
		}
		result = append(result, value)
	}
	_ = response.WriteJSON(w, http.StatusOK, result)
}
func (h *Handler) AddFavoriteCall(w http.ResponseWriter, r *http.Request) {
	h.changeFavoriteCall(w, r, true)
}
func (h *Handler) RemoveFavoriteCall(w http.ResponseWriter, r *http.Request) {
	h.changeFavoriteCall(w, r, false)
}

func (h *Handler) changeContact(w http.ResponseWriter, r *http.Request, add bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	contactID, err := uuid.Parse(chi.URLParam(r, "user_uuid"))
	if err != nil {
		invalid(w)
		return
	}
	input := models.AddContactInput{UserID: userID, ContactID: contactID}
	if add {
		err = h.service.AddContact(r.Context(), input)
	} else {
		err = h.service.RemoveContact(r.Context(), input)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) changeFavoriteCall(w http.ResponseWriter, r *http.Request, add bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "call_uuid"))
	if err != nil {
		invalid(w)
		return
	}
	input := models.FavoriteCallInput{UserID: userID, CallID: callID}
	if add {
		err = h.service.AddFavoriteCall(r.Context(), input)
	} else {
		err = h.service.RemoveFavoriteCall(r.Context(), input)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func usersToAPI(users []models.User) []dto.UserResponse {
	result := make([]dto.UserResponse, 0, len(users))
	for _, user := range users {
		item, err := converter.UserModelToAPI(user)
		if err == nil {
			result = append(result, item)
		}
	}
	return result
}
func unauthorized(w http.ResponseWriter) {
	response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
}
func invalid(w http.ResponseWriter) {
	response.WriteError(w, http.StatusBadRequest, response.CodeInvalidContactInput, "invalid contact input")
}
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrUserNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeUserNotFound, "user not found")
	case errors.Is(err, models.ErrCallNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
	case errors.Is(err, models.ErrInvalidContactInput):
		invalid(w)
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToManageContacts, "failed to manage contacts")
	}
}
