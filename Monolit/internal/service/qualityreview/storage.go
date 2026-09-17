package qualityreview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"verbatrace/monolit/internal/companystate"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type rowScanner interface{ Scan(...any) error }

func (s *Service) loadReview(ctx context.Context, q queryer, id uuid.UUID) (models.QualityReview, []byte, error) {
	return loadReview(ctx, q, id, false)
}

func loadChallenge(ctx context.Context, q queryer, reviewID uuid.UUID) (*models.QualityReviewChallenge, error) {
	var item models.QualityReviewChallenge
	err := q.QueryRowContext(ctx, `SELECT challenge_uuid,review_uuid,analysis_uuid,call_uuid,author_user_uuid,reason,created_at,updated_at FROM call_quality_review_challenges WHERE review_uuid=$1`, reviewID).
		Scan(&item.ID, &item.ReviewUUID, &item.AnalysisUUID, &item.CallUUID, &item.AuthorUserUUID, &item.Reason, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

// loadReviewTx is how every mutation picks up the review it is about to change.
// Taking the row lock is the moment that says "I intend to write", so the frozen
// company check belongs here rather than repeated in each caller: a review in a
// frozen company stays readable and keeps its history, but nothing moves.
func loadReviewTx(ctx context.Context, tx *sql.Tx, id uuid.UUID, lock bool) (models.QualityReview, []byte, error) {
	review, analysis, err := loadReview(ctx, tx, id, lock)
	if err != nil || !lock {
		return review, analysis, err
	}
	if err = companystate.EnsureActiveNullable(ctx, tx, review.CompanyUUID); err != nil {
		return review, analysis, err
	}

	return review, analysis, nil
}

func loadReview(ctx context.Context, q queryer, id uuid.UUID, lock bool) (models.QualityReview, []byte, error) {
	query := `SELECT r.review_uuid,r.call_uuid,r.analysis_uuid,r.analysis_attempt_uuid,r.transcription_revision,r.company_uuid,r.department_uuid,r.reviewed_subject_user_uuid,r.assignee_user_uuid,r.status,r.active_revision_uuid,r.lock_version,r.due_at,r.created_by_user_uuid,r.created_at,r.updated_at,r.published_at,a.result_json FROM call_quality_reviews r JOIN call_analyses a ON a.analysis_uuid=r.analysis_uuid WHERE r.review_uuid=$1`
	if lock {
		query += " FOR UPDATE OF r"
	}
	var result []byte
	r, err := scanReviewWithAnalysis(q.QueryRowContext(ctx, query, id), &result)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil, ErrNotFound
	}
	return r, result, err
}

func scanReview(row rowScanner) (models.QualityReview, error) {
	return scanReviewWithAnalysis(row, nil)
}
func scanReviewWithAnalysis(row rowScanner, analysis *[]byte) (models.QualityReview, error) {
	var q models.QualityReview
	var due, published sql.NullTime
	fields := []any{&q.ID, &q.CallUUID, &q.AnalysisUUID, &q.AnalysisAttemptUUID, &q.TranscriptionRevision, &q.CompanyUUID, &q.DepartmentUUID, &q.SubjectUserUUID, &q.AssigneeUserUUID, &q.Status, &q.ActiveRevisionUUID, &q.LockVersion, &due, &q.CreatedByUserUUID, &q.CreatedAt, &q.UpdatedAt, &published}
	if analysis != nil {
		fields = append(fields, analysis)
	}
	if err := row.Scan(fields...); err != nil {
		return q, err
	}
	if due.Valid {
		q.DueAt = &due.Time
	}
	if published.Valid {
		q.PublishedAt = &published.Time
	}
	if q.AnalysisAttemptUUID.Valid {
		id := q.AnalysisAttemptUUID.UUID
		q.AnalysisAttemptID = &id
	}
	if q.CompanyUUID.Valid {
		id := q.CompanyUUID.UUID
		q.CompanyID = &id
	}
	if q.DepartmentUUID.Valid {
		id := q.DepartmentUUID.UUID
		q.DepartmentID = &id
	}
	if q.SubjectUserUUID.Valid {
		id := q.SubjectUserUUID.UUID
		q.SubjectUserID = &id
	}
	if q.AssigneeUserUUID.Valid {
		id := q.AssigneeUserUUID.UUID
		q.AssigneeUserID = &id
	}
	if q.ActiveRevisionUUID.Valid {
		id := q.ActiveRevisionUUID.UUID
		q.ActiveRevisionID = &id
	}
	return q, nil
}

func authorize(ctx context.Context, q queryer, call uuid.UUID, company uuid.NullUUID, department uuid.NullUUID, actor uuid.UUID) (actorAccess, error) {
	return authorizeQuery(ctx, q, call, company, department, actor)
}
func authorizeTx(ctx context.Context, tx *sql.Tx, call uuid.UUID, company uuid.NullUUID, department uuid.NullUUID, actor uuid.UUID) (actorAccess, error) {
	return authorizeQuery(ctx, tx, call, company, department, actor)
}
func authorizeQuery(ctx context.Context, q queryer, call uuid.UUID, company uuid.NullUUID, department uuid.NullUUID, actor uuid.UUID) (actorAccess, error) {
	var a actorAccess
	if !company.Valid {
		var visibility string
		var uploader uuid.NullUUID
		err := q.QueryRowContext(ctx, `SELECT visibility_scope,uploaded_by_user_uuid FROM calls WHERE call_uuid=$1`, call).Scan(&visibility, &uploader)
		if err != nil {
			return a, err
		}
		if visibility == string(models.CallVisibilityScopePersonal) && uploader.Valid && uploader.UUID == actor {
			a.CanReview = true
			a.CanRead = true
		}
		return a, nil
	}
	var companyRole, departmentRole sql.NullString
	err := q.QueryRowContext(ctx, `SELECT (SELECT role FROM company_members WHERE company_uuid=$1 AND user_uuid=$3 AND status='active'),(SELECT role FROM department_members WHERE department_uuid=$2 AND user_uuid=$3 AND status='active')`, company.UUID, nullableUUID(department), actor).Scan(&companyRole, &departmentRole)
	if err != nil {
		return a, err
	}
	if companyRole.Valid {
		a.CompanyRole = companyRole.String
	}
	if departmentRole.Valid {
		a.DepartmentRole = departmentRole.String
	}
	a.CanReview = models.CompanyMemberRole(a.CompanyRole).ManagesCompany() || (department.Valid && a.DepartmentRole == string(models.DepartmentMemberRoleLeader))
	a.CanRead = a.CanReview
	return a, nil
}

func (s *Service) sourceOutdated(ctx context.Context, q models.QualityReview) bool {
	return sourceOutdatedQuery(ctx, s.db, q)
}
func sourceOutdatedTx(ctx context.Context, tx *sql.Tx, q models.QualityReview) bool {
	return sourceOutdatedQuery(ctx, tx, q)
}
func sourceOutdatedQuery(ctx context.Context, db queryer, q models.QualityReview) bool {
	var revision sql.NullInt64
	var analysis uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT a.analysis_uuid,a.transcription_revision FROM call_analyses a WHERE a.call_uuid=$1`, q.CallUUID).Scan(&analysis, &revision)
	if err != nil || analysis != q.AnalysisUUID {
		return true
	}
	if !revision.Valid {
		err = db.QueryRowContext(ctx, `SELECT COALESCE(s.active_revision,(SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid),1) FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1 AND t.status='transcribed'`, q.CallUUID).Scan(&revision)
	}
	return err != nil || !revision.Valid || int(revision.Int64) != q.TranscriptionRevision
}

func insertEvent(ctx context.Context, tx *sql.Tx, review uuid.UUID, revision, appeal uuid.NullUUID, actor uuid.UUID, event string, access actorAccess, before, after any) error {
	var beforeJSON, afterJSON []byte
	var err error
	if before != nil {
		beforeJSON, err = json.Marshal(before)
		if err != nil {
			return err
		}
	}
	if after != nil {
		afterJSON, err = json.Marshal(after)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO call_quality_review_events(event_uuid,review_uuid,revision_uuid,appeal_uuid,actor_user_uuid,actor_company_role_snapshot,actor_department_role_snapshot,event_type,before_json,after_json,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, uuid.New(), review, nullableUUID(revision), nullableUUID(appeal), actor, nullString(access.CompanyRole), nullString(access.DepartmentRole), event, nullableBytes(beforeJSON), nullableBytes(afterJSON), time.Now().UTC())
	return err
}
func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}
func nullableBytes(v []byte) any {
	if len(v) == 0 {
		return nil
	}
	return v
}

func (s *Service) loadDraft(ctx context.Context, review, author uuid.UUID) (models.QualityReviewRevision, error) {
	var id uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT revision_uuid FROM call_quality_review_revisions WHERE review_uuid=$1 AND author_user_uuid=$2 AND status='draft'`, review, author).Scan(&id)
	if err != nil {
		return models.QualityReviewRevision{}, err
	}
	return s.loadRevision(ctx, id)
}
func (s *Service) loadRevision(ctx context.Context, id uuid.UUID) (models.QualityReviewRevision, error) {
	var r models.QualityReviewRevision
	var overall sql.NullString
	var score, max sql.NullFloat64
	var published sql.NullTime
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision_uuid,revision_number,author_user_uuid,status,overall_comment,human_score,score_max,payload_json,source_hash,created_at,updated_at,published_at FROM call_quality_review_revisions WHERE revision_uuid=$1`, id).Scan(&r.ID, &r.Number, &r.AuthorUserUUID, &r.Status, &overall, &score, &max, &payload, &r.SourceHash, &r.CreatedAt, &r.UpdatedAt, &published)
	if err != nil {
		return r, err
	}
	if overall.Valid {
		r.OverallComment = overall.String
	}
	if score.Valid {
		r.HumanScore = &score.Float64
	}
	if max.Valid {
		r.ScoreMax = &max.Float64
	}
	if published.Valid {
		r.PublishedAt = &published.Time
	}
	r.Payload = json.RawMessage(payload)
	rows, err := s.db.QueryContext(ctx, `SELECT criterion_uuid,criterion_key,title_snapshot,ai_score,human_score,score_min,score_max,weight,decision,COALESCE(comment,''),position FROM call_quality_review_criteria WHERE revision_uuid=$1 ORDER BY position`, id)
	if err != nil {
		return r, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var c models.QualityReviewCriterion
		var ai, human, min, max sql.NullFloat64
		if err = rows.Scan(&c.ID, &c.Key, &c.Title, &ai, &human, &min, &max, &c.Weight, &c.Decision, &c.Comment, &c.Position); err != nil {
			return r, err
		}
		if ai.Valid {
			c.AIScore = &ai.Float64
		}
		if human.Valid {
			c.HumanScore = &human.Float64
		}
		if min.Valid {
			c.ScoreMin = &min.Float64
		}
		if max.Valid {
			c.ScoreMax = &max.Float64
		}
		r.Criteria = append(r.Criteria, c)
	}
	return r, rows.Err()
}

func (s *Service) CreateAppeal(ctx context.Context, in AppealInput) (models.QualityReviewAppeal, error) {
	reason := strings.TrimSpace(in.Reason)
	if in.ReviewUUID == uuid.Nil || in.ActorUserUUID == uuid.Nil || !validComment(reason, 10, 5000) {
		return models.QualityReviewAppeal{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QualityReviewAppeal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q, _, err := loadReviewTx(ctx, tx, in.ReviewUUID, true)
	if err != nil {
		return models.QualityReviewAppeal{}, err
	}
	if !q.ActiveRevisionUUID.Valid {
		return models.QualityReviewAppeal{}, ErrInvalidInput
	}
	access, err := authorizeTx(ctx, tx, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, in.ActorUserUUID)
	if err != nil || !q.CompanyUUID.Valid {
		return models.QualityReviewAppeal{}, ErrForbidden
	}
	allowed := q.SubjectUserUUID.Valid && q.SubjectUserUUID.UUID == in.ActorUserUUID
	if !allowed {
		var uploader uuid.NullUUID
		_ = tx.QueryRowContext(ctx, `SELECT uploaded_by_user_uuid FROM calls WHERE call_uuid=$1`, q.CallUUID).Scan(&uploader)
		allowed = uploader.Valid && uploader.UUID == in.ActorUserUUID
	}
	if allowed {
		var activeMember bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active')`, q.CompanyUUID.UUID, in.ActorUserUUID).Scan(&activeMember); err != nil || !activeMember {
			allowed = false
		}
	}
	if !allowed {
		return models.QualityReviewAppeal{}, ErrForbidden
	}
	// The deputy is the ceiling of the appeal chain: an assessment written by
	// the deputy or the owner has nobody above it to overturn it.
	var byManagement bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM call_quality_review_revisions r
		JOIN company_members cm ON cm.user_uuid=r.author_user_uuid
		WHERE r.revision_uuid=$1 AND cm.company_uuid=$2 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy'))`, q.ActiveRevisionUUID.UUID, q.CompanyUUID.UUID).Scan(&byManagement); err != nil {
		return models.QualityReviewAppeal{}, err
	}
	if byManagement {
		return models.QualityReviewAppeal{}, ErrAppealCeilingReached
	}
	id, now := uuid.New(), time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO call_quality_review_appeals(appeal_uuid,review_uuid,revision_uuid,author_user_uuid,status,reason,created_at,updated_at,lock_version) VALUES($1,$2,$3,$4,'open',$5,$6,$6,1)`, id, q.ID, q.ActiveRevisionUUID.UUID, in.ActorUserUUID, reason, now)
	if err != nil {
		return models.QualityReviewAppeal{}, ErrVersionConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE call_quality_reviews SET status='appealed',lock_version=lock_version+1,updated_at=$2 WHERE review_uuid=$1`, q.ID, now)
	if err != nil {
		return models.QualityReviewAppeal{}, err
	}
	if err = insertEvent(ctx, tx, q.ID, q.ActiveRevisionUUID, uuid.NullUUID{UUID: id, Valid: true}, in.ActorUserUUID, "appeal_opened", access, nil, nil); err != nil {
		return models.QualityReviewAppeal{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.QualityReviewAppeal{}, err
	}
	return models.QualityReviewAppeal{ID: id, ReviewUUID: q.ID, RevisionUUID: q.ActiveRevisionUUID.UUID, AuthorUserUUID: in.ActorUserUUID, Status: "open", Reason: reason, CreatedAt: now, UpdatedAt: now, LockVersion: 1}, nil
}

func (s *Service) ResolveAppeal(ctx context.Context, in ResolveAppealInput) (models.QualityReviewAppeal, error) {
	comment := strings.TrimSpace(in.Comment)
	if in.AppealUUID == uuid.Nil || in.ActorUserUUID == uuid.Nil || !validComment(comment, 3, 5000) || (in.Status != "rejected" && in.Status != "accepted" && in.Status != "partially_accepted") {
		return models.QualityReviewAppeal{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QualityReviewAppeal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var a models.QualityReviewAppeal
	err = tx.QueryRowContext(ctx, `SELECT appeal_uuid,review_uuid,revision_uuid,author_user_uuid,status,reason,created_at,updated_at,lock_version FROM call_quality_review_appeals WHERE appeal_uuid=$1 FOR UPDATE`, in.AppealUUID).Scan(&a.ID, &a.ReviewUUID, &a.RevisionUUID, &a.AuthorUserUUID, &a.Status, &a.Reason, &a.CreatedAt, &a.UpdatedAt, &a.LockVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, err
	}
	if a.Status != "open" && a.Status != "in_review" {
		return a, ErrVersionConflict
	}
	q, _, err := loadReviewTx(ctx, tx, a.ReviewUUID, true)
	if err != nil {
		return a, err
	}
	access, err := authorizeTx(ctx, tx, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, in.ActorUserUUID)
	if err != nil || !access.CanReview {
		return a, ErrForbidden
	}
	// An appeal against a leader's assessment goes up, not sideways: only the
	// deputy or the owner settles it.
	if q.CompanyUUID.Valid {
		var management bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active' AND role IN ('company_manager','company_deputy'))`, q.CompanyUUID.UUID, in.ActorUserUUID).Scan(&management); err != nil {
			return a, err
		}
		if !management {
			return a, ErrForbidden
		}
	}
	var revisionAuthor uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT author_user_uuid FROM call_quality_review_revisions WHERE revision_uuid=$1`, a.RevisionUUID).Scan(&revisionAuthor); err != nil {
		return a, err
	}
	if revisionAuthor == in.ActorUserUUID {
		return a, ErrConflictOfInterest
	}
	now := time.Now().UTC()
	eventType := "appeal_rejected"
	if in.Status != "rejected" {
		if !in.ReplacementRevisionUUID.Valid {
			return a, ErrPublicationBlocked
		}
		var replacementStatus models.QualityReviewRevisionStatus
		var replacementAuthor uuid.UUID
		if err = tx.QueryRowContext(ctx, `SELECT status,author_user_uuid FROM call_quality_review_revisions WHERE revision_uuid=$1 AND review_uuid=$2`, in.ReplacementRevisionUUID.UUID, q.ID).Scan(&replacementStatus, &replacementAuthor); err != nil {
			return a, ErrPublicationBlocked
		}
		if replacementStatus != models.QualityRevisionPublished || in.ReplacementRevisionUUID.UUID == a.RevisionUUID {
			return a, ErrPublicationBlocked
		}
		if q.ActiveRevisionUUID.Valid && q.ActiveRevisionUUID.UUID != in.ReplacementRevisionUUID.UUID {
			_, err = tx.ExecContext(ctx, `UPDATE call_quality_review_revisions SET status='superseded' WHERE revision_uuid=$1 AND status='published'`, q.ActiveRevisionUUID.UUID)
			if err != nil {
				return a, err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE call_quality_reviews SET active_revision_uuid=$2 WHERE review_uuid=$1`, q.ID, in.ReplacementRevisionUUID.UUID)
		if err != nil {
			return a, err
		}
		eventType = "appeal_accepted"
	}
	_, err = tx.ExecContext(ctx, `UPDATE call_quality_review_appeals SET status=$2,resolution_comment=$3,resolved_by_user_uuid=$4,resolved_at=$5,updated_at=$5,lock_version=lock_version+1 WHERE appeal_uuid=$1`, a.ID, in.Status, comment, in.ActorUserUUID, now)
	if err != nil {
		return a, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE call_quality_reviews SET status='resolved',updated_at=$2,lock_version=lock_version+1 WHERE review_uuid=$1`, q.ID, now)
	if err != nil {
		return a, err
	}
	if err = insertEvent(ctx, tx, q.ID, uuid.NullUUID{UUID: a.RevisionUUID, Valid: true}, uuid.NullUUID{UUID: a.ID, Valid: true}, in.ActorUserUUID, eventType, access, nil, nil); err != nil {
		return a, err
	}
	if err = tx.Commit(); err != nil {
		return a, err
	}
	a.Status = in.Status
	a.ResolutionComment = &comment
	a.ResolvedByUserUUID = uuid.NullUUID{UUID: in.ActorUserUUID, Valid: true}
	a.ResolvedByUserID = &in.ActorUserUUID
	a.ResolvedAt = &now
	a.UpdatedAt = now
	a.LockVersion++
	return a, nil
}

func (s *Service) ListAppeals(ctx context.Context, reviewID, actor uuid.UUID) ([]models.QualityReviewAppeal, error) {
	q, _, err := s.loadReview(ctx, s.db, reviewID)
	if err != nil {
		return nil, err
	}
	access, _ := authorize(ctx, s.db, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, actor)
	if !access.CanRead && (!q.SubjectUserUUID.Valid || q.SubjectUserUUID.UUID != actor) {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT appeal_uuid,review_uuid,revision_uuid,author_user_uuid,status,reason,resolution_comment,resolved_by_user_uuid,created_at,updated_at,resolved_at,lock_version FROM call_quality_review_appeals WHERE review_uuid=$1 ORDER BY created_at DESC`, reviewID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.QualityReviewAppeal, 0)
	for rows.Next() {
		var a models.QualityReviewAppeal
		var comment sql.NullString
		var resolved sql.NullTime
		if err = rows.Scan(&a.ID, &a.ReviewUUID, &a.RevisionUUID, &a.AuthorUserUUID, &a.Status, &a.Reason, &comment, &a.ResolvedByUserUUID, &a.CreatedAt, &a.UpdatedAt, &resolved, &a.LockVersion); err != nil {
			return nil, err
		}
		if comment.Valid {
			a.ResolutionComment = &comment.String
		}
		if a.ResolvedByUserUUID.Valid {
			id := a.ResolvedByUserUUID.UUID
			a.ResolvedByUserID = &id
		}
		if resolved.Valid {
			a.ResolvedAt = &resolved.Time
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

func (s *Service) ListEvents(ctx context.Context, reviewID, actor uuid.UUID) ([]models.QualityReviewEvent, error) {
	q, _, err := s.loadReview(ctx, s.db, reviewID)
	if err != nil {
		return nil, err
	}
	access, _ := authorize(ctx, s.db, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, actor)
	if !access.CanReview {
		return nil, ErrForbidden
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_uuid,review_uuid,revision_uuid,appeal_uuid,actor_user_uuid,event_type,before_json,after_json,reason,created_at FROM call_quality_review_events WHERE review_uuid=$1 ORDER BY created_at DESC,event_uuid`, reviewID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.QualityReviewEvent, 0)
	for rows.Next() {
		var e models.QualityReviewEvent
		var revision, appeal uuid.NullUUID
		var before, after []byte
		var reason sql.NullString
		if err = rows.Scan(&e.ID, &e.ReviewUUID, &revision, &appeal, &e.ActorUserUUID, &e.EventType, &before, &after, &reason, &e.CreatedAt); err != nil {
			return nil, err
		}
		if revision.Valid {
			id := revision.UUID
			e.RevisionUUID = &id
		}
		if appeal.Valid {
			id := appeal.UUID
			e.AppealUUID = &id
		}
		if len(before) > 0 {
			e.Before = json.RawMessage(before)
		}
		if len(after) > 0 {
			e.After = json.RawMessage(after)
		}
		if reason.Valid {
			e.Reason = &reason.String
		}
		items = append(items, e)
	}
	return items, rows.Err()
}
