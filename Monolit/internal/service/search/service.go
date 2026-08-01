package search

import (
	"context"
	"strings"

	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

const (
	minSearchQueryLength = 2
	maxSearchLimit       = 50
	defaultSearchLimit   = 10
)

type Service struct {
	repository repository.SearchRepository
}

func NewService(repository repository.SearchRepository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Search(ctx context.Context, input models.SearchInput) (models.SearchResult, error) {
	input.Query = strings.TrimSpace(input.Query)
	if input.UserUUID == uuid.Nil || len([]rune(input.Query)) < minSearchQueryLength {
		return models.SearchResult{}, models.ErrInvalidSearchInput
	}
	if input.Limit == 0 {
		input.Limit = defaultSearchLimit
	}
	if input.Limit < 0 || input.Limit > maxSearchLimit {
		return models.SearchResult{}, models.ErrInvalidSearchInput
	}
	for _, searchType := range input.Types {
		if !isValidSearchType(searchType) {
			return models.SearchResult{}, models.ErrInvalidSearchInput
		}
	}

	return s.repository.Search(ctx, input)
}

func isValidSearchType(searchType models.SearchType) bool {
	switch searchType {
	case models.SearchTypeCalls, models.SearchTypeCompanies, models.SearchTypeReports, models.SearchTypeInstructions:
		return true
	default:
		return false
	}
}
