package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"verbatrace/monolit/internal/httpserver/middleware"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSearchRequiresAuthAndValidQuery(t *testing.T) {
	handler := NewHandler(&fakeSearchService{})

	recorder := httptest.NewRecorder()
	handler.Search(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/search?q=call", nil))
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=a", nil)
	request = request.WithContext(middleware.ContextWithUserID(request.Context(), uuid.New()))
	recorder = httptest.NewRecorder()
	handler.Search(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestSearchParsesTypesAndLimit(t *testing.T) {
	service := &fakeSearchService{}
	handler := NewHandler(service)
	userID := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=call&types=calls,reports&limit=5", nil)
	request = request.WithContext(middleware.ContextWithUserID(request.Context(), userID))

	recorder := httptest.NewRecorder()
	handler.Search(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, userID, service.lastInput.UserUUID)
	require.Equal(t, "call", service.lastInput.Query)
	require.Equal(t, []models.SearchType{models.SearchTypeCalls, models.SearchTypeReports}, service.lastInput.Types)
	require.Equal(t, 5, service.lastInput.Limit)
}

type fakeSearchService struct {
	lastInput models.SearchInput
}

func (s *fakeSearchService) Search(ctx context.Context, input models.SearchInput) (models.SearchResult, error) {
	s.lastInput = input
	if len([]rune(input.Query)) < 2 {
		return models.SearchResult{}, models.ErrInvalidSearchInput
	}
	return models.SearchResult{}, nil
}
