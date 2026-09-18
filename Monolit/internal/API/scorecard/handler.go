// Package scorecard serves the scorecard of an analysis instruction: the
// criteria calls are scored on, compiled once per instruction version.
package scorecard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Service interface {
	Get(ctx context.Context, instructionID, userID uuid.UUID) (models.Scorecard, error)
	GetForVersion(ctx context.Context, instructionID, versionID, userID uuid.UUID) (models.Scorecard, error)
	Edit(ctx context.Context, input models.EditScorecardInput) (models.Scorecard, error)
	Ensure(ctx context.Context, instructionID, userID uuid.UUID) (models.Scorecard, error)
	Recompile(ctx context.Context, instructionID, userID uuid.UUID) (models.Scorecard, error)
	Confirm(ctx context.Context, instructionID, scorecardID, userID uuid.UUID, lockVersion int) (models.Scorecard, error)
	SameAs(ctx context.Context, instructionID, userID, criterionKey, canonicalKey uuid.UUID) (models.Scorecard, error)
	Split(ctx context.Context, instructionID, userID, criterionKey uuid.UUID) (models.Scorecard, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID, instructionID, ok := identify(w, r)
	if !ok {
		return
	}
	card, err := h.service.Get(r.Context(), instructionID, userID)
	respond(w, card, err)
}

func (h *Handler) GetForVersion(w http.ResponseWriter, r *http.Request) {
	userID, instructionID, ok := identify(w, r)
	if !ok {
		return
	}
	versionID, err := uuid.Parse(chi.URLParam(r, "version_uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid version uuid")
		return
	}
	card, err := h.service.GetForVersion(r.Context(), instructionID, versionID, userID)
	respond(w, card, err)
}

func (h *Handler) Edit(w http.ResponseWriter, r *http.Request) {
	userID, instructionID, ok := identify(w, r)
	if !ok {
		return
	}
	var body dto.EditScorecardRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	input := models.EditScorecardInput{InstructionID: instructionID, UserID: userID, LockVersion: body.LockVersion, ConfirmRequired: body.ConfirmRequired}
	for _, item := range body.Criteria {
		key, err := uuid.Parse(item.CriterionKey)
		if err != nil {
			response.WriteError(w, http.StatusUnprocessableEntity, response.CodeScorecardInvalid, "invalid criterion key")
			return
		}
		input.Criteria = append(input.Criteria, models.ScorecardCriterionEdit{Key: key, Title: item.Title, Weight: item.Weight, IsCritical: item.IsCritical, Enabled: item.Enabled})
	}
	card, err := h.service.Edit(r.Context(), input)
	respond(w, card, err)
}

func (h *Handler) Ensure(w http.ResponseWriter, r *http.Request) {
	userID, instructionID, ok := identify(w, r)
	if !ok {
		return
	}
	card, err := h.service.Ensure(r.Context(), instructionID, userID)
	respond(w, card, err)
}

func (h *Handler) Recompile(w http.ResponseWriter, r *http.Request) {
	userID, instructionID, ok := identify(w, r)
	if !ok {
		return
	}
	card, err := h.service.Recompile(r.Context(), instructionID, userID)
	respond(w, card, err)
}

func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	userID, instructionID, ok := identify(w, r)
	if !ok {
		return
	}
	var body dto.ConfirmScorecardRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	scorecardID, err := uuid.Parse(body.ScorecardUUID)
	if err != nil {
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeScorecardInvalid, "invalid scorecard uuid")
		return
	}
	card, err := h.service.Confirm(r.Context(), instructionID, scorecardID, userID, body.LockVersion)
	respond(w, card, err)
}

func (h *Handler) SameAs(w http.ResponseWriter, r *http.Request) {
	userID, instructionID, ok := identify(w, r)
	if !ok {
		return
	}
	criterionKey, err := uuid.Parse(chi.URLParam(r, "criterion_key"))
	if err != nil {
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeScorecardInvalid, "invalid criterion key")
		return
	}
	var body dto.CriterionSameAsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	canonicalKey, err := uuid.Parse(body.CanonicalKey)
	if err != nil {
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeScorecardInvalid, "invalid canonical key")
		return
	}
	card, err := h.service.SameAs(r.Context(), instructionID, userID, criterionKey, canonicalKey)
	respond(w, card, err)
}

func (h *Handler) Split(w http.ResponseWriter, r *http.Request) {
	userID, instructionID, ok := identify(w, r)
	if !ok {
		return
	}
	criterionKey, err := uuid.Parse(chi.URLParam(r, "criterion_key"))
	if err != nil {
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeScorecardInvalid, "invalid criterion key")
		return
	}
	card, err := h.service.Split(r.Context(), instructionID, userID, criterionKey)
	respond(w, card, err)
}

func identify(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return uuid.Nil, uuid.Nil, false
	}
	instructionID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidInstructionUUID, "invalid instruction uuid")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, instructionID, true
}

func respond(w http.ResponseWriter, card models.Scorecard, err error) {
	if err != nil {
		writeError(w, err)
		return
	}
	_ = response.WriteJSON(w, http.StatusOK, toResponse(card))
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrScorecardNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeScorecardNotFound, "scorecard not found")
	case errors.Is(err, models.ErrAnalysisInstructionNotFound), errors.Is(err, models.ErrCompanyNotFound), errors.Is(err, models.ErrDepartmentNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeAnalysisInstructionNotFound, "analysis instruction not found")
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "forbidden")
	case errors.Is(err, models.ErrScorecardNotReady):
		response.WriteError(w, http.StatusConflict, response.CodeScorecardNotReady, "scorecard is not ready")
	case errors.Is(err, models.ErrScorecardVersionConflict):
		response.WriteError(w, http.StatusConflict, response.CodeScorecardVersionConflict, "scorecard was changed by someone else")
	case errors.Is(err, models.ErrScorecardLimit):
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeScorecardLimit, "too many enabled criteria")
	case errors.Is(err, models.ErrScorecardInvalid), errors.Is(err, models.ErrInvalidAnalysisInstructionInput):
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeScorecardInvalid, "invalid scorecard edit")
	case errors.Is(err, models.ErrScorecardRecompileLimited):
		response.WriteError(w, http.StatusTooManyRequests, response.CodeScorecardRecompileLimited, "scorecard recompiled too recently")
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToGetScorecard, "failed to process scorecard")
	}
}

func toResponse(card models.Scorecard) dto.ScorecardResponse {
	resp := dto.ScorecardResponse{
		InstructionUUID: card.InstructionID.String(), InstructionTitle: card.InstructionTitle,
		InstructionVersionUUID: card.VersionID.String(), InstructionVersion: card.InstructionVersion,
		Revision: card.Revision, Status: string(card.Status), Origin: card.Origin,
		IsCurrent: card.IsCurrent, AwaitingConfirmation: card.AwaitingConfirmation, ConfirmRequired: card.ConfirmRequired,
		LockVersion: card.LockVersion, CompileAfter: card.CompileAfter, ConfirmedAt: card.ConfirmedAt,
		Criteria: []dto.ScorecardCriterionItem{}, RemovedCriteria: []dto.ScorecardRemovedCriteria{},
	}
	if card.ID != uuid.Nil {
		id := card.ID.String()
		created := card.CreatedAt
		resp.ScorecardUUID = &id
		resp.CreatedAt = &created
	}
	if card.ErrorCode != nil {
		message := ""
		if card.ErrorMessage != nil {
			message = *card.ErrorMessage
		}
		resp.Error = &dto.ScorecardError{Code: *card.ErrorCode, Message: message}
	}
	for _, c := range card.Criteria {
		item := dto.ScorecardCriterionItem{
			CriterionKey: c.Key.String(), Position: c.Position, Title: c.Title, Requirement: c.Requirement,
			SourceExcerpt: c.SourceExcerpt, Applicability: c.Applicability, Depth: c.Depth,
			RequiredQuestion: c.RequiredQuestion, CrossCutting: c.CrossCutting, Weight: c.Weight,
			IsCritical: c.IsCritical, Enabled: c.Enabled, EditedFields: nonNil(c.EditedFields),
			ChangeKind: c.ChangeKind, Warnings: nonNil(c.Warnings),
		}
		if c.SameAs != nil {
			item.SameAs = &dto.ScorecardRemovedCriteria{CriterionKey: c.SameAs.Key.String(), Title: c.SameAs.Title}
		}
		resp.Criteria = append(resp.Criteria, item)
		if c.Enabled {
			resp.EnabledCount++
		}
	}
	for _, removed := range card.RemovedCriteria {
		resp.RemovedCriteria = append(resp.RemovedCriteria, dto.ScorecardRemovedCriteria{CriterionKey: removed.Key.String(), Title: removed.Title})
	}
	// Every started batch of three criteria is one assessment request and one
	// audit request in each analysed call.
	resp.EstimatedRequestsPerCall = (resp.EnabledCount + 2) / 3 * 2
	return resp
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
