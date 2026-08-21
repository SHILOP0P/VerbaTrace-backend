package analysis_instruction

import (
	"context"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) ListVersions(ctx context.Context, id uuid.UUID, userID uuid.UUID) ([]models.AnalysisInstructionVersion, error) {
	instruction, err := s.repository.GetByUUIDIncludingInactive(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = s.authorizeRead(ctx, instruction, userID); err != nil {
		return nil, err
	}
	return s.repository.ListVersions(ctx, id)
}

func (s *Service) GetVersionFile(ctx context.Context, id uuid.UUID, versionID uuid.UUID, userID uuid.UUID) (models.File, error) {
	instruction, err := s.repository.GetByUUIDIncludingInactive(ctx, id)
	if err != nil {
		return models.File{}, err
	}
	if err = s.authorizeRead(ctx, instruction, userID); err != nil {
		return models.File{}, err
	}
	version, err := s.repository.GetVersion(ctx, id, versionID)
	if err != nil {
		return models.File{}, err
	}
	content, err := s.instructionStorage.Open(ctx, version.FilePath)
	if err != nil {
		return models.File{}, fmt.Errorf("open instruction version: %w", err)
	}
	return models.File{Content: content, Path: version.FilePath, OriginalFilename: version.OriginalFilename, MimeType: version.MimeType, SizeBytes: version.SizeBytes}, nil
}
