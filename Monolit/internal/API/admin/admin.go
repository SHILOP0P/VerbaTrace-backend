package admin

import (
	"context"
	"net/http"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/service"

	"github.com/google/uuid"
)

type supportAccessAuthorizer interface {
	AuthorizedSubjects(context.Context, uuid.UUID, string) ([]uuid.UUID, []uuid.UUID, error)
	AuthorizeUser(context.Context, uuid.UUID, uuid.UUID, string, string) error
	AuthorizeCompany(context.Context, uuid.UUID, uuid.UUID, string, string) error
	AuthorizeCall(context.Context, uuid.UUID, uuid.UUID, string, string) error
}

type Handler struct {
	service           service.AdminService
	supportAuthorizer supportAccessAuthorizer
}

func (h *Handler) SetSupportAccessAuthorizer(authorizer supportAccessAuthorizer) {
	h.supportAuthorizer = authorizer
}

func (h *Handler) authorizeUser(w http.ResponseWriter, r *http.Request, id uuid.UUID, resource string) bool {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok || h.supportAuthorizer == nil || h.supportAuthorizer.AuthorizeUser(r.Context(), actor, id, resource, "") != nil {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "temporary support access is required")
		return false
	}
	return true
}

func (h *Handler) authorizeCompany(w http.ResponseWriter, r *http.Request, id uuid.UUID, resource string) bool {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok || h.supportAuthorizer == nil || h.supportAuthorizer.AuthorizeCompany(r.Context(), actor, id, resource, "") != nil {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "temporary support access is required")
		return false
	}
	return true
}

func (h *Handler) authorizeCall(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	actor, ok := middleware.UserIDFromContext(r.Context())
	if !ok || h.supportAuthorizer == nil || h.supportAuthorizer.AuthorizeCall(r.Context(), actor, id, "calls", "") != nil {
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "temporary support access is required")
		return false
	}
	return true
}

func NewHandler(adminService service.AdminService) *Handler {
	return &Handler{service: adminService}
}
