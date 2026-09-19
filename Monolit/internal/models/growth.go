package models

import "errors"

var (
	ErrGrowthAreaNotFound      = errors.New("growth area not found")
	ErrInvalidGrowthAreaReason = errors.New("a reason is required to hide a growth area")
	ErrGrowthAreaNotDismissed  = errors.New("growth area is not hidden")
)

// GrowthContext is what the summary step is told about the employee's open
// growth areas. A request without it runs the step exactly as before growth
// areas existed.
type GrowthContext struct {
	SubjectSpeaker string          `json:"subject_speaker"`
	OpenAreas      []GrowthAreaRef `json:"open_growth_areas"`
}

type GrowthAreaRef struct {
	ID          string `json:"area_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// GrowthObservation is the summary step's verdict on one open growth area.
type GrowthObservation struct {
	AreaID  string   `json:"area_id"`
	Verdict string   `json:"verdict"`
	ItemIDs []string `json:"item_ids"`
	Note    string   `json:"note"`
}

// NewGrowthArea is a shortcomings the summary step found for the first time.
type NewGrowthArea struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ItemIDs     []string `json:"item_ids"`
}

// GrowthOutcome is what one analysis says about growth areas. It is stored in
// tables only; the result JSON never carries it.
type GrowthOutcome struct {
	Observations []GrowthObservation
	NewAreas     []NewGrowthArea
	// ItemTitles are the titles of the analysis cards by ID, so a note that
	// cites a card by its ID can name it instead.
	ItemTitles map[string]string
}

// Growth observation verdicts.
const (
	GrowthVerdictNew           = "new"
	GrowthVerdictRepeated      = "repeated"
	GrowthVerdictImproved      = "improved"
	GrowthVerdictNotApplicable = "not_applicable"
)
