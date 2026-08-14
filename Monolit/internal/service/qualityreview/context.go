package qualityreview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) GetAnalysisContext(ctx context.Context, callID, analysisID, actor uuid.UUID) (models.AnalysisReviewContext, error) {
	if callID == uuid.Nil || analysisID == uuid.Nil || actor == uuid.Nil {
		return models.AnalysisReviewContext{}, ErrInvalidInput
	}

	var companyID, departmentID, uploaderID uuid.NullUUID
	var visibility, analysisStatus string
	var analysis []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT c.company_uuid,c.department_uuid,c.uploaded_by_user_uuid,c.visibility_scope,a.status,a.result_json
		FROM calls c JOIN call_analyses a ON a.call_uuid=c.call_uuid
		WHERE c.call_uuid=$1 AND a.analysis_uuid=$2`, callID, analysisID).
		Scan(&companyID, &departmentID, &uploaderID, &visibility, &analysisStatus, &analysis)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AnalysisReviewContext{}, ErrNotFound
	}
	if err != nil {
		return models.AnalysisReviewContext{}, err
	}
	if analysisStatus != string(models.CallAnalysisStatusDone) || len(analysis) == 0 {
		return models.AnalysisReviewContext{}, ErrInvalidInput
	}

	access, err := authorize(ctx, s.db, callID, companyID, departmentID, actor)
	if err != nil {
		return models.AnalysisReviewContext{}, ErrNotFound
	}
	personal := visibility == string(models.CallVisibilityScopePersonal)
	isSubject := uploaderID.Valid && uploaderID.UUID == actor
	isActiveCompanyMember := false
	if companyID.Valid {
		if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active')`, companyID.UUID, actor).Scan(&isActiveCompanyMember); err != nil {
			return models.AnalysisReviewContext{}, err
		}
	}
	if personal {
		if !isSubject {
			return models.AnalysisReviewContext{}, ErrNotFound
		}
	} else if !access.CanRead && (!isSubject || !isActiveCompanyMember) {
		return models.AnalysisReviewContext{}, ErrNotFound
	}

	limit := 2
	if personal {
		limit = 1
	}
	result := models.AnalysisReviewContext{HumanReviewLimit: limit, ActiveScoreSource: "ai"}
	result.Capabilities.CanCommentAnalysis = true
	result.Comments, err = s.ListAnalysisComments(ctx, callID, analysisID, actor)
	if err != nil {
		return models.AnalysisReviewContext{}, err
	}
	result.Capabilities.CanEditAnalysis = personal && isSubject || !personal && access.CanReview && !isSubject
	result.Capabilities.CanDisputeAnalysis = !personal && isSubject && isActiveCompanyMember

	var reviewID uuid.UUID
	err = s.db.QueryRowContext(ctx, `SELECT review_uuid FROM call_quality_reviews WHERE analysis_uuid=$1 AND status<>'canceled'`, analysisID).Scan(&reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		result.EffectiveAnalysis, _ = buildEffectiveAnalysis(analysis, nil)
		return result, nil
	}
	if err != nil {
		return models.AnalysisReviewContext{}, err
	}

	review, _, err := s.loadReview(ctx, s.db, reviewID)
	if err != nil {
		return models.AnalysisReviewContext{}, err
	}
	if review.SubjectUserUUID.Valid {
		isSubject = review.SubjectUserUUID.UUID == actor
	}
	result.Capabilities.CanEditAnalysis = personal && isSubject || !personal && access.CanReview && !isSubject
	result.Capabilities.CanDisputeAnalysis = !personal && isSubject && isActiveCompanyMember
	revisions, err := s.loadPublishedRevisions(ctx, reviewID)
	if err != nil {
		return models.AnalysisReviewContext{}, err
	}
	result.ReviewUUID = &review.ID
	result.Status = &review.Status
	result.HumanReviewCount = len(revisions)
	result.NextReviewRequiresDifferentAuthor = len(revisions) == 1
	result.SourceOutdated = s.sourceOutdated(ctx, review)
	result.Challenge, _ = loadChallenge(ctx, s.db, reviewID)
	result.EffectiveAnalysis, _ = buildEffectiveAnalysis(analysis, revisions)
	if len(revisions) > 0 {
		result.ActiveScoreSource = humanReviewSource(len(revisions))
	}

	var openAppeal bool
	if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM call_quality_review_appeals WHERE review_uuid=$1 AND status IN ('open','in_review'))`, reviewID).Scan(&openAppeal); err != nil {
		return models.AnalysisReviewContext{}, err
	}
	previousAuthorMatches := len(revisions) > 0 && revisions[len(revisions)-1].AuthorUserUUID == actor
	canStartSecond := len(revisions) == 0 || review.Status == models.QualityReviewAppealed || access.CompanyRole == string(models.CompanyMemberRoleManager)
	assignmentAllowsEdit := !review.AssigneeUserUUID.Valid || review.AssigneeUserUUID.UUID == actor || access.CompanyRole == string(models.CompanyMemberRoleManager)
	canEdit := result.Capabilities.CanEditAnalysis && assignmentAllowsEdit && !result.SourceOutdated && len(revisions) < limit && !previousAuthorMatches && canStartSecond
	result.Capabilities.CanEditAnalysis = canEdit
	result.Capabilities.CanEdit = canEdit
	result.Capabilities.CanPublish = canEdit
	result.Capabilities.CanClaim = canEdit && !review.AssigneeUserUUID.Valid
	result.Capabilities.CanViewEvents = access.CanReview
	result.Capabilities.CanDisputeAnalysis = result.Capabilities.CanDisputeAnalysis && !openAppeal && (result.Challenge == nil || len(revisions) != 0)
	result.Capabilities.CanAppeal = result.Capabilities.CanDisputeAnalysis && len(revisions) > 0
	if openAppeal && access.CanReview && len(revisions) > 0 && revisions[len(revisions)-1].AuthorUserUUID != actor {
		result.Capabilities.CanResolveAppeal = true
		result.Capabilities.CanResolveDispute = true
	}
	return result, nil
}

func (s *Service) loadPublishedRevisions(ctx context.Context, reviewID uuid.UUID) ([]models.QualityReviewRevision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT revision_uuid FROM call_quality_review_revisions WHERE review_uuid=$1 AND status IN ('published','superseded') ORDER BY revision_number`, reviewID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]uuid.UUID, 0, 2)
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	items := make([]models.QualityReviewRevision, 0, len(ids))
	for _, id := range ids {
		revision, loadErr := s.loadRevision(ctx, id)
		if loadErr != nil {
			return nil, loadErr
		}
		items = append(items, revision)
	}
	return items, nil
}

func buildEffectiveAnalysis(raw []byte, revisions []models.QualityReviewRevision) (*models.EffectiveAnalysis, error) {
	source, err := parseSourceCriteria(raw)
	if err != nil {
		return nil, err
	}
	result := &models.EffectiveAnalysis{Source: "ai", Criteria: make([]models.EffectiveAnalysisCriterion, 0, len(source))}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) == nil {
		result.TotalScore = numberPointer(payload["score"])
	}
	byKey := make(map[string]int, len(source))
	for _, item := range source {
		criterion := models.EffectiveAnalysisCriterion{Key: item.Key, Title: item.Title, AIScore: item.Score, EffectiveScore: item.Score, EffectiveSource: "ai", ScoreMin: floatPointer(item.Min), ScoreMax: floatPointer(item.Max), Weight: item.Weight}
		result.Criteria = append(result.Criteria, criterion)
		byKey[item.Key] = len(result.Criteria) - 1
	}
	for index := range revisions {
		revision := revisions[index]
		for _, item := range revision.Criteria {
			criterionIndex, ok := byKey[item.Key]
			if !ok {
				result.Criteria = append(result.Criteria, models.EffectiveAnalysisCriterion{Key: item.Key, Title: item.Title, ScoreMin: item.ScoreMin, ScoreMax: item.ScoreMax, Weight: item.Weight})
				criterionIndex = len(result.Criteria) - 1
				byKey[item.Key] = criterionIndex
			}
			criterion := &result.Criteria[criterionIndex]
			switch index {
			case 0:
				criterion.HumanReview1Score = item.HumanScore
			case 1:
				criterion.HumanReview2Score = item.HumanScore
			}
			if index == len(revisions)-1 {
				criterion.NotApplicable = item.Decision == models.QualityDecisionNotApplicable
				if criterion.NotApplicable {
					criterion.EffectiveScore = nil
					criterion.EffectiveSource = humanReviewSource(index + 1)
				} else if item.HumanScore != nil {
					criterion.EffectiveScore = item.HumanScore
					criterion.EffectiveSource = humanReviewSource(index + 1)
				} else {
					criterion.EffectiveScore = criterion.AIScore
					criterion.EffectiveSource = "ai"
				}
			}
		}
		if index == len(revisions)-1 {
			result.TotalScore = revision.HumanScore
			result.Source = humanReviewSource(index + 1)
		}
	}
	return result, nil
}

func floatPointer(value float64) *float64 { return &value }

func humanReviewSource(number int) string {
	if number > 2 {
		number = 2
	}
	return "human_review_" + strconv.Itoa(number)
}
