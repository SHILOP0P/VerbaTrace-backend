package call

import (
	"net/http"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/service"
	"verbatrace/monolit/internal/service/transcriptionedit"

	"github.com/google/uuid"
)

type CallHandler struct {
	service service.CallService
	editor  *transcriptionedit.Service
}

func NewCallHandler(service service.CallService) *CallHandler {
	return &CallHandler{service: service}
}

func (h *CallHandler) SetTranscriptionEditor(editor *transcriptionedit.Service) { h.editor = editor }

func userIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return middleware.UserIDFromContext(r.Context())
}
