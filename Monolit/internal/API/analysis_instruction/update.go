package analysis_instruction

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"

	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
)

type updateInstructionRequest struct {
	Title     *string `json:"title"`
	IsActive  *bool   `json:"is_active"`
	SortOrder *int    `json:"sort_order"`
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
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

	var body updateInstructionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}

	updated, err := h.service.Update(r.Context(), models.UpdateAnalysisInstructionInput{
		ID:        id,
		UserUUID:  userID,
		Title:     body.Title,
		IsActive:  body.IsActive,
		SortOrder: body.SortOrder,
	})
	if err != nil {
		writeInstructionError(w, err, response.CodeFailedToCreateInstruction, "failed to update instruction")
		return
	}

	resp, err := converter.AnalysisInstructionModelToAPI(updated)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInstruction, "failed to convert instruction")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) ReplaceFile(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 1 << 20

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

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidMultipartForm, "failed to parse multipart form")
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInstructionFileRequired, "instruction file is required")
		return
	}
	defer func() { _ = file.Close() }()

	if filepath.Ext(fileHeader.Filename) == "" {
		response.WriteError(w, http.StatusBadRequest, response.CodeInstructionFileExtensionRequired, "instruction file extension is required")
		return
	}

	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInstructionFileReadFailed, "failed to read file")
		return
	}

	detectedMimeType := http.DetectContentType(buffer[:n])
	fileContent := io.MultiReader(bytes.NewReader(buffer[:n]), file)
	updated, err := h.service.ReplaceFile(r.Context(), models.ReplaceAnalysisInstructionFileInput{
		ID:               id,
		UserUUID:         userID,
		OriginalFilename: fileHeader.Filename,
		MimeType:         detectedMimeType,
		SizeBytes:        fileHeader.Size,
		Content:          fileContent,
	})
	if err != nil {
		writeInstructionError(w, err, response.CodeFailedToCreateInstruction, "failed to replace instruction file")
		return
	}

	resp, err := converter.AnalysisInstructionModelToAPI(updated)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertInstruction, "failed to convert instruction")
		return
	}

	_ = response.WriteJSON(w, http.StatusOK, resp)
}
