package analytics

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/models"
)

type fakeAnalyticsRepository struct {
	departments map[uuid.UUID]uuid.UUID
}

func (r *fakeAnalyticsRepository) GetAnalyticsOverview(context.Context, models.AnalyticsOverviewInput) (models.AnalyticsOverview, error) {
	risks := 3
	return models.AnalyticsOverview{
		CallsTotal:      10,
		AverageScore:    ptr(71.0),
		CriteriaSummary: []models.AnalyticsCriterionSummary{{Code: "budget"}},
		TopWeakCriteria: []models.AnalyticsWeakCriterion{{Code: "budget"}},
		TopTopics:       []models.AnalyticsTopicCount{{Title: "Цена", Count: 2}},
		RisksCount:      &risks,
	}, nil
}

func (r *fakeAnalyticsRepository) DepartmentCompany(_ context.Context, id uuid.UUID) (uuid.UUID, error) {
	return r.departments[id], nil
}

type fakeGate struct {
	allowed map[uuid.UUID]bool
	fail    error
}

func (g fakeGate) CanAccessTeamAnalytics(_ context.Context, companyID uuid.UUID) error {
	if g.fail != nil {
		return g.fail
	}
	if g.allowed[companyID] {
		return nil
	}
	return models.ErrTeamAnalyticsAccessDenied
}

func ptr[T any](value T) *T { return &value }

func TestOverviewKeepsTheScoreButHidesBreakdownsWithoutTeamAnalytics(t *testing.T) {
	paid, basic, department := uuid.New(), uuid.New(), uuid.New()
	repository := &fakeAnalyticsRepository{departments: map[uuid.UUID]uuid.UUID{department: basic}}
	service := NewService(repository)
	service.SetTeamAnalyticsGate(fakeGate{allowed: map[uuid.UUID]bool{paid: true}})
	ctx := context.Background()

	full, err := service.GetOverview(ctx, models.AnalyticsOverviewInput{UserID: uuid.New(), CompanyUUID: uuid.NullUUID{UUID: paid, Valid: true}})
	require.NoError(t, err)
	require.True(t, full.TeamAnalyticsEnabled)
	require.Len(t, full.CriteriaSummary, 1)

	for name, input := range map[string]models.AnalyticsOverviewInput{
		"company":    {CompanyUUID: uuid.NullUUID{UUID: basic, Valid: true}},
		"department": {DepartmentUUID: uuid.NullUUID{UUID: department, Valid: true}},
	} {
		t.Run(name, func(t *testing.T) {
			limited, err := service.GetOverview(ctx, input)
			require.NoError(t, err)
			require.False(t, limited.TeamAnalyticsEnabled)
			require.Equal(t, 10, limited.CallsTotal)
			require.Equal(t, 71.0, *limited.AverageScore)
			require.Empty(t, limited.CriteriaSummary)
			require.Empty(t, limited.TopWeakCriteria)
			require.Empty(t, limited.TopTopics)
			require.Nil(t, limited.RisksCount)
		})
	}

	personal, err := service.GetOverview(ctx, models.AnalyticsOverviewInput{VisibilityScope: models.CallVisibilityScopePersonal})
	require.NoError(t, err)
	require.True(t, personal.TeamAnalyticsEnabled)
	require.Len(t, personal.CriteriaSummary, 1)
}

func TestOverviewTreatsMissingSubscriptionAsNoTeamAnalyticsAndPropagatesOtherErrors(t *testing.T) {
	company := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	service := NewService(&fakeAnalyticsRepository{})

	service.SetTeamAnalyticsGate(fakeGate{fail: models.ErrSubscriptionRequired})
	overview, err := service.GetOverview(context.Background(), models.AnalyticsOverviewInput{CompanyUUID: company})
	require.NoError(t, err)
	require.False(t, overview.TeamAnalyticsEnabled)

	boom := errors.New("database is down")
	service.SetTeamAnalyticsGate(fakeGate{fail: boom})
	_, err = service.GetOverview(context.Background(), models.AnalyticsOverviewInput{CompanyUUID: company})
	require.ErrorIs(t, err, boom)
}
