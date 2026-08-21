package analysis

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"verbatrace/monolit/internal/models"
)

func (s *Service) ListAppliedInstructions(ctx context.Context, analysisID, userID uuid.UUID) ([]models.AppliedInstruction, error) {
	repo, ok := s.analysisRepository.(instructionSnapshotRepository)
	if !ok {
		return nil, models.ErrAnalysisInstructionNotFound
	}
	callID, err := repo.InstructionSnapshotCallUUID(ctx, analysisID)
	if err != nil {
		return nil, err
	}
	if _, err = s.callRepository.GetByUUID(ctx, callID, userID); err != nil {
		return nil, fmt.Errorf("authorize instruction snapshots: %w", err)
	}
	items, err := repo.ListInstructionSnapshots(ctx, analysisID)
	if err != nil {
		return nil, err
	}
	return items, nil
}
func (s *Service) GetAppliedInstruction(ctx context.Context, analysisID, versionID, userID uuid.UUID) (models.AppliedInstruction, error) {
	repo, ok := s.analysisRepository.(instructionSnapshotRepository)
	if !ok {
		return models.AppliedInstruction{}, models.ErrAnalysisInstructionNotFound
	}
	item, err := repo.GetInstructionSnapshot(ctx, analysisID, versionID)
	if err != nil {
		return item, err
	}
	if _, err = s.callRepository.GetByUUID(ctx, item.CallUUID, userID); err != nil {
		return models.AppliedInstruction{}, fmt.Errorf("authorize instruction snapshot: %w", err)
	}
	return item, nil
}
