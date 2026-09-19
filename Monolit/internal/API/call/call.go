package call

import (
	"net/http"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/service"
	privacyservice "verbatrace/monolit/internal/service/privacy"
	"verbatrace/monolit/internal/service/transcriptionedit"

	"github.com/google/uuid"
)

type CallHandler struct {
	service service.CallService
	editor  *transcriptionedit.Service
	privacy *privacyservice.Service
	// access and subjects fill a single call's response; both are optional.
	access   CallAccessReader
	subjects CallSubjectsService
	speech   CallSpeechReader
}

func NewCallHandler(service service.CallService) *CallHandler {
	return &CallHandler{service: service}
}

func (h *CallHandler) SetTranscriptionEditor(editor *transcriptionedit.Service) { h.editor = editor }
func (h *CallHandler) SetPrivacyService(service *privacyservice.Service)        { h.privacy = service }

func userIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return middleware.UserIDFromContext(r.Context())
}
