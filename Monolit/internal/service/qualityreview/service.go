package qualityreview

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

var (
	ErrNotFound           = errors.New("quality review not found")
	ErrForbidden          = errors.New("quality review forbidden")
	ErrInvalidInput       = errors.New("invalid quality review input")
	ErrAlreadyExists      = errors.New("quality review already exists")
	ErrAlreadyClaimed     = errors.New("quality review already claimed")
	ErrVersionConflict    = errors.New("quality review version conflict")
	ErrSourceOutdated     = errors.New("quality review source outdated")
	ErrPublicationBlocked = errors.New("quality review publication blocked")
	ErrConflictOfInterest = errors.New("quality review conflict of interest")
	ErrReviewLimitReached = errors.New("quality review limit reached")
	ErrReviewerMustDiffer = errors.New("quality review reviewer must differ")
	ErrActiveAppealExists = errors.New("quality review active appeal exists")
)

type Service struct{ db *sql.DB }

func NewService(db *sql.DB) *Service { return &Service{db: db} }

type CreateInput struct {
	CallUUID         uuid.UUID
	AnalysisUUID     uuid.UUID
	SubjectUserUUID  uuid.NullUUID
	AssigneeUserUUID uuid.NullUUID
	DueAt            *time.Time
	ActorUserUUID    uuid.UUID
}

type ListInput struct {
	ActorUserUUID  uuid.UUID
	CompanyUUID    uuid.NullUUID
	DepartmentUUID uuid.NullUUID
	Status         models.QualityReviewStatus
	Limit          int
	Offset         int
}

type CriterionInput struct {
	Key           string   `json:"criterion_key"`
	Title         string   `json:"title,omitempty"`
	Custom        bool     `json:"custom,omitempty"`
	HumanScore    *float64 `json:"human_score"`
	NotApplicable bool     `json:"not_applicable"`
	Comment       string   `json:"comment"`
}

type DraftInput struct {
	ReviewUUID      uuid.UUID
	ActorUserUUID   uuid.UUID
	ExpectedVersion int64
	OverallComment  string
	Payload         json.RawMessage
	Criteria        []CriterionInput
}

type AppealInput struct {
	ReviewUUID    uuid.UUID
	ActorUserUUID uuid.UUID
	Reason        string
}

type ChallengeInput struct {
	CallUUID      uuid.UUID
	AnalysisUUID  uuid.UUID
	ActorUserUUID uuid.UUID
	Reason        string
}

type ResolveAppealInput struct {
	AppealUUID              uuid.UUID
	ActorUserUUID           uuid.UUID
	Status                  models.QualityReviewAppealStatus
	Comment                 string
	ReplacementRevisionUUID uuid.NullUUID
}

type actorAccess struct {
	CompanyRole    string
	DepartmentRole string
	CanReview      bool
	CanRead        bool
}

type sourceCriterion struct {
	Key      string
	Title    string
	Score    *float64
	Min      float64
	Max      float64
	Weight   float64
	Position int
}

func isReviewVisibleWithoutReviewPermission(status models.QualityReviewStatus) bool {
	switch status {
	case models.QualityReviewUnassigned, models.QualityReviewAssigned, models.QualityReviewPublished, models.QualityReviewResolved, models.QualityReviewCanceled:
		return true
	default:
		return false
	}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (models.QualityReview, error) {
	if in.CallUUID == uuid.Nil || in.AnalysisUUID == uuid.Nil || in.ActorUserUUID == uuid.Nil {
		return models.QualityReview{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QualityReview{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var companyID, departmentID, attemptID, uploaderID uuid.NullUUID
	var revision sql.NullInt64
	var analysisStatus, visibility string
	var result []byte
	err = tx.QueryRowContext(ctx, `
		SELECT c.company_uuid,c.department_uuid,c.uploaded_by_user_uuid,c.visibility_scope,a.source_attempt_uuid,a.transcription_revision,a.status,a.result_json
		FROM calls c JOIN call_analyses a ON a.call_uuid=c.call_uuid
		WHERE c.call_uuid=$1 AND a.analysis_uuid=$2`, in.CallUUID, in.AnalysisUUID).
		Scan(&companyID, &departmentID, &uploaderID, &visibility, &attemptID, &revision, &analysisStatus, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return models.QualityReview{}, ErrNotFound
	}
	if err != nil {
		return models.QualityReview{}, err
	}
	if analysisStatus != string(models.CallAnalysisStatusDone) || len(result) == 0 {
		return models.QualityReview{}, ErrInvalidInput
	}
	if !revision.Valid {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(s.active_revision,(SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid),1) FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1 AND t.status='transcribed'`, in.CallUUID).Scan(&revision); err != nil {
			return models.QualityReview{}, ErrInvalidInput
		}
	}
	personal := visibility == string(models.CallVisibilityScopePersonal)
	if personal && (!uploaderID.Valid || uploaderID.UUID != in.ActorUserUUID) {
		return models.QualityReview{}, ErrForbidden
	}
	if !personal && !companyID.Valid {
		return models.QualityReview{}, ErrInvalidInput
	}
	if !personal && uploaderID.Valid && uploaderID.UUID == in.ActorUserUUID {
		return models.QualityReview{}, ErrConflictOfInterest
	}
	access, err := authorizeTx(ctx, tx, in.CallUUID, companyID, departmentID, in.ActorUserUUID)
	if err != nil || !access.CanReview {
		return models.QualityReview{}, ErrForbidden
	}
	if personal {
		in.SubjectUserUUID = uploaderID
		in.AssigneeUserUUID = uploaderID
	} else if in.SubjectUserUUID.Valid {
		var active bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active')`, companyID, in.SubjectUserUUID.UUID).Scan(&active)
		if err != nil || !active {
			return models.QualityReview{}, ErrInvalidInput
		}
	}
	if in.AssigneeUserUUID.Valid {
		assigned, e := authorizeTx(ctx, tx, in.CallUUID, companyID, departmentID, in.AssigneeUserUUID.UUID)
		if e != nil || !assigned.CanReview {
			return models.QualityReview{}, ErrInvalidInput
		}
	}
	now, id := time.Now().UTC(), uuid.New()
	status := models.QualityReviewUnassigned
	if in.AssigneeUserUUID.Valid {
		status = models.QualityReviewAssigned
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO call_quality_reviews
		(review_uuid,call_uuid,analysis_uuid,analysis_attempt_uuid,transcription_revision,company_uuid,department_uuid,reviewed_subject_user_uuid,assignee_user_uuid,status,lock_version,due_at,created_by_user_uuid,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,1,$11,$12,$13,$13)`, id, in.CallUUID, in.AnalysisUUID, nullableUUID(attemptID), revision.Int64, nullableUUID(companyID), nullableUUID(departmentID), nullableUUID(in.SubjectUserUUID), nullableUUID(in.AssigneeUserUUID), status, in.DueAt, in.ActorUserUUID, now)
	if err != nil {
		if strings.Contains(err.Error(), "uq_call_quality_reviews_active_analysis") {
			return models.QualityReview{}, ErrAlreadyExists
		}
		return models.QualityReview{}, err
	}
	if err = insertEvent(ctx, tx, id, uuid.NullUUID{}, uuid.NullUUID{}, in.ActorUserUUID, "created", access, nil, nil); err != nil {
		return models.QualityReview{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.QualityReview{}, err
	}
	return s.Get(ctx, id, in.ActorUserUUID)
}

func (s *Service) ChallengeAnalysis(ctx context.Context, in ChallengeInput) (models.QualityReview, error) {
	reason := strings.TrimSpace(in.Reason)
	if in.CallUUID == uuid.Nil || in.AnalysisUUID == uuid.Nil || in.ActorUserUUID == uuid.Nil || !validComment(reason, 10, 5000) {
		return models.QualityReview{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QualityReview{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var uploader, companyID, departmentID, attemptID uuid.NullUUID
	var revision sql.NullInt64
	var analysisStatus string
	var result []byte
	err = tx.QueryRowContext(ctx, `SELECT c.uploaded_by_user_uuid,c.company_uuid,c.department_uuid,a.source_attempt_uuid,a.transcription_revision,a.status,a.result_json FROM calls c JOIN call_analyses a ON a.call_uuid=c.call_uuid WHERE c.call_uuid=$1 AND a.analysis_uuid=$2`, in.CallUUID, in.AnalysisUUID).
		Scan(&uploader, &companyID, &departmentID, &attemptID, &revision, &analysisStatus, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return models.QualityReview{}, ErrNotFound
	}
	if err != nil {
		return models.QualityReview{}, err
	}
	if !uploader.Valid || uploader.UUID != in.ActorUserUUID || !companyID.Valid {
		return models.QualityReview{}, ErrForbidden
	}
	var activeMember bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active')`, companyID.UUID, in.ActorUserUUID).Scan(&activeMember); err != nil || !activeMember {
		return models.QualityReview{}, ErrForbidden
	}
	if analysisStatus != string(models.CallAnalysisStatusDone) || len(result) == 0 {
		return models.QualityReview{}, ErrInvalidInput
	}
	if !revision.Valid {
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(s.active_revision,(SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid),1) FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1 AND t.status='transcribed'`, in.CallUUID).Scan(&revision); err != nil {
			return models.QualityReview{}, ErrInvalidInput
		}
	}

	var reviewID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT review_uuid FROM call_quality_reviews WHERE analysis_uuid=$1 AND status<>'canceled' FOR UPDATE`, in.AnalysisUUID).Scan(&reviewID)
	now := time.Now().UTC()
	if errors.Is(err, sql.ErrNoRows) {
		reviewID = uuid.New()
		_, err = tx.ExecContext(ctx, `INSERT INTO call_quality_reviews(review_uuid,call_uuid,analysis_uuid,analysis_attempt_uuid,transcription_revision,company_uuid,department_uuid,reviewed_subject_user_uuid,status,lock_version,created_by_user_uuid,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'unassigned',1,$8,$9,$9)`, reviewID, in.CallUUID, in.AnalysisUUID, nullableUUID(attemptID), revision.Int64, companyID.UUID, nullableUUID(departmentID), in.ActorUserUUID, now)
	}
	if err != nil {
		return models.QualityReview{}, err
	}
	var activeRevision uuid.NullUUID
	if err = tx.QueryRowContext(ctx, `SELECT active_revision_uuid FROM call_quality_reviews WHERE review_uuid=$1`, reviewID).Scan(&activeRevision); err != nil {
		return models.QualityReview{}, err
	}
	var existingChallenge bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_quality_review_challenges WHERE review_uuid=$1)`, reviewID).Scan(&existingChallenge); err != nil {
		return models.QualityReview{}, err
	}
	if !activeRevision.Valid && existingChallenge {
		return models.QualityReview{}, ErrActiveAppealExists
	}
	if !existingChallenge {
		_, err = tx.ExecContext(ctx, `INSERT INTO call_quality_review_challenges(challenge_uuid,review_uuid,analysis_uuid,call_uuid,author_user_uuid,reason,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$7)`, uuid.New(), reviewID, in.AnalysisUUID, in.CallUUID, in.ActorUserUUID, reason, now)
		if err != nil {
			return models.QualityReview{}, err
		}
	}
	var eventAppeal uuid.NullUUID
	if activeRevision.Valid {
		var openAppeal bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_quality_review_appeals WHERE review_uuid=$1 AND status IN ('open','in_review'))`, reviewID).Scan(&openAppeal); err != nil {
			return models.QualityReview{}, err
		}
		if openAppeal {
			return models.QualityReview{}, ErrActiveAppealExists
		}
		appealID := uuid.New()
		eventAppeal = uuid.NullUUID{UUID: appealID, Valid: true}
		if _, err = tx.ExecContext(ctx, `INSERT INTO call_quality_review_appeals(appeal_uuid,review_uuid,revision_uuid,author_user_uuid,status,reason,created_at,updated_at,lock_version) VALUES($1,$2,$3,$4,'open',$5,$6,$6,1)`, appealID, reviewID, activeRevision.UUID, in.ActorUserUUID, reason, now); err != nil {
			return models.QualityReview{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE call_quality_reviews SET status='appealed',assignee_user_uuid=NULL,lock_version=lock_version+1,updated_at=$2 WHERE review_uuid=$1`, reviewID, now); err != nil {
			return models.QualityReview{}, err
		}
	}
	access, err := authorizeTx(ctx, tx, in.CallUUID, companyID, departmentID, in.ActorUserUUID)
	if err != nil {
		return models.QualityReview{}, err
	}
	if err = insertEvent(ctx, tx, reviewID, activeRevision, eventAppeal, in.ActorUserUUID, "analysis_challenged", access, nil, nil); err != nil {
		return models.QualityReview{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.QualityReview{}, err
	}
	return s.Get(ctx, reviewID, in.ActorUserUUID)
}

func (s *Service) List(ctx context.Context, in ListInput) ([]models.QualityReview, error) {
	if in.ActorUserUUID == uuid.Nil {
		return nil, ErrInvalidInput
	}
	if in.Limit <= 0 {
		in.Limit = 25
	}
	if in.Limit > 100 {
		in.Limit = 100
	}
	if in.Offset < 0 {
		in.Offset = 0
	}
	args := []any{in.ActorUserUUID}
	where := []string{`(
		EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=q.company_uuid AND cm.user_uuid=$1 AND cm.status='active' AND cm.role='company_manager')
		OR EXISTS(SELECT 1 FROM department_members dm WHERE dm.department_uuid=q.department_uuid AND dm.user_uuid=$1 AND dm.status='active' AND dm.role='department_leader')
		OR (q.status IN ('unassigned','assigned','published','resolved','canceled')
			AND EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=q.company_uuid AND cm.user_uuid=$1 AND cm.status='active')
			AND (q.reviewed_subject_user_uuid=$1 OR EXISTS(SELECT 1 FROM calls c WHERE c.call_uuid=q.call_uuid AND c.uploaded_by_user_uuid=$1)))
		OR (q.company_uuid IS NULL AND EXISTS(SELECT 1 FROM calls c WHERE c.call_uuid=q.call_uuid AND c.visibility_scope='personal' AND c.uploaded_by_user_uuid=$1)))`}
	if in.CompanyUUID.Valid {
		args = append(args, in.CompanyUUID.UUID)
		where = append(where, fmt.Sprintf("q.company_uuid=$%d", len(args)))
	}
	if in.DepartmentUUID.Valid {
		args = append(args, in.DepartmentUUID.UUID)
		where = append(where, fmt.Sprintf("q.department_uuid=$%d", len(args)))
	}
	if in.Status != "" {
		if in.Status == "pending" {
			where = append(where, "q.status IN ('unassigned','assigned')")
		} else {
			args = append(args, in.Status)
			where = append(where, fmt.Sprintf("q.status=$%d", len(args)))
		}
	}
	args = append(args, in.Limit, in.Offset)
	query := `SELECT q.review_uuid,q.call_uuid,q.analysis_uuid,q.analysis_attempt_uuid,q.transcription_revision,q.company_uuid,q.department_uuid,q.reviewed_subject_user_uuid,q.assignee_user_uuid,q.status,q.active_revision_uuid,q.lock_version,q.due_at,q.created_by_user_uuid,q.created_at,q.updated_at,q.published_at FROM call_quality_reviews q WHERE ` + strings.Join(where, " AND ") + fmt.Sprintf(" ORDER BY q.updated_at DESC,q.review_uuid LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.QualityReview, 0)
	for rows.Next() {
		q, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		if reviewContext, contextErr := s.GetAnalysisContext(ctx, q.CallUUID, q.AnalysisUUID, in.ActorUserUUID); contextErr == nil {
			q.Capabilities = reviewContext.Capabilities
			q.SourceOutdated = reviewContext.SourceOutdated
		}
		items = append(items, q)
	}
	return items, rows.Err()
}

func (s *Service) Get(ctx context.Context, id, actor uuid.UUID) (models.QualityReview, error) {
	if id == uuid.Nil || actor == uuid.Nil {
		return models.QualityReview{}, ErrInvalidInput
	}
	q, analysis, err := s.loadReview(ctx, s.db, id)
	if err != nil {
		return q, err
	}
	access, err := authorize(ctx, s.db, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, actor)
	if err != nil {
		return models.QualityReview{}, ErrNotFound
	}
	if !access.CanReview && !isReviewVisibleWithoutReviewPermission(q.Status) {
		return models.QualityReview{}, ErrNotFound
	}
	q.Challenge, _ = loadChallenge(ctx, s.db, q.ID)
	if !access.CanRead {
		allowed := q.SubjectUserUUID.Valid && q.SubjectUserUUID.UUID == actor
		if q.Challenge != nil && q.Challenge.AuthorUserUUID == actor {
			allowed = true
		}
		if !allowed {
			var uploader uuid.NullUUID
			_ = s.db.QueryRowContext(ctx, `SELECT uploaded_by_user_uuid FROM calls WHERE call_uuid=$1`, q.CallUUID).Scan(&uploader)
			allowed = uploader.Valid && uploader.UUID == actor
		}
		if !allowed {
			return models.QualityReview{}, ErrNotFound
		}
	}
	q.Analysis = analysis
	q.SourceOutdated = s.sourceOutdated(ctx, q)
	q.Capabilities = models.QualityReviewCapabilities{CanClaim: access.CanReview && !q.AssigneeUserUUID.Valid, CanEdit: access.CanReview && (!q.AssigneeUserUUID.Valid || q.AssigneeUserUUID.UUID == actor || access.CompanyRole == string(models.CompanyMemberRoleManager)), CanPublish: access.CanReview, CanViewEvents: access.CanReview}
	if q.ActiveRevisionUUID.Valid {
		r, e := s.loadRevision(ctx, q.ActiveRevisionUUID.UUID)
		if e == nil {
			q.PublishedRevision = &r
		}
	}
	q.Revisions, _ = s.loadPublishedRevisions(ctx, q.ID)
	q.EffectiveAnalysis, _ = buildEffectiveAnalysis(analysis, q.Revisions)
	if reviewContext, contextErr := s.GetAnalysisContext(ctx, q.CallUUID, q.AnalysisUUID, actor); contextErr == nil {
		q.Capabilities = reviewContext.Capabilities
	}
	q.Appeals, _ = s.ListAppeals(ctx, q.ID, actor)
	if q.Capabilities.CanEdit {
		if r, e := s.loadDraft(ctx, id, actor); e == nil {
			q.Draft = &r
		}
	}
	return q, nil
}

func (s *Service) Claim(ctx context.Context, id, actor uuid.UUID, expected int64) (models.QualityReview, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QualityReview{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q, _, err := loadReviewTx(ctx, tx, id, true)
	if err != nil {
		return q, err
	}
	access, err := authorizeTx(ctx, tx, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, actor)
	if err != nil || !access.CanReview {
		return q, ErrForbidden
	}
	if expected > 0 && q.LockVersion != expected {
		return q, ErrVersionConflict
	}
	if q.AssigneeUserUUID.Valid && q.AssigneeUserUUID.UUID != actor {
		return q, ErrAlreadyClaimed
	}
	res, err := tx.ExecContext(ctx, `UPDATE call_quality_reviews SET assignee_user_uuid=$2,status='in_review',lock_version=lock_version+1,updated_at=now() WHERE review_uuid=$1 AND lock_version=$3`, id, actor, q.LockVersion)
	if err != nil {
		return q, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return q, ErrVersionConflict
	}
	if err = insertEvent(ctx, tx, id, uuid.NullUUID{}, uuid.NullUUID{}, actor, "claimed", access, nil, nil); err != nil {
		return q, err
	}
	if err = tx.Commit(); err != nil {
		return q, err
	}
	return s.Get(ctx, id, actor)
}

func (s *Service) SaveDraft(ctx context.Context, in DraftInput) (models.QualityReview, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QualityReview{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q, analysis, err := loadReviewTx(ctx, tx, in.ReviewUUID, true)
	if err != nil {
		return q, err
	}
	access, err := authorizeTx(ctx, tx, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, in.ActorUserUUID)
	if err != nil || !access.CanReview {
		return q, ErrForbidden
	}
	if q.AssigneeUserUUID.Valid && q.AssigneeUserUUID.UUID != in.ActorUserUUID && access.CompanyRole != string(models.CompanyMemberRoleManager) {
		return q, ErrForbidden
	}
	if q.LockVersion != in.ExpectedVersion {
		return q, ErrVersionConflict
	}
	if sourceOutdatedTx(ctx, tx, q) {
		return q, ErrSourceOutdated
	}
	source, err := parseSourceCriteria(analysis)
	if err != nil {
		return q, ErrInvalidInput
	}
	normalized, score, scoreMax, err := normalizeDraft(source, in.Criteria, false)
	if err != nil {
		return q, err
	}
	payload := in.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if !json.Valid(payload) {
		return q, ErrInvalidInput
	}
	now := time.Now().UTC()
	sourceHash := hashSource(q.AnalysisUUID, q.TranscriptionRevision, analysis)
	var revisionID uuid.UUID
	var number int
	err = tx.QueryRowContext(ctx, `SELECT revision_uuid,revision_number FROM call_quality_review_revisions WHERE review_uuid=$1 AND author_user_uuid=$2 AND status='draft' FOR UPDATE`, q.ID, in.ActorUserUUID).Scan(&revisionID, &number)
	if errors.Is(err, sql.ErrNoRows) {
		revisionID = uuid.New()
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_number),0)+1 FROM call_quality_review_revisions WHERE review_uuid=$1`, q.ID).Scan(&number)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO call_quality_review_revisions(revision_uuid,review_uuid,revision_number,author_user_uuid,status,overall_comment,human_score,score_max,payload_json,source_hash,created_at,updated_at) VALUES($1,$2,$3,$4,'draft',$5,$6,$7,$8,$9,$10,$10)`, revisionID, q.ID, number, in.ActorUserUUID, strings.TrimSpace(in.OverallComment), score, scoreMax, payload, sourceHash, now)
		}
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE call_quality_review_revisions SET overall_comment=$2,human_score=$3,score_max=$4,payload_json=$5,source_hash=$6,updated_at=$7 WHERE revision_uuid=$1`, revisionID, strings.TrimSpace(in.OverallComment), score, scoreMax, payload, sourceHash, now)
	}
	if err != nil {
		return q, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM call_quality_review_criteria WHERE revision_uuid=$1`, revisionID); err != nil {
		return q, err
	}
	for _, c := range normalized {
		_, err = tx.ExecContext(ctx, `INSERT INTO call_quality_review_criteria(criterion_uuid,revision_uuid,criterion_key,title_snapshot,ai_score,human_score,score_min,score_max,weight,decision,comment,position) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, uuid.New(), revisionID, c.Key, c.Title, c.AIScore, c.HumanScore, c.ScoreMin, c.ScoreMax, c.Weight, c.Decision, c.Comment, c.Position)
		if err != nil {
			return q, err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE call_quality_reviews SET assignee_user_uuid=COALESCE(assignee_user_uuid,$2),status='in_review',lock_version=lock_version+1,updated_at=$3 WHERE review_uuid=$1 AND lock_version=$4`, q.ID, in.ActorUserUUID, now, q.LockVersion)
	if err != nil {
		return q, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return q, ErrVersionConflict
	}
	if err = insertEvent(ctx, tx, q.ID, uuid.NullUUID{UUID: revisionID, Valid: true}, uuid.NullUUID{}, in.ActorUserUUID, "draft_saved", access, nil, nil); err != nil {
		return q, err
	}
	if err = tx.Commit(); err != nil {
		return q, err
	}
	return s.Get(ctx, q.ID, in.ActorUserUUID)
}

func (s *Service) Publish(ctx context.Context, id, draftID, actor uuid.UUID, expected int64) (models.QualityReview, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QualityReview{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q, analysis, err := loadReviewTx(ctx, tx, id, true)
	if err != nil {
		return q, err
	}
	access, err := authorizeTx(ctx, tx, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, actor)
	if err != nil || !access.CanReview {
		return q, ErrForbidden
	}
	if q.LockVersion != expected {
		return q, ErrVersionConflict
	}
	if sourceOutdatedTx(ctx, tx, q) {
		return q, ErrSourceOutdated
	}
	var author uuid.UUID
	var overall, sourceHash string
	err = tx.QueryRowContext(ctx, `SELECT author_user_uuid,COALESCE(overall_comment,''),source_hash FROM call_quality_review_revisions WHERE revision_uuid=$1 AND review_uuid=$2 AND status='draft' FOR UPDATE`, draftID, id).Scan(&author, &overall, &sourceHash)
	if errors.Is(err, sql.ErrNoRows) {
		return q, ErrNotFound
	}
	if err != nil {
		return q, err
	}
	if author != actor {
		return q, ErrForbidden
	}
	var publishedCount int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM call_quality_review_revisions WHERE review_uuid=$1 AND status IN ('published','superseded')`, id).Scan(&publishedCount); err != nil {
		return q, err
	}
	limit := 2
	if !q.CompanyUUID.Valid {
		limit = 1
	}
	if publishedCount >= limit {
		return q, ErrReviewLimitReached
	}
	if publishedCount == 1 && q.Status != models.QualityReviewAppealed && access.CompanyRole != string(models.CompanyMemberRoleManager) {
		return q, ErrForbidden
	}
	if q.ActiveRevisionUUID.Valid {
		var previousAuthor uuid.UUID
		if err = tx.QueryRowContext(ctx, `SELECT author_user_uuid FROM call_quality_review_revisions WHERE revision_uuid=$1`, q.ActiveRevisionUUID.UUID).Scan(&previousAuthor); err != nil {
			return q, err
		}
		if previousAuthor == actor {
			return q, ErrReviewerMustDiffer
		}
	}
	if sourceHash != hashSource(q.AnalysisUUID, q.TranscriptionRevision, analysis) {
		return q, ErrSourceOutdated
	}
	if strings.TrimSpace(overall) != "" && !validComment(overall, 3, 5000) {
		return q, ErrPublicationBlocked
	}
	source, err := parseSourceCriteria(analysis)
	if err != nil {
		return q, ErrInvalidInput
	}
	sourceKeys := make(map[string]struct{}, len(source))
	for _, criterion := range source {
		sourceKeys[criterion.Key] = struct{}{}
	}
	rows, err := tx.QueryContext(ctx, `SELECT criterion_key,title_snapshot,human_score,score_max,decision FROM call_quality_review_criteria WHERE revision_uuid=$1`, draftID)
	if err != nil {
		return q, err
	}
	editedCount := 0
	for rows.Next() {
		var key, title, decision string
		var human, max sql.NullFloat64
		if err = rows.Scan(&key, &title, &human, &max, &decision); err != nil {
			_ = rows.Close()
			return q, err
		}
		if decision == string(models.QualityDecisionUnscored) {
			continue
		}
		editedCount++
		if _, builtIn := sourceKeys[key]; !builtIn && !validComment(title, 3, 200) {
			_ = rows.Close()
			return q, ErrPublicationBlocked
		}
		if decision == string(models.QualityDecisionOverridden) && !human.Valid {
			_ = rows.Close()
			return q, ErrPublicationBlocked
		}
	}
	_ = rows.Close()
	if editedCount == 0 && strings.TrimSpace(overall) == "" {
		return q, ErrPublicationBlocked
	}
	now := time.Now().UTC()
	if q.ActiveRevisionUUID.Valid {
		if _, err = tx.ExecContext(ctx, `UPDATE call_quality_review_revisions SET status='superseded' WHERE revision_uuid=$1 AND status='published'`, q.ActiveRevisionUUID.UUID); err != nil {
			return q, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_quality_review_revisions SET status='published',published_at=$2,updated_at=$2 WHERE revision_uuid=$1`, draftID, now); err != nil {
		return q, err
	}
	nextStatus := models.QualityReviewPublished
	if publishedCount == 1 {
		nextStatus = models.QualityReviewResolved
	}
	res, err := tx.ExecContext(ctx, `UPDATE call_quality_reviews SET active_revision_uuid=$2,status=$3,published_at=$4,updated_at=$4,lock_version=lock_version+1 WHERE review_uuid=$1 AND lock_version=$5`, id, draftID, nextStatus, now, q.LockVersion)
	if err != nil {
		return q, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return q, ErrVersionConflict
	}
	eventType := "human_review_1_published"
	if publishedCount == 1 {
		eventType = "human_review_2_published"
		if _, err = tx.ExecContext(ctx, `UPDATE call_quality_review_appeals SET status='accepted',resolution_comment=COALESCE(NULLIF($2,''),'Опубликована независимая переоценка'),resolved_by_user_uuid=$3,resolved_at=$4,updated_at=$4,lock_version=lock_version+1 WHERE review_uuid=$1 AND status IN ('open','in_review')`, id, strings.TrimSpace(overall), actor, now); err != nil {
			return q, err
		}
	}
	if err = insertEvent(ctx, tx, id, uuid.NullUUID{UUID: draftID, Valid: true}, uuid.NullUUID{}, actor, eventType, access, nil, nil); err != nil {
		return q, err
	}
	if err = tx.Commit(); err != nil {
		return q, err
	}
	return s.Get(ctx, id, actor)
}

func (s *Service) DiscardDraft(ctx context.Context, id, actor uuid.UUID, expected int64) (models.QualityReview, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.QualityReview{}, err
	}
	defer func() { _ = tx.Rollback() }()
	q, _, err := loadReviewTx(ctx, tx, id, true)
	if err != nil {
		return q, err
	}
	access, err := authorizeTx(ctx, tx, q.CallUUID, q.CompanyUUID, q.DepartmentUUID, actor)
	if err != nil || !access.CanReview {
		return q, ErrForbidden
	}
	if q.AssigneeUserUUID.Valid && q.AssigneeUserUUID.UUID != actor && access.CompanyRole != string(models.CompanyMemberRoleManager) {
		return q, ErrForbidden
	}
	if q.LockVersion != expected {
		return q, ErrVersionConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_quality_review_events SET revision_uuid=NULL WHERE revision_uuid IN (SELECT revision_uuid FROM call_quality_review_revisions WHERE review_uuid=$1 AND author_user_uuid=$2 AND status='draft')`, id, actor); err != nil {
		return q, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM call_quality_review_revisions WHERE review_uuid=$1 AND author_user_uuid=$2 AND status='draft'`, id, actor); err != nil {
		return q, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE call_quality_reviews SET lock_version=lock_version+1,updated_at=now() WHERE review_uuid=$1 AND lock_version=$2`, id, q.LockVersion)
	if err != nil {
		return q, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return q, ErrVersionConflict
	}
	if err = insertEvent(ctx, tx, id, uuid.NullUUID{}, uuid.NullUUID{}, actor, "draft_discarded", access, nil, nil); err != nil {
		return q, err
	}
	if err = tx.Commit(); err != nil {
		return q, err
	}
	return s.Get(ctx, id, actor)
}

func normalizeDraft(source []sourceCriterion, input []CriterionInput, publish bool) ([]models.QualityReviewCriterion, *float64, *float64, error) {
	sourceKeys := make(map[string]struct{}, len(source))
	for _, criterion := range source {
		sourceKeys[criterion.Key] = struct{}{}
	}
	byKey := make(map[string]CriterionInput, len(input))
	custom := make([]sourceCriterion, 0)
	for _, c := range input {
		c.Key = strings.TrimSpace(c.Key)
		if c.Key == "" {
			return nil, nil, nil, ErrInvalidInput
		}
		if _, ok := sourceKeys[c.Key]; !ok {
			if !c.Custom || (publish && !validComment(c.Title, 3, 200)) || (!publish && utf8.RuneCountInString(strings.TrimSpace(c.Title)) > 200) {
				return nil, nil, nil, ErrInvalidInput
			}
			custom = append(custom, sourceCriterion{Key: c.Key, Title: strings.TrimSpace(c.Title), Min: 0, Max: 10, Weight: 1, Position: len(source) + len(custom)})
			sourceKeys[c.Key] = struct{}{}
		}
		if _, ok := byKey[c.Key]; ok {
			return nil, nil, nil, ErrInvalidInput
		}
		byKey[c.Key] = c
	}
	source = append(source, custom...)
	out := make([]models.QualityReviewCriterion, 0, len(source))
	var weighted, totalWeight float64
	for _, s := range source {
		in, ok := byKey[s.Key]
		if !ok {
			in = CriterionInput{Key: s.Key}
		}
		comment := strings.TrimSpace(in.Comment)
		decision := models.QualityDecisionUnscored
		human := in.HumanScore
		if in.NotApplicable {
			human = nil
			decision = models.QualityDecisionNotApplicable
		} else if human != nil {
			if math.IsNaN(*human) || math.IsInf(*human, 0) || *human < s.Min || *human > s.Max {
				return nil, nil, nil, ErrInvalidInput
			}
			if s.Score != nil && math.Abs(*human-*s.Score) < 0.000001 {
				decision = models.QualityDecisionConfirmed
			} else {
				decision = models.QualityDecisionOverridden
			}
		}
		out = append(out, models.QualityReviewCriterion{ID: uuid.New(), Key: s.Key, Title: s.Title, AIScore: s.Score, HumanScore: human, ScoreMin: &s.Min, ScoreMax: &s.Max, Weight: s.Weight, Decision: decision, Comment: comment, Position: s.Position})
		effective := human
		if effective == nil && !in.NotApplicable {
			effective = s.Score
		}
		if effective != nil && !in.NotApplicable {
			weighted += ((*effective - s.Min) / (s.Max - s.Min)) * s.Weight
			totalWeight += s.Weight
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	if totalWeight == 0 {
		return out, nil, nil, nil
	}
	score, max := weighted/totalWeight*100, 100.0
	return out, &score, &max, nil
}

func parseSourceCriteria(raw []byte) ([]sourceCriterion, error) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, ErrInvalidInput
	}
	items, ok := payload["criteria_results"].([]any)
	if !ok && numberValue(payload["schema_version"], 0) == 3 {
		items, ok = payload["items"].([]any)
	}
	if !ok || len(items) == 0 {
		return nil, ErrInvalidInput
	}
	out := make([]sourceCriterion, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := stringValue(m["code"])
		if key == "" {
			key = stringValue(m["id"])
		}
		if key == "" {
			key = fmt.Sprintf("criterion_%d", i+1)
		}
		if seen[key] {
			key = fmt.Sprintf("%s_%d", key, i+1)
		}
		seen[key] = true
		title := stringValue(m["title"])
		if title == "" {
			title = stringValue(m["topic"])
		}
		if title == "" {
			title = key
		}
		max := numberValue(m["points_max"], 100)
		min := 0.0
		score := numberPointer(m["score"])
		if v := numberPointer(m["points_awarded"]); v != nil {
			score = v
		}
		if max <= min {
			max = 100
		}
		weight := numberValue(m["weight"], 1)
		if weight < 0 {
			weight = 1
		}
		out = append(out, sourceCriterion{Key: key, Title: title, Score: score, Min: min, Max: max, Weight: weight, Position: i})
	}
	if len(out) == 0 {
		return nil, ErrInvalidInput
	}
	return out, nil
}

func validComment(v string, min, max int) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(v))
	return n >= min && n <= max
}
func stringValue(v any) string { s, _ := v.(string); return strings.TrimSpace(s) }
func numberPointer(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	case int:
		f := float64(n)
		return &f
	}
	return nil
}
func numberValue(v any, fallback float64) float64 {
	if n := numberPointer(v); n != nil {
		return *n
	}
	return fallback
}
func hashSource(id uuid.UUID, revision int, raw []byte) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s:%d:", id, revision)
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}
func nullableUUID(v uuid.NullUUID) any {
	if v.Valid {
		return v.UUID
	}
	return nil
}
