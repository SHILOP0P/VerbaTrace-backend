package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// GrowthAreas hides and brings back growth areas.
type GrowthAreas interface {
	Dismiss(ctx context.Context, actor, areaID uuid.UUID, reason string) error
	Reopen(ctx context.Context, actor, areaID uuid.UUID) error
}

func (h *Handler) SetGrowth(growth GrowthAreas) { h.growth = growth }

// GetEmployeeGrowthAreas serves /analytics/employees/{user_uuid}/growth-areas.
func (h *Handler) GetEmployeeGrowthAreas(w http.ResponseWriter, r *http.Request) {
	req, ok := h.teamRequest(w, r)
	if !ok {
		return
	}
	target := uuid.Nil
	if raw := chi.URLParam(r, "user_uuid"); raw != "me" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalyticsFilter, "invalid user uuid")
			return
		}
		target = parsed
	}
	value, err := h.team.EmployeeGrowthAreas(r.Context(), req, target, r.URL.Query().Get("status"))
	respondTeam(w, value, err)
}

// DismissGrowthArea is POST /growth-areas/{area_uuid}/dismiss with a reason.
func (h *Handler) DismissGrowthArea(w http.ResponseWriter, r *http.Request) {
	userID, areaID, ok := h.growthTarget(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	writeGrowthResult(w, h.growth.Dismiss(r.Context(), userID, areaID, body.Reason))
}

// ReopenGrowthArea is POST /growth-areas/{area_uuid}/reopen.
func (h *Handler) ReopenGrowthArea(w http.ResponseWriter, r *http.Request) {
	userID, areaID, ok := h.growthTarget(w, r)
	if !ok {
		return
	}
	writeGrowthResult(w, h.growth.Reopen(r.Context(), userID, areaID))
}

func (h *Handler) growthTarget(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return uuid.Nil, uuid.Nil, false
	}
	if h.growth == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "growth areas are not configured")
		return uuid.Nil, uuid.Nil, false
	}
	areaID, err := uuid.Parse(chi.URLParam(r, "area_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusNotFound, response.CodeGrowthAreaNotFound, "growth area not found")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, areaID, true
}

func writeGrowthResult(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, models.ErrGrowthAreaNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeGrowthAreaNotFound, "growth area not found")
	case errors.Is(err, models.ErrInvalidGrowthAreaReason):
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeInvalidGrowthAreaReason, "Укажите причину, по которой зона скрывается")
	case errors.Is(err, models.ErrGrowthAreaNotDismissed):
		response.WriteError(w, http.StatusConflict, response.CodeGrowthAreaNotDismissed, "Зона не скрыта")
	case errors.Is(err, models.ErrCompanyFrozen):
		response.WriteError(w, http.StatusConflict, response.CodeCompanyFrozen, "company is frozen")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeInternalServerError, "failed to change growth area")
	}
}
