package search

import (
	"context"
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSearchValidationAndTypeFilter(t *testing.T) {
	repo := &fakeSearchRepository{}
	svc := NewService(repo)
	userID := uuid.New()

	_, err := svc.Search(context.Background(), models.SearchInput{UserUUID: userID, Query: "a"})
	require.ErrorIs(t, err, models.ErrInvalidSearchInput)

	_, err = svc.Search(context.Background(), models.SearchInput{UserUUID: userID, Query: "call", Types: []models.SearchType{"crm_clients"}})
	require.ErrorIs(t, err, models.ErrInvalidSearchInput)

	_, err = svc.Search(context.Background(), models.SearchInput{
		UserUUID: userID,
		Query:    " report ",
		Types:    []models.SearchType{models.SearchTypeReports},
		Limit:    7,
	})
	require.NoError(t, err)
	require.Equal(t, "report", repo.lastInput.Query)
	require.Equal(t, []models.SearchType{models.SearchTypeReports}, repo.lastInput.Types)
	require.Equal(t, 7, repo.lastInput.Limit)
}

type fakeSearchRepository struct {
	lastInput models.SearchInput
}

func (r *fakeSearchRepository) Search(ctx context.Context, input models.SearchInput) (models.SearchResult, error) {
	r.lastInput = input
	return models.SearchResult{}, nil
}
