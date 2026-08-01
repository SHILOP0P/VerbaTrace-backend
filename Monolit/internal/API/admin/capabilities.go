package admin

import (
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
)

func (h *Handler) GetCapabilities(w http.ResponseWriter, r *http.Request) {
	role, ok := middleware.UserRoleFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	capabilities, err := h.service.GetCapabilities(r.Context(), models.UserRole(role))
	if err != nil {
		if errors.Is(err, models.ErrForbidden) {
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
			return
		}

		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAdminCapabilities, "failed to get admin capabilities")
		return
	}

	permissions := make([]string, 0, len(capabilities.Permissions))
	for _, permission := range capabilities.Permissions {
		permissions = append(permissions, string(permission))
	}

	_ = response.WriteJSON(w, http.StatusOK, dto.AdminCapabilitiesResponse{
		Role:        string(capabilities.Role),
		Permissions: permissions,
	})
}
