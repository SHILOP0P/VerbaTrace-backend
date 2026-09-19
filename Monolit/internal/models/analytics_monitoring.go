package models

import (
	"time"

	"github.com/google/uuid"
)

type AnalyticsOverviewInput struct {
	UserID          uuid.UUID
	VisibilityScope CallVisibilityScope
	CompanyUUID     uuid.NullUUID
	DepartmentUUID  uuid.NullUUID
	From            *time.Time
	To              *time.Time
	FolderUUID      uuid.NullUUID
}

type AnalyticsTopicCount struct {
	Title string
	Count int
}

type AnalyticsOverview struct {
	CallsTotal             int
	CallsCreatedToday      int
	CallsNew               int
	CallsProcessing        int
	CallsTranscribed       int
	CallsWithTranscription int
	CallsAnalyzed          int
	CallsFailed            int
	AverageDurationSeconds *int
	AverageQualityScore    *float64
	QualityScoreScale      int
	AverageScore           *float64
	ScoreScale             int
	ScoreDistribution      AnalyticsScoreDistribution
	CriteriaSummary        []AnalyticsCriterionSummary
	TopWeakCriteria        []AnalyticsWeakCriterion
	TopIssueCodes          []AnalyticsCodeCount
	BusinessOutcomes       []AnalyticsStatusCount
	NextStepSummary        AnalyticsNextStepSummary
	TopTopics              []AnalyticsTopicCount
	RisksCount             *int
	RecommendationsCount   *int
	Charts                 AnalyticsCharts
	// TeamAnalyticsEnabled is false when a company's plan does not include team
	// analytics: the counters and the overall score stay, the breakdowns go.
	TeamAnalyticsEnabled bool
}

type AnalyticsScoreDistribution struct {
	Critical  int
	Weak      int
	Normal    int
	Good      int
	Excellent int
}

type AnalyticsCriterionSummary struct {
	Code          string
	Title         string
	AverageScore  *float64
	Met           int
	PartiallyMet  int
	Missed        int
	Unclear       int
	NotApplicable int
	CallsCount    int
}

type AnalyticsWeakCriterion struct {
	Code              string
	Title             string
	AverageScore      *float64
	MissedCount       int
	PartiallyMetCount int
}

type AnalyticsCodeCount struct {
	Code  string
	Count int
}

type AnalyticsStatusCount struct {
	Status string
	Count  int
}

type AnalyticsNextStepSummary struct {
	WithNextStep          int
	Specific              int
	WithDeadline          int
	WithResponsiblePerson int
	Missing               int
}

type AnalyticsCharts struct {
	CallsByDay    []AnalyticsCountPoint
	AnalyzedByDay []AnalyticsCountPoint
	QualityByDay  []AnalyticsQualityPoint
	ScoreByDay    []AnalyticsScorePoint
	DurationByDay []AnalyticsDurationPoint
	RisksByDay    []AnalyticsCountPoint
}

type AnalyticsCountPoint struct {
	Date  string
	Count int
}

type AnalyticsQualityPoint struct {
	Date                string
	AverageQualityScore float64
}

type AnalyticsScorePoint struct {
	Date         string
	AverageScore float64
}

type AnalyticsDurationPoint struct {
	Date                   string
	AverageDurationSeconds int
}

type ProcessingMonitoringInput struct {
	UserID      uuid.UUID
	UserRole    UserRole
	CompanyUUID uuid.NullUUID
	From        *time.Time
	To          *time.Time
}

type ProcessingQueueSummary struct {
	Pending int
	Running int
	Done    int
	Failed  int
	Retry   int
}

type FailedProcessingJob struct {
	ID         uuid.UUID
	Type       ProcessingJobType
	EntityUUID uuid.UUID
	Attempts   int
	LastError  *string
	UpdatedAt  time.Time
}

type ProcessingServicesStatus struct {
	Transcriber string
	Analyzer    string
	Storage     string
}

type ProcessingMonitoring struct {
	Queue                    ProcessingQueueSummary
	AverageProcessingSeconds *int
	LastFailedJobs           []FailedProcessingJob
	Services                 ProcessingServicesStatus
}
