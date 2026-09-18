package dto

type AnalyticsOverviewResponse struct {
	CallsTotal             int                         `json:"calls_total"`
	CallsCreatedToday      int                         `json:"calls_created_today"`
	CallsNew               int                         `json:"calls_new"`
	CallsProcessing        int                         `json:"calls_processing"`
	CallsTranscribed       int                         `json:"calls_transcribed"`
	CallsWithTranscription int                         `json:"calls_with_transcription"`
	CallsAnalyzed          int                         `json:"calls_analyzed"`
	CallsFailed            int                         `json:"calls_failed"`
	AverageDurationSeconds *int                        `json:"average_duration_seconds"`
	AverageQualityScore    *float64                    `json:"average_quality_score"`
	QualityScoreScale      int                         `json:"quality_score_scale"`
	AverageScore           *float64                    `json:"average_score"`
	ScoreScale             int                         `json:"score_scale"`
	ScoreDistribution      AnalyticsScoreDistribution  `json:"score_distribution"`
	CriteriaSummary        []AnalyticsCriterionSummary `json:"criteria_summary"`
	TopWeakCriteria        []AnalyticsWeakCriterion    `json:"top_weak_criteria"`
	TopIssueCodes          []AnalyticsCodeCount        `json:"top_issue_codes"`
	BusinessOutcomes       []AnalyticsStatusCount      `json:"business_outcomes"`
	NextStepSummary        AnalyticsNextStepSummary    `json:"next_step_summary"`
	TopTopics              []AnalyticsTopicItem        `json:"top_topics"`
	RisksCount             *int                        `json:"risks_count"`
	RecommendationsCount   *int                        `json:"recommendations_count"`
	Charts                 AnalyticsCharts             `json:"charts"`
	TeamAnalyticsEnabled   bool                        `json:"team_analytics_enabled"`
}

type AnalyticsScoreDistribution struct {
	Critical  int `json:"critical"`
	Weak      int `json:"weak"`
	Normal    int `json:"normal"`
	Good      int `json:"good"`
	Excellent int `json:"excellent"`
}

type AnalyticsCriterionSummary struct {
	Code          string   `json:"code"`
	Title         string   `json:"title"`
	AverageScore  *float64 `json:"average_score"`
	Met           int      `json:"met"`
	PartiallyMet  int      `json:"partially_met"`
	Missed        int      `json:"missed"`
	Unclear       int      `json:"unclear"`
	NotApplicable int      `json:"not_applicable"`
	CallsCount    int      `json:"calls_count"`
}

type AnalyticsWeakCriterion struct {
	Code              string   `json:"code"`
	Title             string   `json:"title"`
	AverageScore      *float64 `json:"average_score"`
	MissedCount       int      `json:"missed_count"`
	PartiallyMetCount int      `json:"partially_met_count"`
}

type AnalyticsCodeCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type AnalyticsStatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type AnalyticsNextStepSummary struct {
	WithNextStep          int `json:"with_next_step"`
	Specific              int `json:"specific"`
	WithDeadline          int `json:"with_deadline"`
	WithResponsiblePerson int `json:"with_responsible_person"`
	Missing               int `json:"missing"`
}

type AnalyticsTopicItem struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}

type ProcessingMonitoringResponse struct {
	Queue                    ProcessingQueueResponse `json:"queue"`
	AverageProcessingSeconds *int                    `json:"average_processing_seconds"`
}

type ProcessingQueueResponse struct {
	Pending int `json:"pending"`
	Running int `json:"running"`
	Done    int `json:"done"`
	Failed  int `json:"failed"`
	Retry   int `json:"retry"`
}

type AnalyticsCharts struct {
	CallsByDay    []AnalyticsCountPoint    `json:"calls_by_day"`
	AnalyzedByDay []AnalyticsCountPoint    `json:"analyzed_by_day"`
	QualityByDay  []AnalyticsQualityPoint  `json:"quality_by_day"`
	ScoreByDay    []AnalyticsScorePoint    `json:"score_by_day"`
	DurationByDay []AnalyticsDurationPoint `json:"duration_by_day"`
	RisksByDay    []AnalyticsCountPoint    `json:"risks_by_day"`
}

type AnalyticsCountPoint struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type AnalyticsQualityPoint struct {
	Date                string  `json:"date"`
	AverageQualityScore float64 `json:"average_quality_score"`
}

type AnalyticsScorePoint struct {
	Date         string  `json:"date"`
	AverageScore float64 `json:"average_score"`
}

type AnalyticsDurationPoint struct {
	Date                   string `json:"date"`
	AverageDurationSeconds int    `json:"average_duration_seconds"`
}
