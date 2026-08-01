package call

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func (h *CallHandler) GetAudioByUUID(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}

	rawUUID := chi.URLParam(r, "uuid")

	callUUID, err := uuid.Parse(rawUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call UUID")
		return
	}

	audioFile, err := h.service.GetAudioByUUID(r.Context(), callUUID, userID)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrCallNotFound):
			response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
			return
		case errors.Is(err, models.ErrForbidden):
			response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
			return
		case errors.Is(err, models.ErrAudioFileNotFound), errors.Is(err, os.ErrNotExist):
			response.WriteError(w, http.StatusGone, response.CodeAudioFileNotFound, "audio file not found")
			return
		}
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAudio, "error getting audio file")
		return
	}

	if audioFile.Content != nil {
		defer func() { _ = audioFile.Content.Close() }()
	}

	if audioFile.ReadSeeker == nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetAudio, "audio file does not support seeking")
		return
	}

	contentType := audioFile.MimeType

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{
		"filename": safeAudioFilename(audioFile.OriginalFilename),
	}))

	http.ServeContent(w, r, safeAudioFilename(audioFile.OriginalFilename), time.Time{}, audioFile.ReadSeeker)
}

func safeAudioFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = path.Base(filename)

	if filename == "" || filename == "." || filename == "/" {
		return "audio"
	}

	return filename
}
