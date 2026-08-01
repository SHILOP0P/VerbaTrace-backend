package call

import (
	"context"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]models.Call, error) {
	return s.repository.List(ctx, userID)
}

func (s *Service) ListFiltered(ctx context.Context, input models.ListCallsInput) (models.ListCallsResult, error) {
	if input.FolderUUID.Valid {
		if s.callFolderRepository == nil {
			return models.ListCallsResult{}, models.ErrCallFolderNotFound
		}
		if _, err := s.callFolderRepository.GetVisibleByUUID(ctx, input.FolderUUID.UUID, input.UserID); err != nil {
			return models.ListCallsResult{}, err
		}
	}
	return s.repository.ListFiltered(ctx, input)
}

func (s *Service) GetFilterOptions(ctx context.Context, input models.CallFilterOptionsInput) (models.CallFilterOptions, error) {
	return s.repository.GetFilterOptions(ctx, input)
}
