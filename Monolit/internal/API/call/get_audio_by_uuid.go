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

	variant := strings.TrimSpace(r.URL.Query().Get("variant"))
	if variant == "" {
		variant = "recommended"
	}
	var sessionID uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("access_session")); raw != "" {
		sessionID, _ = uuid.Parse(raw)
	}
	var audioFile models.File
	if h.privacy != nil {
		callModel, callErr := h.service.GetByUUID(r.Context(), callUUID, userID)
		if callErr != nil {
			if errors.Is(callErr, models.ErrCallNotFound) {
				response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
				return
			}
			response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToFindCall, "failed to find call")
			return
		}
		capabilities, capErr := h.privacy.Capabilities(r.Context(), callModel, userID)
		if capErr != nil {
			response.WriteError(w, 500, "privacy_internal_error", "Не удалось проверить доступ к записи")
			return
		}
		if variant == "recommended" {
			if capabilities.CanReadOriginalMedia {
				variant = "original"
			} else {
				variant = "redacted"
			}
		}
		if variant == "original" && !capabilities.CanReadOriginalMedia {
			response.WriteError(w, 403, "original_media_forbidden", "Доступ к оригинальной записи запрещён")
			return
		}
		if variant != "original" && variant != "redacted" {
			response.WriteError(w, 422, "privacy_policy_invalid", "Некорректный вариант записи")
			return
		}
		if sessionID != uuid.Nil {
			if sessionErr := h.privacy.ValidateMediaAccessSession(r.Context(), sessionID, callUUID, userID, variant); sessionErr != nil {
				response.WriteError(w, 403, "original_media_forbidden", "Медиасессия недействительна")
				return
			}
		}
		if variant == "redacted" {
			media, mediaErr := h.privacy.OpenSanitizedMedia(r.Context(), callModel, userID)
			if mediaErr != nil {
				writePrivacyError(w, mediaErr)
				return
			}
			audioFile = models.File{Content: media.File, ReadSeeker: media.File, OriginalFilename: media.Name, MimeType: media.MIMEType, SizeBytes: media.Size}
		} else {
			audioFile, err = h.service.GetAudioByUUID(r.Context(), callUUID, userID)
		}
		_ = h.privacy.AuditMediaOpen(r.Context(), callModel, userID, sessionID, variant)
	} else {
		audioFile, err = h.service.GetAudioByUUID(r.Context(), callUUID, userID)
	}
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
	w.Header().Set("Cache-Control", "private, no-store")
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
