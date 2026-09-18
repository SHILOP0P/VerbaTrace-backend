package callsubject

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/companystate"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// maxManualSubjects bounds a hand-made composition; a call is a conversation,
// not a meeting of the whole company.
const maxManualSubjects = 20

// SetManual lets the owner, the deputy or the leader of the call's department
// say whom a call counts for. The choice holds against automatic resolution
// until it is cleared with an empty list, and every such change is recorded.
func (s *Service) SetManual(ctx context.Context, input models.SetCallSubjectsInput) (models.CallSubjects, error) {
	if len(input.UserIDs) > maxManualSubjects {
		return models.CallSubjects{}, models.ErrInvalidCallSubjects
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.CallSubjects{}, fmt.Errorf("begin manual call subjects: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	call, err := lockCall(ctx, tx, input.CallID)
	if err != nil {
		return models.CallSubjects{}, err
	}
	if !call.Company.Valid {
		// A personal call has one subject, whoever uploaded it.
		return models.CallSubjects{}, models.ErrCallSubjectsLocked
	}
	var allowed bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM company_members WHERE company_uuid = $1 AND user_uuid = $3 AND status = 'active' AND role IN ('company_manager','company_deputy'))
		    OR EXISTS (SELECT 1 FROM department_members WHERE $2::uuid IS NOT NULL AND department_uuid = $2 AND user_uuid = $3 AND status = 'active' AND role = 'department_leader')`,
		call.Company, call.Department, input.ActorID).Scan(&allowed); err != nil {
		return models.CallSubjects{}, fmt.Errorf("check call subject rights: %w", err)
	}
	if !allowed {
		return models.CallSubjects{}, models.ErrForbidden
	}
	if err := companystate.EnsureActive(ctx, tx, call.Company.UUID); err != nil {
		return models.CallSubjects{}, err
	}
	before, err := loadRows(ctx, tx, input.CallID)
	if err != nil {
		return models.CallSubjects{}, err
	}
	actor := uuid.NullUUID{UUID: input.ActorID, Valid: true}
	change := Change{CallID: input.CallID, Title: call.Title, OccurredAt: call.OccurredAt, CompanyID: call.Company, Department: call.Department, Uploader: call.Uploader, Actor: actor}

	var result decision
	if len(input.UserIDs) == 0 {
		// Back to automatic resolution.
		decideIn, err := loadInput(ctx, tx, input.CallID, call)
		if err != nil {
			return models.CallSubjects{}, err
		}
		result = decide(decideIn)
	} else {
		result, err = manualDecision(ctx, tx, call, input, before)
		if err != nil {
			return models.CallSubjects{}, err
		}
	}
	change, err = s.write(ctx, tx, change, before, result, models.CallSubjectCauseManual)
	if err != nil {
		return models.CallSubjects{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.CallSubjects{}, fmt.Errorf("commit manual call subjects: %w", err)
	}
	s.After(ctx, change)
	return s.Get(ctx, input.CallID)
}

func manualDecision(ctx context.Context, q querier, call callRow, input models.SetCallSubjectsInput, before []subjectRow) (decision, error) {
	seen := map[uuid.UUID]bool{}
	for _, id := range input.UserIDs {
		if id == uuid.Nil || seen[id] {
			return decision{}, models.ErrInvalidCallSubjects
		}
		seen[id] = true
	}
	primary := input.Primary
	if primary == uuid.Nil {
		primary = input.UserIDs[0]
	}
	if !seen[primary] {
		return decision{}, models.ErrInvalidCallSubjects
	}
	var members int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM company_members WHERE company_uuid = $1 AND user_uuid = ANY($2::uuid[]) AND status = 'active'`, call.Company, uuidStrings(input.UserIDs)).Scan(&members); err != nil {
		return decision{}, fmt.Errorf("check call subject members: %w", err)
	}
	if members != len(input.UserIDs) {
		// Only employees of the call's company can be what a call counts for.
		return decision{}, models.ErrInvalidCallSubjects
	}
	previous := map[uuid.UUID]subjectRow{}
	for _, row := range before {
		previous[row.UserID] = row
	}
	var state struct{ shared, internal bool }
	_ = q.QueryRowContext(ctx, `SELECT is_shared, is_internal FROM call_subject_states WHERE call_uuid = $1`, input.CallID).Scan(&state.shared, &state.internal)
	result := decision{Internal: state.internal}
	for _, id := range input.UserIDs {
		row := subjectRow{UserID: id, Source: models.CallSubjectSourceManual, IsPrimary: id == primary, GrantsAccess: true, SetBy: uuid.NullUUID{UUID: input.ActorID, Valid: true}}
		// What was found about the person's voice stays with them.
		if old, ok := previous[id]; ok {
			row.SpeakerKey, row.TalkShare, row.Signals = old.SpeakerKey, old.TalkShare, old.Signals
		}
		result.Subjects = append(result.Subjects, row)
	}
	result.Shared = len(result.Subjects) > 1 && !result.Internal
	return result, nil
}

func uuidStrings(ids []uuid.UUID) []string {
	result := make([]string, len(ids))
	for i, id := range ids {
		result[i] = id.String()
	}
	return result
}
