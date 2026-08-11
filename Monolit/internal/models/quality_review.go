package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type QualityReviewStatus string
type QualityReviewRevisionStatus string
type QualityReviewDecision string
type QualityReviewAppealStatus string

const (
	QualityReviewUnassigned QualityReviewStatus = "unassigned"
	QualityReviewAssigned   QualityReviewStatus = "assigned"
	QualityReviewInReview   QualityReviewStatus = "in_review"
	QualityReviewPublished  QualityReviewStatus = "published"
	QualityReviewAppealed   QualityReviewStatus = "appealed"
	QualityReviewResolved   QualityReviewStatus = "resolved"
	QualityReviewCanceled   QualityReviewStatus = "canceled"

	QualityRevisionDraft      QualityReviewRevisionStatus = "draft"
	QualityRevisionPublished  QualityReviewRevisionStatus = "published"
	QualityRevisionSuperseded QualityReviewRevisionStatus = "superseded"

	QualityDecisionConfirmed     QualityReviewDecision = "confirmed"
	QualityDecisionOverridden    QualityReviewDecision = "overridden"
	QualityDecisionNotApplicable QualityReviewDecision = "not_applicable"
	QualityDecisionUnscored      QualityReviewDecision = "unscored"
)

type QualityReview struct {
	ID                    uuid.UUID                 `json:"review_uuid"`
	CallUUID              uuid.UUID                 `json:"call_uuid"`
	AnalysisUUID          uuid.UUID                 `json:"analysis_uuid"`
	AnalysisAttemptUUID   uuid.NullUUID             `json:"-"`
	AnalysisAttemptID     *uuid.UUID                `json:"analysis_attempt_uuid,omitempty"`
	TranscriptionRevision int                       `json:"transcription_revision"`
	CompanyUUID           uuid.NullUUID             `json:"-"`
	CompanyID             *uuid.UUID                `json:"company_uuid"`
	DepartmentUUID        uuid.NullUUID             `json:"-"`
	DepartmentID          *uuid.UUID                `json:"department_uuid,omitempty"`
	SubjectUserUUID       uuid.NullUUID             `json:"-"`
	SubjectUserID         *uuid.UUID                `json:"reviewed_subject_user_uuid,omitempty"`
	AssigneeUserUUID      uuid.NullUUID             `json:"-"`
	AssigneeUserID        *uuid.UUID                `json:"assignee_user_uuid,omitempty"`
	Status                QualityReviewStatus       `json:"status"`
	ActiveRevisionUUID    uuid.NullUUID             `json:"-"`
	ActiveRevisionID      *uuid.UUID                `json:"active_revision_uuid,omitempty"`
	LockVersion           int64                     `json:"lock_version"`
	DueAt                 *time.Time                `json:"due_at,omitempty"`
	CreatedByUserUUID     uuid.UUID                 `json:"created_by_user_uuid"`
	CreatedAt             time.Time                 `json:"created_at"`
	UpdatedAt             time.Time                 `json:"updated_at"`
	PublishedAt           *time.Time                `json:"published_at,omitempty"`
	SourceOutdated        bool                      `json:"source_outdated"`
	Capabilities          QualityReviewCapabilities `json:"capabilities"`
	Analysis              json.RawMessage           `json:"analysis"`
	Draft                 *QualityReviewRevision    `json:"draft,omitempty"`
	PublishedRevision     *QualityReviewRevision    `json:"published_revision,omitempty"`
	Revisions             []QualityReviewRevision   `json:"revisions"`
	Appeals               []QualityReviewAppeal     `json:"appeals"`
	Challenge             *QualityReviewChallenge   `json:"challenge,omitempty"`
	EffectiveAnalysis     *EffectiveAnalysis        `json:"effective_analysis,omitempty"`
}

type QualityReviewChallenge struct {
	ID             uuid.UUID `json:"challenge_uuid"`
	ReviewUUID     uuid.UUID `json:"review_uuid"`
	AnalysisUUID   uuid.UUID `json:"analysis_uuid"`
	CallUUID       uuid.UUID `json:"call_uuid"`
	AuthorUserUUID uuid.UUID `json:"author_user_uuid"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type QualityReviewEvent struct {
	ID            uuid.UUID       `json:"event_uuid"`
	ReviewUUID    uuid.UUID       `json:"review_uuid"`
	RevisionUUID  *uuid.UUID      `json:"revision_uuid,omitempty"`
	AppealUUID    *uuid.UUID      `json:"appeal_uuid,omitempty"`
	ActorUserUUID uuid.UUID       `json:"actor_user_uuid"`
	EventType     string          `json:"event_type"`
	Before        json.RawMessage `json:"before,omitempty"`
	After         json.RawMessage `json:"after,omitempty"`
	Reason        *string         `json:"reason,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

type QualityReviewCriterion struct {
	ID         uuid.UUID             `json:"criterion_uuid"`
	Key        string                `json:"criterion_key"`
	Title      string                `json:"title"`
	AIScore    *float64              `json:"ai_score,omitempty"`
	HumanScore *float64              `json:"human_score,omitempty"`
	ScoreMin   *float64              `json:"score_min,omitempty"`
	ScoreMax   *float64              `json:"score_max,omitempty"`
	Weight     float64               `json:"weight"`
	Decision   QualityReviewDecision `json:"decision"`
	Comment    string                `json:"comment"`
	Position   int                   `json:"position"`
}

type QualityReviewRevision struct {
	ID             uuid.UUID                   `json:"revision_uuid"`
	Number         int                         `json:"revision_number"`
	AuthorUserUUID uuid.UUID                   `json:"author_user_uuid"`
	Status         QualityReviewRevisionStatus `json:"status"`
	OverallComment string                      `json:"overall_comment"`
	HumanScore     *float64                    `json:"human_score,omitempty"`
	ScoreMax       *float64                    `json:"score_max,omitempty"`
	Payload        json.RawMessage             `json:"payload"`
	SourceHash     string                      `json:"source_hash"`
	Criteria       []QualityReviewCriterion    `json:"criteria"`
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
	PublishedAt    *time.Time                  `json:"published_at,omitempty"`
}

type QualityReviewCapabilities struct {
	CanClaim           bool `json:"can_claim"`
	CanEdit            bool `json:"can_edit"`
	CanPublish         bool `json:"can_publish"`
	CanAppeal          bool `json:"can_appeal"`
	CanResolveAppeal   bool `json:"can_resolve_appeal"`
	CanViewEvents      bool `json:"can_view_events"`
	CanEditAnalysis    bool `json:"can_edit_analysis"`
	CanDisputeAnalysis bool `json:"can_dispute_analysis"`
	CanResolveDispute  bool `json:"can_resolve_dispute"`
}

type AnalysisReviewContext struct {
	ReviewUUID                        *uuid.UUID                `json:"review_uuid,omitempty"`
	Status                            *QualityReviewStatus      `json:"status,omitempty"`
	Capabilities                      QualityReviewCapabilities `json:"capabilities"`
	HumanReviewCount                  int                       `json:"human_review_count"`
	HumanReviewLimit                  int                       `json:"human_review_limit"`
	NextReviewRequiresDifferentAuthor bool                      `json:"next_review_requires_different_author"`
	ActiveScoreSource                 string                    `json:"active_score_source"`
	SourceOutdated                    bool                      `json:"source_outdated"`
	Challenge                         *QualityReviewChallenge   `json:"challenge,omitempty"`
	EffectiveAnalysis                 *EffectiveAnalysis        `json:"effective_analysis,omitempty"`
}

type EffectiveAnalysis struct {
	TotalScore *float64                     `json:"total_score,omitempty"`
	Source     string                       `json:"source"`
	Criteria   []EffectiveAnalysisCriterion `json:"criteria"`
}

type EffectiveAnalysisCriterion struct {
	Key               string   `json:"criterion_key"`
	Title             string   `json:"title"`
	AIScore           *float64 `json:"ai_score,omitempty"`
	HumanReview1Score *float64 `json:"human_review_1_score,omitempty"`
	HumanReview2Score *float64 `json:"human_review_2_score,omitempty"`
	EffectiveScore    *float64 `json:"effective_score,omitempty"`
	EffectiveSource   string   `json:"effective_source"`
	ScoreMin          *float64 `json:"score_min,omitempty"`
	ScoreMax          *float64 `json:"score_max,omitempty"`
	Weight            float64  `json:"weight"`
	NotApplicable     bool     `json:"not_applicable"`
}

type QualityReviewAppeal struct {
	ID                 uuid.UUID                 `json:"appeal_uuid"`
	ReviewUUID         uuid.UUID                 `json:"review_uuid"`
	RevisionUUID       uuid.UUID                 `json:"revision_uuid"`
	AuthorUserUUID     uuid.UUID                 `json:"author_user_uuid"`
	Status             QualityReviewAppealStatus `json:"status"`
	Reason             string                    `json:"reason"`
	ResolutionComment  *string                   `json:"resolution_comment,omitempty"`
	ResolvedByUserUUID uuid.NullUUID             `json:"-"`
	ResolvedByUserID   *uuid.UUID                `json:"resolved_by_user_uuid,omitempty"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
	ResolvedAt         *time.Time                `json:"resolved_at,omitempty"`
	LockVersion        int64                     `json:"lock_version"`
}
