package company

import (
	"net/http"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/service"

	"github.com/google/uuid"
)

type Handler struct {
	service service.CompanyService
}

func NewCompanyHandler(service service.CompanyService) *Handler {
	return &Handler{service: service}
}

func userIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return middleware.UserIDFromContext(r.Context())
}
