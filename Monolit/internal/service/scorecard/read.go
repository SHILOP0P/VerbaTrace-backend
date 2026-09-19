package scorecard

import (
	"context"
	"errors"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Get returns the scorecard of the latest version of an instruction, the one
// the instruction page shows. A version that has no scorecard row yet is
// answered with the not_compiled status rather than an error; reading never
// starts a compile, which is paid.
func (s *Service) Get(ctx context.Context, instructionID, userID uuid.UUID) (models.Scorecard, error) {
	instruction, err := s.access.AuthorizeRead(ctx, instructionID, userID)
	if err != nil {
		return models.Scorecard{}, err
	}
	version, err := latestVersionOf(ctx, s.db, instructionID)
	if err != nil {
		return models.Scorecard{}, err
	}
	return s.viewOfVersion(ctx, instruction, version.ID, version.Version)
}

// GetForVersion returns the scorecard of one version, for instance the one an
// old call was scored by.
func (s *Service) GetForVersion(ctx context.Context, instructionID, versionID, userID uuid.UUID) (models.Scorecard, error) {
	instruction, err := s.access.AuthorizeRead(ctx, instructionID, userID)
	if err != nil {
		return models.Scorecard{}, err
	}
	version, err := loadVersion(ctx, s.db, versionID)
	if err != nil {
		return models.Scorecard{}, err
	}
	if version.InstructionID != instructionID {
		return models.Scorecard{}, models.ErrScorecardNotFound
	}
	return s.viewOfVersion(ctx, instruction, versionID, version.Version)
}

func (s *Service) viewOfVersion(ctx context.Context, instruction models.AnalysisInstruction, versionID uuid.UUID, versionNumber int) (models.Scorecard, error) {
	card, err := latestCardOfVersion(ctx, s.db, versionID)
	if errors.Is(err, models.ErrScorecardNotFound) {
		confirm, confirmErr := confirmRequired(ctx, s.db, instruction.ID)
		if confirmErr != nil {
			return models.Scorecard{}, confirmErr
		}
		return models.Scorecard{
			InstructionID: instruction.ID, InstructionTitle: instruction.Title, VersionID: versionID,
			InstructionVersion: versionNumber, Status: models.ScorecardStatusNotCompiled,
			ConfirmRequired: confirm, Criteria: []models.ScorecardCriterion{}, RemovedCriteria: []models.RemovedCriterion{},
		}, nil
	}
	if err != nil {
		return models.Scorecard{}, err
	}
	return s.withRemoved(ctx, card)
}

// withRemoved lists what the scorecard dropped compared with the one before
// it, so the owner sees what changed and can tie a criterion back to its
// predecessor.
func (s *Service) withRemoved(ctx context.Context, card models.Scorecard) (models.Scorecard, error) {
	card.RemovedCriteria = []models.RemovedCriterion{}
	if card.Status != models.ScorecardStatusReady {
		return card, nil
	}
	previous, err := previousCard(ctx, s.db, card.InstructionID, card.ID, card.CreatedAt)
	if errors.Is(err, models.ErrScorecardNotFound) {
		return card, nil
	}
	if err != nil {
		return models.Scorecard{}, err
	}
	// Revisions of one version share the base they were compiled against.
	for previous.VersionID == card.VersionID {
		next, nextErr := previousCard(ctx, s.db, card.InstructionID, previous.ID, previous.CreatedAt)
		if errors.Is(nextErr, models.ErrScorecardNotFound) {
			return card, nil
		}
		if nextErr != nil {
			return models.Scorecard{}, nextErr
		}
		previous = next
	}
	aliased, byAlias, err := aliasedKeys(ctx, s.db, card.InstructionID)
	if err != nil {
		return models.Scorecard{}, err
	}
	card.RemovedCriteria = removedCriteria(card.Criteria, previous.Criteria, aliased)
	for i := range card.Criteria {
		canonical, ok := byAlias[card.Criteria[i].Key]
		if !ok {
			continue
		}
		for _, old := range previous.Criteria {
			if old.Key == canonical {
				card.Criteria[i].SameAs = &models.RemovedCriterion{Key: old.Key, Title: old.Title}
			}
		}
	}
	return card, nil
}

func confirmRequired(ctx context.Context, q queryer, instructionID uuid.UUID) (bool, error) {
	var confirm bool
	err := q.QueryRowContext(ctx, `SELECT scorecard_confirm_required FROM analysis_instructions WHERE instruction_uuid = $1`, instructionID).Scan(&confirm)
	return confirm, err
}
