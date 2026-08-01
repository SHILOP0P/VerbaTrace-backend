package analysis_instruction

import (
	"net/http"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/service"

	"github.com/google/uuid"
)

type Handler struct {
	service service.AnalysisInstructionService
}

func NewHandler(service service.AnalysisInstructionService) *Handler {
	return &Handler{service: service}
}

func userIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return middleware.UserIDFromContext(r.Context())
}
