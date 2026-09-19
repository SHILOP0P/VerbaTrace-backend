package dto

import "time"

type ScorecardResponse struct {
	ScorecardUUID            *string                    `json:"scorecard_uuid"`
	InstructionUUID          string                     `json:"instruction_uuid"`
	InstructionTitle         string                     `json:"instruction_title"`
	InstructionVersionUUID   string                     `json:"instruction_version_uuid"`
	InstructionVersion       int                        `json:"instruction_version"`
	Revision                 int                        `json:"revision"`
	Status                   string                     `json:"status"`
	Origin                   string                     `json:"origin"`
	IsCurrent                bool                       `json:"is_current"`
	AwaitingConfirmation     bool                       `json:"awaiting_confirmation"`
	ConfirmRequired          bool                       `json:"confirm_required"`
	LockVersion              int                        `json:"lock_version"`
	CompileAfter             *time.Time                 `json:"compile_after"`
	EnabledCount             int                        `json:"enabled_count"`
	EstimatedRequestsPerCall int                        `json:"estimated_requests_per_call"`
	Criteria                 []ScorecardCriterionItem   `json:"criteria"`
	RemovedCriteria          []ScorecardRemovedCriteria `json:"removed_criteria"`
	Error                    *ScorecardError            `json:"error"`
	ConfirmedAt              *time.Time                 `json:"confirmed_at"`
	CreatedAt                *time.Time                 `json:"created_at"`
}

type ScorecardCriterionItem struct {
	CriterionKey     string                    `json:"criterion_key"`
	Position         int                       `json:"position"`
	Title            string                    `json:"title"`
	Requirement      string                    `json:"requirement"`
	SourceExcerpt    string                    `json:"source_excerpt"`
	Applicability    string                    `json:"applicability"`
	Depth            string                    `json:"depth"`
	RequiredQuestion bool                      `json:"required_question"`
	CrossCutting     bool                      `json:"cross_cutting"`
	Weight           int                       `json:"weight"`
	IsCritical       bool                      `json:"is_critical"`
	Enabled          bool                      `json:"enabled"`
	EditedFields     []string                  `json:"edited_fields"`
	ChangeKind       string                    `json:"change_kind"`
	Warnings         []string                  `json:"warnings"`
	SameAs           *ScorecardRemovedCriteria `json:"same_as"`
}

type ScorecardRemovedCriteria struct {
	CriterionKey string `json:"criterion_key"`
	Title        string `json:"title"`
}

type ScorecardError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type EditScorecardRequest struct {
	LockVersion     int                          `json:"lock_version"`
	Criteria        []EditScorecardCriterionItem `json:"criteria"`
	ConfirmRequired *bool                        `json:"confirm_required"`
}

type EditScorecardCriterionItem struct {
	CriterionKey string  `json:"criterion_key"`
	Title        *string `json:"title"`
	Weight       *int    `json:"weight"`
	IsCritical   *bool   `json:"is_critical"`
	Enabled      *bool   `json:"enabled"`
}

type ConfirmScorecardRequest struct {
	ScorecardUUID string `json:"scorecard_uuid"`
	LockVersion   int    `json:"lock_version"`
}

type CriterionSameAsRequest struct {
	CanonicalKey string `json:"canonical_key"`
}
