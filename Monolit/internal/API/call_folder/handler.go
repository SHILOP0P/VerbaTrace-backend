package call_folder

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/converter"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	service service.CallFolderService
}

func NewHandler(service service.CallFolderService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	var req dto.CreateCallFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	input, err := createRequestToInput(req, userID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder input")
		return
	}
	folder, err := h.service.Create(r.Context(), input)
	if err != nil {
		writeFolderError(w, err, response.CodeFailedToCreateCallFolder)
		return
	}
	_ = response.WriteJSON(w, http.StatusCreated, converter.CallFolderModelToAPI(folder))
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	input, err := parseListInput(r, userID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder input")
		return
	}
	result, err := h.service.List(r.Context(), input)
	if err != nil {
		writeFolderError(w, err, response.CodeFailedToListCallFolders)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, converter.CallFoldersListModelToAPI(result))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	folder, err := h.service.Get(r.Context(), folderID, userID)
	if err != nil {
		writeFolderError(w, err, response.CodeFailedToListCallFolders)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, converter.CallFolderModelToAPI(folder))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	var req dto.UpdateCallFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	folder, err := h.service.Update(r.Context(), models.UpdateCallFolderInput{
		UserID:      userID,
		FolderUUID:  folderID,
		Name:        req.Name,
		Description: req.Description,
		Color:       req.Color,
	})
	if err != nil {
		writeFolderError(w, err, response.CodeFailedToUpdateCallFolder)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, converter.CallFolderModelToAPI(folder))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	if err := h.service.Delete(r.Context(), folderID, userID); err != nil {
		writeFolderError(w, err, response.CodeFailedToDeleteCallFolder)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AssignCall(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	var req dto.AssignCallToFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	callID, err := uuid.Parse(req.CallUUID)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call uuid")
		return
	}
	if err := h.service.AssignCall(r.Context(), models.AssignCallToFolderInput{UserID: userID, FolderUUID: folderID, CallUUID: callID}); err != nil {
		writeFolderError(w, err, response.CodeFailedToAssignCallFolder)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveCall(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "call_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call uuid")
		return
	}
	if err := h.service.RemoveCall(r.Context(), models.RemoveCallFromFolderInput{UserID: userID, FolderUUID: folderID, CallUUID: callID}); err != nil {
		writeFolderError(w, err, response.CodeFailedToRemoveCallFolder)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListCalls(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	limit, offset, err := parseLimitOffset(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder input")
		return
	}
	result, err := h.service.ListFolderCalls(r.Context(), models.ListFolderCallsInput{UserID: userID, FolderUUID: folderID, Limit: limit, Offset: offset})
	if err != nil {
		writeFolderError(w, err, response.CodeFailedToListCallFolders)
		return
	}
	resp, err := callsListResultToAPI(result)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToConvertCall, "failed to convert call")
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) GrantAccess(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	targetUserID, err := uuid.Parse(chi.URLParam(r, "user_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid user uuid")
		return
	}
	access, err := h.service.GrantAccess(r.Context(), models.GrantCallFolderAccessInput{UserID: userID, FolderUUID: folderID, TargetUserUUID: targetUserID})
	if err != nil {
		writeFolderError(w, err, response.CodeFailedToManageCallFolderAccess)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, accessToAPI(access))
}

func (h *Handler) RevokeAccess(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	targetUserID, err := uuid.Parse(chi.URLParam(r, "user_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid user uuid")
		return
	}
	if err := h.service.RevokeAccess(r.Context(), models.RevokeCallFolderAccessInput{UserID: userID, FolderUUID: folderID, TargetUserUUID: targetUserID}); err != nil {
		writeFolderError(w, err, response.CodeFailedToManageCallFolderAccess)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListAccesses(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	items, err := h.service.ListAccesses(r.Context(), folderID, userID)
	if err != nil {
		writeFolderError(w, err, response.CodeFailedToManageCallFolderAccess)
		return
	}
	responseItems := make([]dto.CallFolderAccessResponse, len(items))
	for i, item := range items {
		responseItems[i] = accessToAPI(item)
	}
	_ = response.WriteJSON(w, http.StatusOK, dto.CallFolderAccessesResponse{Items: responseItems})
}

func accessToAPI(access models.CallFolderAccess) dto.CallFolderAccessResponse {
	return dto.CallFolderAccessResponse{
		UserUUID: access.UserUUID.String(), GrantedByUserUUID: access.GrantedByUserUUID.String(), CreatedAt: access.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func callsListResultToAPI(result models.ListCallsResult) (dto.CallsListResponse, error) {
	items := make([]dto.CallResponse, len(result.Items))
	for i, call := range result.Items {
		callResponse, err := converter.CallModelToAPI(call)
		if err != nil {
			return dto.CallsListResponse{}, err
		}
		items[i] = callResponse
	}
	return dto.CallsListResponse{Items: items, Total: result.Total, Limit: result.Limit, Offset: result.Offset}, nil
}

func userIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return middleware.UserIDFromContext(r.Context())
}

func folderIDFromRequest(r *http.Request) (uuid.UUID, error) {
	return uuid.Parse(chi.URLParam(r, "folder_uuid"))
}

func createRequestToInput(req dto.CreateCallFolderRequest, userID uuid.UUID) (models.CreateCallFolderInput, error) {
	companyID, err := optionalUUID(req.CompanyUUID)
	if err != nil {
		return models.CreateCallFolderInput{}, err
	}
	departmentID, err := optionalUUID(req.DepartmentUUID)
	if err != nil {
		return models.CreateCallFolderInput{}, err
	}
	instructionIDs, err := parseUUIDs(req.InstructionUUIDs)
	if err != nil {
		return models.CreateCallFolderInput{}, err
	}
	return models.CreateCallFolderInput{
		UserID:         userID,
		Scope:          models.CallFolderScope(req.Scope),
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		Name:           req.Name,
		Description:    req.Description,
		Color:          req.Color,
		InstructionIDs: instructionIDs,
	}, nil
}

func (h *Handler) ReplaceInstructions(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	folderID, err := folderIDFromRequest(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder uuid")
		return
	}
	var body struct {
		InstructionUUIDs []string `json:"instruction_uuids"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	ids, err := parseUUIDs(body.InstructionUUIDs)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid instruction uuid")
		return
	}
	if err = h.service.ReplaceInstructions(r.Context(), userID, folderID, ids); err != nil {
		writeFolderError(w, err, response.CodeFailedToUpdateCallFolder)
		return
	}
	folder, err := h.service.Get(r.Context(), folderID, userID)
	if err != nil {
		writeFolderError(w, err, response.CodeFailedToUpdateCallFolder)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, converter.CallFolderModelToAPI(folder))
}

func parseUUIDs(values []string) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, nil
}

func parseListInput(r *http.Request, userID uuid.UUID) (models.ListCallFoldersInput, error) {
	query := r.URL.Query()
	companyID, err := optionalUUIDString(query.Get("company_uuid"))
	if err != nil {
		return models.ListCallFoldersInput{}, err
	}
	departmentID, err := optionalUUIDString(query.Get("department_uuid"))
	if err != nil {
		return models.ListCallFoldersInput{}, err
	}
	limit, offset, err := parseLimitOffset(r)
	if err != nil {
		return models.ListCallFoldersInput{}, err
	}
	return models.ListCallFoldersInput{
		UserID:         userID,
		Scope:          models.CallFolderScope(query.Get("scope")),
		CompanyUUID:    companyID,
		DepartmentUUID: departmentID,
		Q:              query.Get("q"),
		Limit:          limit,
		Offset:         offset,
	}, nil
}

func parseLimitOffset(r *http.Request) (int, int, error) {
	query := r.URL.Query()
	limit := 20
	offset := 0
	var err error
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, err
		}
	}
	if value := query.Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, err
		}
	}
	return limit, offset, nil
}

func optionalUUID(value *string) (uuid.NullUUID, error) {
	if value == nil {
		return uuid.NullUUID{}, nil
	}
	return optionalUUIDString(*value)
}

func optionalUUIDString(value string) (uuid.NullUUID, error) {
	if value == "" {
		return uuid.NullUUID{}, nil
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}, nil
}

func writeFolderError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, models.ErrInvalidCallFolderInput):
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallFolderInput, "invalid call folder input")
	case errors.Is(err, models.ErrCallFolderNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCallFolderNotFound, "call folder not found")
	case errors.Is(err, models.ErrCallFolderScopeMismatch):
		response.WriteError(w, http.StatusBadRequest, response.CodeCallFolderScopeMismatch, "call folder scope mismatch")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrCallNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, fallback, "call folder operation failed")
	}
}
