package qualityreview

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type CreateCommentInput struct {
	CallUUID, AnalysisUUID, ActorUserUUID uuid.UUID
	Body                                  string
	CriterionKey                          string
}

type UpdateCommentInput struct {
	CommentUUID, ActorUserUUID uuid.UUID
	Body                       string
	ExpectedVersion            int64
}

func normalizeCommentBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > 4000 {
		return "", ErrInvalidInput
	}
	return body, nil
}

func (s *Service) commentAccess(ctx context.Context, callID, analysisID, actor uuid.UUID) (bool, error) {
	var company, department, uploader uuid.NullUUID
	var visibility string
	err := s.db.QueryRowContext(ctx, `SELECT c.company_uuid,c.department_uuid,c.uploaded_by_user_uuid,c.visibility_scope FROM calls c JOIN call_analyses a ON a.call_uuid=c.call_uuid WHERE c.call_uuid=$1 AND a.analysis_uuid=$2`, callID, analysisID).Scan(&company, &department, &uploader, &visibility)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	access, err := authorize(ctx, s.db, callID, company, department, actor)
	if err != nil {
		return false, err
	}
	isAuthor := uploader.Valid && uploader.UUID == actor
	if visibility == string(models.CallVisibilityScopePersonal) {
		return isAuthor, nil
	}
	if access.CanReview {
		return true, nil
	}
	if !company.Valid {
		return false, nil
	}
	var isSubject bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_quality_reviews WHERE analysis_uuid=$1 AND reviewed_subject_user_uuid=$2 AND status<>'canceled')`, analysisID, actor).Scan(&isSubject); err != nil {
		return false, err
	}
	if !isAuthor && !isSubject {
		return false, nil
	}
	var active bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active')`, company.UUID, actor).Scan(&active)
	return active, err
}

func (s *Service) ListAnalysisComments(ctx context.Context, callID, analysisID, actor uuid.UUID) ([]models.AnalysisComment, error) {
	allowed, err := s.commentAccess(ctx, callID, analysisID, actor)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.comment_uuid,c.call_uuid,c.analysis_uuid,c.author_user_uuid,COALESCE(NULLIF(btrim(concat_ws(' ',p.full_name,p.full_surname)),''),p.username,'Пользователь'),c.body,c.criterion_key,c.created_at,c.edited_at,c.lock_version FROM call_analysis_comments c LEFT JOIN user_profiles p ON p.user_uuid=c.author_user_uuid WHERE c.call_uuid=$1 AND c.analysis_uuid=$2 ORDER BY c.created_at,c.comment_uuid`, callID, analysisID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]models.AnalysisComment, 0)
	for rows.Next() {
		var item models.AnalysisComment
		var edited sql.NullTime
		var criterion sql.NullString
		if err = rows.Scan(&item.ID, &item.CallUUID, &item.AnalysisUUID, &item.AuthorUserUUID, &item.AuthorName, &item.Body, &criterion, &item.CreatedAt, &edited, &item.LockVersion); err != nil {
			return nil, err
		}
		if criterion.Valid {
			item.CriterionKey = &criterion.String
		}
		item.CanEdit = item.AuthorUserUUID == actor
		if edited.Valid {
			item.EditedAt = &edited.Time
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateAnalysisComment(ctx context.Context, in CreateCommentInput) (models.AnalysisComment, error) {
	if in.CallUUID == uuid.Nil || in.AnalysisUUID == uuid.Nil || in.ActorUserUUID == uuid.Nil {
		return models.AnalysisComment{}, ErrInvalidInput
	}
	body, err := normalizeCommentBody(in.Body)
	if err != nil {
		return models.AnalysisComment{}, err
	}
	allowed, err := s.commentAccess(ctx, in.CallUUID, in.AnalysisUUID, in.ActorUserUUID)
	if err != nil {
		return models.AnalysisComment{}, err
	}
	if !allowed {
		return models.AnalysisComment{}, ErrForbidden
	}
	criterionKey := strings.TrimSpace(in.CriterionKey)
	if len([]rune(criterionKey)) > 200 {
		return models.AnalysisComment{}, ErrInvalidInput
	}
	id, now := uuid.New(), time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO call_analysis_comments(comment_uuid,call_uuid,analysis_uuid,author_user_uuid,body,criterion_key,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, in.CallUUID, in.AnalysisUUID, in.ActorUserUUID, body, nullString(criterionKey), now)
	if err != nil {
		return models.AnalysisComment{}, err
	}
	items, err := s.ListAnalysisComments(ctx, in.CallUUID, in.AnalysisUUID, in.ActorUserUUID)
	if err != nil {
		return models.AnalysisComment{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return models.AnalysisComment{}, ErrNotFound
}

func (s *Service) UpdateAnalysisComment(ctx context.Context, in UpdateCommentInput) (models.AnalysisComment, error) {
	if in.CommentUUID == uuid.Nil || in.ActorUserUUID == uuid.Nil || in.ExpectedVersion < 1 {
		return models.AnalysisComment{}, ErrInvalidInput
	}
	body, err := normalizeCommentBody(in.Body)
	if err != nil {
		return models.AnalysisComment{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AnalysisComment{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var callID, analysisID, author uuid.UUID
	var previous string
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT call_uuid,analysis_uuid,author_user_uuid,body,lock_version FROM call_analysis_comments WHERE comment_uuid=$1 FOR UPDATE`, in.CommentUUID).Scan(&callID, &analysisID, &author, &previous, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AnalysisComment{}, ErrNotFound
	}
	if err != nil {
		return models.AnalysisComment{}, err
	}
	allowed, err := s.commentAccess(ctx, callID, analysisID, in.ActorUserUUID)
	if err != nil {
		return models.AnalysisComment{}, err
	}
	if !allowed || author != in.ActorUserUUID {
		return models.AnalysisComment{}, ErrForbidden
	}
	if version != in.ExpectedVersion {
		return models.AnalysisComment{}, ErrVersionConflict
	}
	if body != previous {
		now := time.Now().UTC()
		if _, err = tx.ExecContext(ctx, `INSERT INTO call_analysis_comment_revisions(revision_uuid,comment_uuid,editor_user_uuid,previous_body,created_at) VALUES($1,$2,$3,$4,$5)`, uuid.New(), in.CommentUUID, in.ActorUserUUID, previous, now); err != nil {
			return models.AnalysisComment{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE call_analysis_comments SET body=$1,edited_at=$2,lock_version=lock_version+1 WHERE comment_uuid=$3`, body, now, in.CommentUUID); err != nil {
			return models.AnalysisComment{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return models.AnalysisComment{}, err
	}
	items, err := s.ListAnalysisComments(ctx, callID, analysisID, in.ActorUserUUID)
	if err != nil {
		return models.AnalysisComment{}, err
	}
	for _, item := range items {
		if item.ID == in.CommentUUID {
			return item, nil
		}
	}
	return models.AnalysisComment{}, ErrNotFound
}
