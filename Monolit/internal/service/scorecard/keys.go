package scorecard

import (
	"slices"

	"verbatrace/monolit/internal/analyzer/scorecardflow"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Fields of a criterion an owner may change by hand.
const (
	fieldTitle      = "title"
	fieldWeight     = "weight"
	fieldIsCritical = "is_critical"
	fieldEnabled    = "enabled"
)

// assignKeys gives the compiled criteria their identity. A criterion the model
// matched to one of the previous scorecard keeps that key, so its history goes
// on; everything else is new. What the owner changed by hand on the previous
// scorecard is carried over, because the next version of the instruction does
// not know about those changes. Criteria beyond the enabled limit arrive
// switched off, so a long instruction never makes every call dearer on its own.
func assignKeys(compiled []scorecardflow.Criterion, previous []models.ScorecardCriterion, newKey func() uuid.UUID) []models.ScorecardCriterion {
	byKey := make(map[string]models.ScorecardCriterion, len(previous))
	for _, criterion := range previous {
		byKey[criterion.Key.String()] = criterion
	}
	result := make([]models.ScorecardCriterion, 0, len(compiled))
	enabled := 0
	for i, c := range compiled {
		criterion := models.ScorecardCriterion{
			Key: newKey(), Position: i + 1,
			Title: c.Title, Requirement: c.Requirement, SourceExcerpt: c.SourceExcerpt,
			Applicability: c.Applicability, Depth: c.Depth,
			RequiredQuestion: c.RequiredQuestion, CrossCutting: c.CrossCutting,
			Weight: c.Weight, IsCritical: c.IsCritical, Enabled: true,
			EditedFields: []string{}, ChangeKind: models.CriterionChangeNew,
			Warnings: append([]string{}, c.Warnings...),
		}
		if c.SameAs != nil {
			if before, ok := byKey[*c.SameAs]; ok {
				criterion.Key = before.Key
				criterion.ChangeKind = models.CriterionChangeReworded
				if scorecardflow.NormalizeTitle(before.Requirement) == scorecardflow.NormalizeTitle(c.Requirement) {
					criterion.ChangeKind = models.CriterionChangeUnchanged
				}
				carryEdits(&criterion, before)
			}
		}
		if criterion.Enabled {
			if enabled >= scorecardflow.MaxEnabled {
				criterion.Enabled = false
			} else {
				enabled++
			}
		}
		result = append(result, criterion)
	}
	return result
}

func carryEdits(criterion *models.ScorecardCriterion, before models.ScorecardCriterion) {
	for _, field := range before.EditedFields {
		switch field {
		case fieldTitle:
			criterion.Title = before.Title
		case fieldWeight:
			criterion.Weight = before.Weight
		case fieldIsCritical:
			criterion.IsCritical = before.IsCritical
		case fieldEnabled:
			criterion.Enabled = before.Enabled
		default:
			continue
		}
		if !slices.Contains(criterion.EditedFields, field) {
			criterion.EditedFields = append(criterion.EditedFields, field)
		}
	}
}

// copyCriteria repeats a scorecard for a version whose text did not change,
// for example after a rename. Nothing is compiled and nothing is paid for.
func copyCriteria(previous []models.ScorecardCriterion) []models.ScorecardCriterion {
	result := make([]models.ScorecardCriterion, 0, len(previous))
	for _, criterion := range previous {
		copied := criterion
		copied.ChangeKind = models.CriterionChangeUnchanged
		copied.EditedFields = append([]string{}, criterion.EditedFields...)
		copied.Warnings = append([]string{}, criterion.Warnings...)
		result = append(result, copied)
	}
	return result
}

// removedCriteria lists the criteria of the previous scorecard that the new one
// no longer has, unless an alias already says where they went.
func removedCriteria(current, previous []models.ScorecardCriterion, aliased map[uuid.UUID]bool) []models.RemovedCriterion {
	present := make(map[uuid.UUID]bool, len(current))
	for _, criterion := range current {
		present[criterion.Key] = true
	}
	removed := []models.RemovedCriterion{}
	for _, criterion := range previous {
		if !present[criterion.Key] && !aliased[criterion.Key] {
			removed = append(removed, models.RemovedCriterion{Key: criterion.Key, Title: criterion.Title})
		}
	}
	return removed
}

// applyEdits returns the criteria of a new revision. A value equal to the
// current one is not an edit: it would otherwise pin the field against the
// next version of the instruction for no reason.
func applyEdits(current []models.ScorecardCriterion, edits []models.ScorecardCriterionEdit) ([]models.ScorecardCriterion, error) {
	byKey := make(map[uuid.UUID]int, len(current))
	result := make([]models.ScorecardCriterion, len(current))
	for i, criterion := range current {
		copied := criterion
		copied.EditedFields = append([]string{}, criterion.EditedFields...)
		copied.Warnings = append([]string{}, criterion.Warnings...)
		result[i] = copied
		byKey[criterion.Key] = i
	}
	seen := map[uuid.UUID]bool{}
	for _, edit := range edits {
		i, ok := byKey[edit.Key]
		if !ok || seen[edit.Key] {
			return nil, models.ErrScorecardInvalid
		}
		seen[edit.Key] = true
		criterion := &result[i]
		if edit.Title != nil {
			title := collapse(*edit.Title)
			if title == "" || len([]rune(title)) > 120 {
				return nil, models.ErrScorecardInvalid
			}
			if title != criterion.Title {
				criterion.Title = title
				markEdited(criterion, fieldTitle)
			}
		}
		if edit.Weight != nil {
			if *edit.Weight < 1 || *edit.Weight > 3 {
				return nil, models.ErrScorecardInvalid
			}
			if *edit.Weight != criterion.Weight {
				criterion.Weight = *edit.Weight
				markEdited(criterion, fieldWeight)
			}
		}
		if edit.IsCritical != nil && *edit.IsCritical != criterion.IsCritical {
			criterion.IsCritical = *edit.IsCritical
			markEdited(criterion, fieldIsCritical)
		}
		if edit.Enabled != nil && *edit.Enabled != criterion.Enabled {
			criterion.Enabled = *edit.Enabled
			markEdited(criterion, fieldEnabled)
		}
	}
	titles := map[string]bool{}
	enabled := 0
	for _, criterion := range result {
		normalized := scorecardflow.NormalizeTitle(criterion.Title)
		if titles[normalized] {
			return nil, models.ErrScorecardInvalid
		}
		titles[normalized] = true
		if criterion.Enabled {
			enabled++
		}
	}
	if enabled > scorecardflow.MaxEnabled {
		return nil, models.ErrScorecardLimit
	}
	return result, nil
}

func markEdited(criterion *models.ScorecardCriterion, field string) {
	if !slices.Contains(criterion.EditedFields, field) {
		criterion.EditedFields = append(criterion.EditedFields, field)
	}
}
