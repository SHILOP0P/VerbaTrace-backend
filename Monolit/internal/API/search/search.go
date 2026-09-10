package search

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"verbatrace/monolit/internal/API/response"
	assistantservice "verbatrace/monolit/internal/assistant"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service"

	"github.com/google/uuid"
)

type Handler struct {
	service   service.SearchService
	assistant *assistantservice.Service
}

func (h *Handler) SetAssistant(service *assistantservice.Service) { h.assistant = service }

func NewHandler(service service.SearchService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	input, err := parseSearchInput(r, userID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidSearchInput, "invalid search input")
		return
	}

	result, err := h.service.Search(r.Context(), input)
	if err != nil {
		if errors.Is(err, models.ErrInvalidSearchInput) {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidSearchInput, "invalid search input")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToSearch, "failed to search")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, converter.SearchModelToAPI(result))
}

func parseSearchInput(r *http.Request, userID uuid.UUID) (models.SearchInput, error) {
	query := r.URL.Query()
	input := models.SearchInput{
		UserUUID: userID,
		Query:    query.Get("q"),
	}
	if types := query.Get("types"); types != "" {
		parts := strings.Split(types, ",")
		input.Types = make([]models.SearchType, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return models.SearchInput{}, models.ErrInvalidSearchInput
			}
			input.Types = append(input.Types, models.SearchType(part))
		}
	}
	if rawLimit := query.Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil {
			return models.SearchInput{}, models.ErrInvalidSearchInput
		}
		input.Limit = limit
	}
	return input, nil
}
