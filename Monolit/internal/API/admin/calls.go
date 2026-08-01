package admin

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) GetCall(w http.ResponseWriter, r *http.Request) {
	id, ok := adminCallID(w, r)
	if !ok {
		return
	}
	call, err := h.service.GetCall(r.Context(), id)
	if err != nil {
		writeAdminCallError(w, err)
		return
	}
	resp, _ := converter.CallModelToAPI(call)
	resp.AudioURL = "/api/v1/admin/calls/" + id.String() + "/audio"
	_ = response.WriteJSON(w, http.StatusOK, resp)
	h.auditCall(r, "call.details_viewed", id)
}
func (h *Handler) GetCallAudio(w http.ResponseWriter, r *http.Request) {
	id, ok := adminCallID(w, r)
	if !ok {
		return
	}
	file, err := h.service.GetCallAudio(r.Context(), id)
	if err != nil {
		writeAdminCallError(w, err)
		return
	}
	if file.Content != nil {
		defer func() { _ = file.Content.Close() }()
	}
	if file.ReadSeeker == nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAudio, "audio cannot be read")
		return
	}
	mimeType := file.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	name := safeAdminAudioName(file.OriginalFilename)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	h.auditCall(r, "call.audio_accessed", id)
	http.ServeContent(w, r, name, time.Time{}, file.ReadSeeker)
}
func (h *Handler) auditCall(r *http.Request, action string, id uuid.UUID) {
	actor, ok := middleware.UserIDFromContext(r.Context())
	role, roleOK := middleware.UserRoleFromContext(r.Context())
	if !ok || !roleOK {
		return
	}
	_, _ = h.service.RecordAudit(r.Context(), models.CreateAdminAuditLogInput{ActorUserUUID: actor, ActorRole: models.UserRole(role), Action: action, TargetType: "call", TargetUUID: uuid.NullUUID{UUID: id, Valid: true}, RequestID: adminMetadata(r, "").RequestID, IPAddress: adminMetadata(r, "").IPAddress, UserAgent: adminMetadata(r, "").UserAgent})
}
func adminCallID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "call_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return uuid.Nil, false
	}
	return id, true
}
func writeAdminCallError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrCallNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
	case errors.Is(err, os.ErrNotExist), errors.Is(err, models.ErrAudioFileNotFound):
		response.WriteError(w, http.StatusGone, response.CodeAudioFileNotFound, "audio file not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAudio, "failed to get call")
	}
}
func safeAdminAudioName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = path.Base(name)
	if name == "" || name == "." || name == "/" {
		return "audio"
	}
	return name
}
