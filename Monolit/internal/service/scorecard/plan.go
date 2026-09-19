package scorecard

import (
	"context"
	"errors"
	"sort"
	"time"

	"verbatrace/monolit/internal/analyzer/scorecardflow"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type planSlot struct {
	instruction models.AnalysisInstructionContent
	row         instructionRow
	card        *models.Scorecard
	waitVersion uuid.UUID
}

// PlanForAnalysis picks the scorecard for each instruction a call is analysed
// with. An instruction whose version has no scorecard yet gets it compiled now,
// and the analysis waits a bounded time for it; failing that the instruction is
// broken down by the model as before, so an analysis never fails because of a
// scorecard. With confirmation switched on, the instruction is analysed with
// the version its applied scorecard belongs to, text and criteria alike.
func (s *Service) PlanForAnalysis(ctx context.Context, instructions []models.AnalysisInstructionContent) (models.ScorecardPlan, error) {
	plan := models.ScorecardPlan{Instructions: []models.AnalysisInstructionContent{}, Requirements: []models.AnalysisRequirement{}, Adhoc: []models.AnalysisInstructionContent{}, Scorecards: []models.AppliedScorecard{}, Mode: models.ScorecardModeNone}
	if len(instructions) == 0 {
		return plan, nil
	}
	slots := make([]planSlot, len(instructions))
	var pending []int
	for i, content := range instructions {
		row, err := loadInstruction(ctx, s.db, content.ID)
		if err != nil {
			return plan, err
		}
		slots[i] = planSlot{instruction: content, row: row}
		switch {
		case row.ConfirmRequired:
			card, err := currentCard(ctx, s.db, content.ID)
			if errors.Is(err, models.ErrScorecardNotFound) {
				continue
			}
			if err != nil {
				return plan, err
			}
			if card.VersionID != content.VersionID {
				if slots[i].instruction, err = s.contentOfVersion(ctx, content, card.VersionID); err != nil {
					return plan, err
				}
			}
			slots[i].card = &card
		case content.VersionID == uuid.Nil:
			// The version of a legacy row could not be resolved; the scorecard in
			// force is used only if it was compiled from the very text read.
			card, err := currentCard(ctx, s.db, content.ID)
			if err == nil && card.ContentSHA256 == content.ContentSHA256 {
				slots[i].card = &card
			} else if err != nil && !errors.Is(err, models.ErrScorecardNotFound) {
				return plan, err
			}
		default:
			card, err := latestCardOfVersion(ctx, s.db, content.VersionID)
			switch {
			case err == nil && card.Status == models.ScorecardStatusReady:
				slots[i].card = &card
			case err == nil && card.Status == models.ScorecardStatusFailed:
			case err == nil || errors.Is(err, models.ErrScorecardNotFound):
				if err := ensureCompile(ctx, s.db, content.ID, content.VersionID); err != nil {
					return plan, err
				}
				slots[i].waitVersion = content.VersionID
				pending = append(pending, i)
			default:
				return plan, err
			}
		}
	}
	if err := s.waitForCompiles(ctx, slots, pending); err != nil {
		return plan, err
	}
	return buildPlan(slots), nil
}

func (s *Service) waitForCompiles(ctx context.Context, slots []planSlot, pending []int) error {
	deadline := time.Now().Add(s.planWait)
	for len(pending) > 0 {
		var still []int
		for _, i := range pending {
			card, err := latestCardOfVersion(ctx, s.db, slots[i].waitVersion)
			switch {
			case err == nil && card.Status == models.ScorecardStatusReady:
				slots[i].card = &card
			case err == nil && card.Status == models.ScorecardStatusFailed:
			case err != nil && !errors.Is(err, models.ErrScorecardNotFound):
				return err
			default:
				still = append(still, i)
			}
		}
		pending = still
		if len(pending) == 0 || !time.Now().Before(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.planPoll):
		}
	}
	return nil
}

func (s *Service) contentOfVersion(ctx context.Context, content models.AnalysisInstructionContent, versionID uuid.UUID) (models.AnalysisInstructionContent, error) {
	version, err := loadVersion(ctx, s.db, versionID)
	if err != nil {
		return content, err
	}
	text, err := s.versionText(ctx, versionID, version)
	if err != nil {
		return content, err
	}
	content.Title = version.Title
	content.Content = text
	content.ContentSHA256 = version.ContentSHA256
	content.VersionID = versionID
	return content, nil
}

type planCandidate struct {
	requirement models.AnalysisRequirement
	rank        int
	instruction int
	position    int
}

// scopeRank orders instructions from the broadest to the narrowest scope. When
// two instructions ask for the same thing, the narrower one is closer to the
// people it applies to, so its weight and criticality win.
func scopeRank(scope models.AnalysisInstructionScope) int {
	switch scope {
	case models.AnalysisInstructionScopeDepartment:
		return 3
	case models.AnalysisInstructionScopeCompany:
		return 2
	default:
		return 1
	}
}

func buildPlan(slots []planSlot) models.ScorecardPlan {
	plan := models.ScorecardPlan{Instructions: []models.AnalysisInstructionContent{}, Requirements: []models.AnalysisRequirement{}, Adhoc: []models.AnalysisInstructionContent{}, Scorecards: []models.AppliedScorecard{}}
	var candidates []planCandidate
	for i, slot := range slots {
		content := slot.instruction
		if slot.card == nil {
			plan.Instructions = append(plan.Instructions, content)
			plan.Adhoc = append(plan.Adhoc, content)
			continue
		}
		card := slot.card
		content.ScorecardID = card.ID
		plan.Instructions = append(plan.Instructions, content)
		plan.Scorecards = append(plan.Scorecards, models.AppliedScorecard{ScorecardID: card.ID, InstructionID: card.InstructionID, VersionID: card.VersionID, Revision: card.Revision})
		for _, criterion := range card.Criteria {
			if !criterion.Enabled {
				continue
			}
			candidates = append(candidates, planCandidate{
				requirement: models.AnalysisRequirement{
					CriterionKey: criterion.Key, AlsoCriterionKeys: []uuid.UUID{}, ScorecardID: card.ID,
					InstructionID: content.ID, InstructionTitle: content.Title,
					Title: criterion.Title, Requirement: criterion.Requirement, Applicability: criterion.Applicability,
					Depth: criterion.Depth, RequiredQuestion: criterion.RequiredQuestion, CrossCutting: criterion.CrossCutting,
					Weight: criterion.Weight, IsCritical: criterion.IsCritical,
				},
				rank: scopeRank(slot.row.Scope), instruction: i, position: criterion.Position,
			})
		}
	}
	candidates = mergeDuplicates(candidates)
	kept := candidates
	if len(candidates) > scorecardflow.MaxEnabled {
		// The narrowest scopes are kept first; within them the instruction order.
		sort.SliceStable(kept, func(a, b int) bool {
			if kept[a].rank != kept[b].rank {
				return kept[a].rank > kept[b].rank
			}
			if kept[a].instruction != kept[b].instruction {
				return kept[a].instruction < kept[b].instruction
			}
			return kept[a].position < kept[b].position
		})
		kept = kept[:scorecardflow.MaxEnabled]
		plan.LimitApplied = true
	}
	sort.SliceStable(kept, func(a, b int) bool {
		if kept[a].instruction != kept[b].instruction {
			return kept[a].instruction < kept[b].instruction
		}
		return kept[a].position < kept[b].position
	})
	for _, candidate := range kept {
		plan.Requirements = append(plan.Requirements, candidate.requirement)
	}
	switch {
	case len(slots) == 0:
		plan.Mode = models.ScorecardModeNone
	case len(plan.Adhoc) == 0:
		plan.Mode = models.ScorecardModeFixed
	case len(plan.Scorecards) == 0:
		plan.Mode = models.ScorecardModeAdhoc
	default:
		plan.Mode = models.ScorecardModePartial
	}
	return plan
}

// mergeDuplicates scores a requirement once when two instructions ask for it
// word for word. The narrower instruction's criterion stays; the other keys
// ride along on it, so each instruction's own history still gets the result.
// Criteria of one instruction are never merged: its scorecard already keeps
// titles apart. Anything short of an exact match is left alone.
func mergeDuplicates(candidates []planCandidate) []planCandidate {
	type key struct{ title, requirement string }
	firstByKey := map[key]int{}
	var result []planCandidate
	for _, candidate := range candidates {
		k := key{scorecardflow.NormalizeTitle(candidate.requirement.Title), scorecardflow.NormalizeTitle(candidate.requirement.Requirement)}
		index, seen := firstByKey[k]
		if !seen || result[index].instruction == candidate.instruction {
			firstByKey[k] = len(result)
			result = append(result, candidate)
			continue
		}
		existing := &result[index]
		if candidate.rank > existing.rank {
			candidate.requirement.AlsoCriterionKeys = append(append([]uuid.UUID{}, existing.requirement.AlsoCriterionKeys...), existing.requirement.CriterionKey)
			*existing = candidate
			continue
		}
		existing.requirement.AlsoCriterionKeys = append(existing.requirement.AlsoCriterionKeys, candidate.requirement.CriterionKey)
	}
	return result
}
