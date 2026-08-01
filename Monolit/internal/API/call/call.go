package call

import (
	"net/http"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/service"

	"github.com/google/uuid"
)

type CallHandler struct {
	service service.CallService
}

func NewCallHandler(service service.CallService) *CallHandler {
	return &CallHandler{service: service}
}

func userIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return middleware.UserIDFromContext(r.Context())
}
