package analysis_instruction

import (
	"io"
	"mime"
	"net/http"
	"strconv"

	"verbatrace/monolit/internal/API/response"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *Handler) ListVersions(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := parseInstructionUUID(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInstructionUUID, "invalid instruction uuid")
		return
	}
	items, err := h.service.ListVersions(r.Context(), id, userID)
	if err != nil {
		writeInstructionError(w, err, response.CodeFailedToListInstructions, "failed to list instruction versions")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) GetVersionFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	id, err := parseInstructionUUID(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInstructionUUID, "invalid instruction uuid")
		return
	}
	versionID, err := uuid.Parse(chi.URLParam(r, "version_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInstructionUUID, "invalid instruction version uuid")
		return
	}
	file, err := h.service.GetVersionFile(r.Context(), id, versionID, userID)
	if err != nil {
		writeInstructionFileError(w, err)
		return
	}
	defer func() { _ = file.Content.Close() }()
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": safeDownloadFilename(file.OriginalFilename)}))
	if file.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
	}
	_, _ = io.Copy(w, file.Content)
}
