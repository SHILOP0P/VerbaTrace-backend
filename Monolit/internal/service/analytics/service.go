package analytics

import (
	"context"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"
)

type Service struct {
	analyticsRepository  repository.AnalyticsRepository
	callFolderRepository repository.CallFolderRepository
}

func NewService(analyticsRepository repository.AnalyticsRepository) *Service {
	return &Service{analyticsRepository: analyticsRepository}
}

func (s *Service) SetCallFolderRepository(repository repository.CallFolderRepository) {
	s.callFolderRepository = repository
}

func (s *Service) GetOverview(ctx context.Context, input models.AnalyticsOverviewInput) (models.AnalyticsOverview, error) {
	if input.FolderUUID.Valid {
		if s.callFolderRepository == nil {
			return models.AnalyticsOverview{}, models.ErrCallFolderNotFound
		}
		if _, err := s.callFolderRepository.GetVisibleByUUID(ctx, input.FolderUUID.UUID, input.UserID); err != nil {
			return models.AnalyticsOverview{}, err
		}
	}
	return s.analyticsRepository.GetAnalyticsOverview(ctx, input)
}
