package call

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"verbatrace/monolit/internal/API/dto"
	"verbatrace/monolit/internal/API/response"
	"verbatrace/monolit/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// CallAccessReader says how a viewer reaches a call.
type CallAccessReader interface {
	GetAccess(ctx context.Context, callID uuid.UUID, userID uuid.UUID) (models.CallAccess, error)
}

// CallSubjectsService reads and sets whom a call counts for.
type CallSubjectsService interface {
	Get(ctx context.Context, callID uuid.UUID) (models.CallSubjects, error)
	SetManual(ctx context.Context, input models.SetCallSubjectsInput) (models.CallSubjects, error)
}

func (h *CallHandler) SetCallAccessReader(reader CallAccessReader)        { h.access = reader }
func (h *CallHandler) SetCallSubjectsService(service CallSubjectsService) { h.subjects = service }

// enrichCallAccess adds what the call page needs to hide editing from a reader
// and to show whom the call counts for. It is best effort: the call itself is
// already authorised.
func (h *CallHandler) enrichCallAccess(r *http.Request, callID, userID uuid.UUID, resp *dto.CallResponse) {
	if h.access != nil {
		if access, err := h.access.GetAccess(r.Context(), callID, userID); err == nil {
			resp.Access = &dto.CallAccessResponse{CanEdit: access.CanEdit, Via: access.Via}
		}
	}
	if h.subjects != nil {
		if subjects, err := h.subjects.Get(r.Context(), callID); err == nil {
			applySubjects(resp, subjects)
		}
	}
}

func applySubjects(resp *dto.CallResponse, subjects models.CallSubjects) {
	resp.Subjects = make([]dto.CallSubjectResponse, 0, len(subjects.Subjects))
	for _, s := range subjects.Subjects {
		resp.Subjects = append(resp.Subjects, dto.CallSubjectResponse{
			UserUUID: s.UserID.String(), FullName: s.FullName, Source: s.Source, IsPrimary: s.IsPrimary,
			GrantsAccess: s.GrantsAccess, SpeakerKey: s.SpeakerKey, TalkShare: s.TalkShare, MatchSignals: s.MatchSignals,
		})
	}
	shared, internal, manual := subjects.IsShared, subjects.IsInternal, subjects.SubjectsChangedManually
	resp.IsShared, resp.IsInternal, resp.SubjectsChangedManually = &shared, &internal, &manual
}

// SetSubjects is PUT /calls/{uuid}/subjects: the owner, the deputy or the
// department leader says whom the call counts for. An empty list returns the
// call to automatic resolution.
func (h *CallHandler) SetSubjects(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromRequest(r)
	if !ok {
		response.WriteError(w, http.StatusUnauthorized, response.CodeUnauthorized, "unauthorized")
		return
	}
	callID, err := uuid.Parse(chi.URLParam(r, "uuid"))
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidCallUUID, "invalid call uuid")
		return
	}
	if h.subjects == nil {
		response.WriteError(w, http.StatusNotImplemented, response.CodeNotImplemented, "call subjects are not configured")
		return
	}
	var body dto.SetCallSubjectsRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, response.CodeInvalidRequestBody, "invalid request body")
		return
	}
	input := models.SetCallSubjectsInput{CallID: callID, ActorID: userID}
	for _, raw := range body.UserUUIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.WriteError(w, http.StatusUnprocessableEntity, response.CodeInvalidCallSubjects, "invalid user uuid")
			return
		}
		input.UserIDs = append(input.UserIDs, id)
	}
	if body.PrimaryUserUUID != nil {
		if input.Primary, err = uuid.Parse(*body.PrimaryUserUUID); err != nil {
			response.WriteError(w, http.StatusUnprocessableEntity, response.CodeInvalidCallSubjects, "invalid primary user uuid")
			return
		}
	}
	// The call must at least be visible; the service decides the right to change.
	if _, err := h.service.GetByUUID(r.Context(), callID, userID); err != nil {
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
		return
	}
	subjects, err := h.subjects.SetManual(r.Context(), input)
	switch {
	case err == nil:
	case errors.Is(err, models.ErrForbidden):
		response.WriteError(w, http.StatusForbidden, response.CodeForbidden, "Состав сотрудников звонка меняют владелец, заместитель или лидер отдела")
		return
	case errors.Is(err, models.ErrInvalidCallSubjects):
		response.WriteError(w, http.StatusUnprocessableEntity, response.CodeInvalidCallSubjects, "Сотрудниками звонка могут быть только активные участники его компании")
		return
	case errors.Is(err, models.ErrCallSubjectsLocked):
		response.WriteError(w, http.StatusConflict, response.CodeCallSubjectsLocked, "У личного звонка один сотрудник — тот, кто его загрузил")
		return
	case errors.Is(err, models.ErrCompanyFrozen):
		response.WriteError(w, http.StatusConflict, response.CodeCompanyFrozen, "company is frozen")
		return
	case errors.Is(err, models.ErrCallNotFound):
		response.WriteError(w, http.StatusNotFound, response.CodeCallNotFound, "call not found")
		return
	default:
		response.WriteError(w, http.StatusInternalServerError, response.CodeFailedToFindCall, "failed to change call subjects")
		return
	}
	var resp dto.CallResponse
	applySubjects(&resp, subjects)
	_ = response.WriteJSON(w, http.StatusOK, map[string]any{
		"subjects": resp.Subjects, "is_shared": resp.IsShared, "is_internal": resp.IsInternal,
		"subjects_changed_manually": resp.SubjectsChangedManually,
	})
}
