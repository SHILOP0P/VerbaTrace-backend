package analysis_instruction

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) GetFile(w http.ResponseWriter, r *http.Request) {
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

	file, err := h.service.GetFile(r.Context(), id, userID)
	if err != nil {
		writeInstructionFileError(w, err)
		return
	}
	defer func() { _ = file.Content.Close() }()

	contentType := file.MimeType
	if contentType == "" {
		contentType = "text/markdown; charset=utf-8"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": safeDownloadFilename(file.OriginalFilename),
	}))

	if file.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
	}

	_, _ = io.Copy(w, file.Content)
}

func safeDownloadFilename(name string) string {
	base := filepath.Base(name)
	if base == "." || base == string(filepath.Separator) {
		return "instruction.md"
	}
	return base
}

func writeInstructionFileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrInvalidAnalysisInstructionInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidAnalysisInstructionInput, "invalid instruction input")
	case errors.Is(err, models.ErrAnalysisInstructionNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeAnalysisInstructionNotFound, "analysis instruction not found")
	case errors.Is(err, models.ErrInstructionFileNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeInstructionFileNotFound, "instruction file not found")
	case errors.Is(err, models.ErrCompanyNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCompanyNotFound, "company not found")
	case errors.Is(err, models.ErrDepartmentNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeDepartmentNotFound, "department not found")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetInstructionFile, "failed to get instruction file")
	}
}
