package integration

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service/supportaccess"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type SupportAccessService interface {
	Create(context.Context, models.CreateSupportAccessRequestInput) (models.SupportAccessRequest, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (models.SupportAccessRequest, error)
	Approve(context.Context, uuid.UUID, uuid.UUID, int64, string) (models.SupportAccessGrant, error)
	Deny(context.Context, uuid.UUID, uuid.UUID, int64, string) error
	Revoke(context.Context, uuid.UUID, uuid.UUID, string) error
}

func (h *Handler) CreateSupportAccessRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", false)
		return
	}
	if h.supportAccess == nil {
		writeError(w, http.StatusServiceUnavailable, "support_access_unavailable", true)
		return
	}
	var req struct {
		SubjectType      string     `json:"subject_type"`
		SubjectUserID    *uuid.UUID `json:"subject_user_uuid"`
		SubjectCompanyID *uuid.UUID `json:"subject_company_uuid"`
		Reason           string     `json:"reason"`
		Resources        []string   `json:"resources"`
		Commands         []string   `json:"commands"`
		DurationMinutes  int        `json:"duration_minutes"`
	}
	if err := decodeStrict(w, r, &req, 32<<10); err != nil {
		writeError(w, 400, "invalid_support_access_request", false)
		return
	}
	in := models.CreateSupportAccessRequestInput{RequesterUserID: actor, SubjectType: req.SubjectType, Reason: req.Reason, Resources: req.Resources, Commands: req.Commands, RequestedDurationMinutes: req.DurationMinutes}
	if req.SubjectUserID != nil {
		in.SubjectUserID = uuid.NullUUID{UUID: *req.SubjectUserID, Valid: true}
	}
	if req.SubjectCompanyID != nil {
		in.SubjectCompanyID = uuid.NullUUID{UUID: *req.SubjectCompanyID, Valid: true}
	}
	item, err := h.supportAccess.Create(r.Context(), in)
	if err != nil {
		writeSupportAccessError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) GetSupportAccessRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "request_uuid"))
	if err != nil {
		writeError(w, 404, "support_access_not_found", false)
		return
	}
	item, err := h.supportAccess.Get(r.Context(), id, actor)
	if err != nil {
		writeSupportAccessError(w, err)
		return
	}
	_ = response.WriteJSON(w, 200, item)
}

func (h *Handler) ApproveSupportAccessRequest(w http.ResponseWriter, r *http.Request) {
	h.decideSupportAccess(w, r, true)
}

func (h *Handler) DenySupportAccessRequest(w http.ResponseWriter, r *http.Request) {
	h.decideSupportAccess(w, r, false)
}

func (h *Handler) decideSupportAccess(w http.ResponseWriter, r *http.Request, approve bool) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "request_uuid"))
	if err != nil {
		writeError(w, 404, "support_access_not_found", false)
		return
	}
	version, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), "\""), 10, 64)
	if err != nil || version < 1 {
		writeError(w, http.StatusPreconditionRequired, "if_match_required", false)
		return
	}
	var req struct {
		Comment string `json:"comment"`
	}
	if err = decodeStrict(w, r, &req, 8<<10); err != nil {
		writeError(w, 400, "invalid_support_access_request", false)
		return
	}
	if approve {
		grant, serviceErr := h.supportAccess.Approve(r.Context(), id, actor, version, req.Comment)
		if serviceErr != nil {
			writeSupportAccessError(w, serviceErr)
			return
		}
		_ = response.WriteJSON(w, 200, grant)
		return
	}
	if err = h.supportAccess.Deny(r.Context(), id, actor, version, req.Comment); err != nil {
		writeSupportAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RevokeSupportAccessGrant(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, 401, "unauthorized", false)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "grant_uuid"))
	if err != nil {
		writeError(w, 404, "support_access_not_found", false)
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err = decodeStrict(w, r, &req, 8<<10); err != nil {
		writeError(w, 400, "invalid_support_access_request", false)
		return
	}
	if err = h.supportAccess.Revoke(r.Context(), id, actor, req.Reason); err != nil {
		writeSupportAccessError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeSupportAccessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, supportaccess.ErrInvalid):
		writeError(w, 400, "invalid_support_access_request", false)
	case errors.Is(err, supportaccess.ErrForbidden):
		writeError(w, 403, "support_access_forbidden", false)
	case errors.Is(err, supportaccess.ErrNotFound):
		writeError(w, 404, "support_access_not_found", false)
	case errors.Is(err, supportaccess.ErrConflict):
		writeError(w, 409, "support_access_conflict", false)
	default:
		writeError(w, 500, "support_access_failed", true)
	}
}
