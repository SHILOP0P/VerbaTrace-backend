package qualityreview

import (
	"context"
	"strings"
	"testing"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCommentBody(t *testing.T) {
	value, err := normalizeCommentBody("  точечное замечание  ")
	require.NoError(t, err)
	require.Equal(t, "точечное замечание", value)
	_, err = normalizeCommentBody("   ")
	require.ErrorIs(t, err, ErrInvalidInput)
	_, err = normalizeCommentBody(strings.Repeat("я", 4001))
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestReviewVisibilityWithoutReviewPermission(t *testing.T) {
	for _, status := range []models.QualityReviewStatus{
		models.QualityReviewUnassigned,
		models.QualityReviewAssigned,
		models.QualityReviewPublished,
		models.QualityReviewResolved,
		models.QualityReviewCanceled,
	} {
		require.True(t, isReviewVisibleWithoutReviewPermission(status), status)
	}
	for _, status := range []models.QualityReviewStatus{models.QualityReviewInReview, models.QualityReviewAppealed} {
		require.False(t, isReviewVisibleWithoutReviewPermission(status), status)
	}
}

func TestChallengeAnalysisRejectsShortReasonBeforeDatabaseAccess(t *testing.T) {
	service := NewService(nil)
	_, err := service.ChallengeAnalysis(context.Background(), ChallengeInput{CallUUID: uuid.New(), AnalysisUUID: uuid.New(), ActorUserUUID: uuid.New(), Reason: "коротко"})
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestNormalizeDraftAllowsOptionalComments(t *testing.T) {
	ai := 10.0
	source := []sourceCriterion{{Key: "greeting", Title: "Приветствие", Score: &ai, Min: 0, Max: 10, Weight: 1}}

	draft, score, maxScore, err := normalizeDraft(source, []CriterionInput{{Key: "greeting", HumanScore: &ai}}, false)
	require.NoError(t, err)
	require.Equal(t, models.QualityDecisionConfirmed, draft[0].Decision)
	require.Equal(t, 100.0, *score)
	require.Equal(t, 100.0, *maxScore)

	_, _, _, err = normalizeDraft(source, []CriterionInput{{Key: "greeting", HumanScore: &ai}}, true)
	require.NoError(t, err)

	commented, _, _, err := normalizeDraft(source, []CriterionInput{{Key: "greeting", HumanScore: &ai, Comment: "Отлично выявлена потребность клиента"}}, true)
	require.NoError(t, err)
	require.Equal(t, "Отлично выявлена потребность клиента", commented[0].Comment)
}

func TestNormalizeDraftRejectsUnknownDuplicateAndOutOfRangeCriteria(t *testing.T) {
	source := []sourceCriterion{{Key: "greeting", Title: "Приветствие", Min: 0, Max: 10, Weight: 1}}
	bad := 11.0

	_, _, _, err := normalizeDraft(source, []CriterionInput{{Key: "greeting", HumanScore: &bad}}, false)
	require.ErrorIs(t, err, ErrInvalidInput)

	_, _, _, err = normalizeDraft(source, []CriterionInput{{Key: "unknown"}}, false)
	require.ErrorIs(t, err, ErrInvalidInput)

	_, _, _, err = normalizeDraft(source, []CriterionInput{{Key: "greeting"}, {Key: "greeting"}}, false)
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestNormalizeDraftLeavesUntouchedCriteriaUnscored(t *testing.T) {
	ai := 8.0
	source := []sourceCriterion{
		{Key: "greeting", Title: "Приветствие", Score: &ai, Min: 0, Max: 10, Weight: 1},
		{Key: "needs", Title: "Потребности", Score: &ai, Min: 0, Max: 10, Weight: 1},
	}
	changed := 6.0
	criteria, score, maxScore, err := normalizeDraft(source, []CriterionInput{{Key: "needs", HumanScore: &changed, Comment: "Нужно задать уточняющий вопрос"}}, true)
	require.NoError(t, err)
	require.Equal(t, models.QualityDecisionUnscored, criteria[0].Decision)
	require.Empty(t, criteria[0].Comment)
	require.Equal(t, models.QualityDecisionOverridden, criteria[1].Decision)
	require.Equal(t, 70.0, *score)
	require.Equal(t, 100.0, *maxScore)
}

func TestNormalizeDraftAcceptsCustomCriterion(t *testing.T) {
	source := []sourceCriterion{{Key: "greeting", Title: "Приветствие", Min: 0, Max: 10, Weight: 1}}
	score := 9.0
	criteria, _, _, err := normalizeDraft(source, []CriterionInput{{Key: "custom_accuracy", Title: "Точность формулировок", Custom: true, HumanScore: &score, Comment: "Технические термины использованы корректно"}}, true)
	require.NoError(t, err)
	require.Len(t, criteria, 2)
	require.Equal(t, "custom_accuracy", criteria[1].Key)
	require.Equal(t, "Точность формулировок", criteria[1].Title)
	require.Equal(t, models.QualityDecisionOverridden, criteria[1].Decision)
}

func TestNormalizeDraftAllowsIncompleteCustomCriterionUntilPublication(t *testing.T) {
	source := []sourceCriterion{{Key: "greeting", Title: "Приветствие", Min: 0, Max: 10, Weight: 1}}

	criteria, _, _, err := normalizeDraft(source, []CriterionInput{{Key: "custom_pending", Custom: true}}, false)
	require.NoError(t, err)
	require.Len(t, criteria, 2)
	require.Equal(t, "custom_pending", criteria[1].Key)

	_, _, _, err = normalizeDraft(source, []CriterionInput{{Key: "custom_pending", Custom: true}}, true)
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestParseSourceCriteriaSupportsNormalizedAnalysis(t *testing.T) {
	criteria, err := parseSourceCriteria([]byte(`{"criteria_results":[{"code":"greeting","title":"Приветствие","points_awarded":8,"points_max":10}]}`))
	require.NoError(t, err)
	require.Len(t, criteria, 1)
	require.Equal(t, "greeting", criteria[0].Key)
	require.Equal(t, 8.0, *criteria[0].Score)
	require.Equal(t, 10.0, criteria[0].Max)
}

func TestBuildEffectiveAnalysisUsesLatestPublishedRevisionAndKeepsAllSources(t *testing.T) {
	aiGreeting, aiNeeds := 8.0, 6.0
	humanGreeting, humanNeeds := 9.0, 4.0
	total1, total2 := 75.0, 65.0
	revisions := []models.QualityReviewRevision{
		{HumanScore: &total1, Criteria: []models.QualityReviewCriterion{
			{Key: "greeting", Title: "Приветствие", AIScore: &aiGreeting, HumanScore: &humanGreeting, Decision: models.QualityDecisionOverridden, Weight: 1},
			{Key: "needs", Title: "Потребности", AIScore: &aiNeeds, Decision: models.QualityDecisionUnscored, Weight: 1},
		}},
		{HumanScore: &total2, Criteria: []models.QualityReviewCriterion{
			{Key: "greeting", Title: "Приветствие", AIScore: &aiGreeting, Decision: models.QualityDecisionUnscored, Weight: 1},
			{Key: "needs", Title: "Потребности", AIScore: &aiNeeds, HumanScore: &humanNeeds, Decision: models.QualityDecisionOverridden, Weight: 1},
		}},
	}

	effective, err := buildEffectiveAnalysis([]byte(`{"score":70,"criteria_results":[{"code":"greeting","title":"Приветствие","points_awarded":8,"points_max":10},{"code":"needs","title":"Потребности","points_awarded":6,"points_max":10}]}`), revisions)
	require.NoError(t, err)
	require.Equal(t, "human_review_2", effective.Source)
	require.Equal(t, 65.0, *effective.TotalScore)
	require.Equal(t, 9.0, *effective.Criteria[0].HumanReview1Score)
	require.Nil(t, effective.Criteria[0].HumanReview2Score)
	require.Equal(t, 8.0, *effective.Criteria[0].EffectiveScore)
	require.Equal(t, "ai", effective.Criteria[0].EffectiveSource)
	require.Equal(t, 4.0, *effective.Criteria[1].HumanReview2Score)
	require.Equal(t, 4.0, *effective.Criteria[1].EffectiveScore)
	require.Equal(t, "human_review_2", effective.Criteria[1].EffectiveSource)
}

func TestBuildEffectiveAnalysisExcludesNotApplicableCriterion(t *testing.T) {
	ai := 8.0
	total := 100.0
	effective, err := buildEffectiveAnalysis([]byte(`{"score":80,"criteria_results":[{"code":"greeting","title":"Приветствие","points_awarded":8,"points_max":10}]}`), []models.QualityReviewRevision{{HumanScore: &total, Criteria: []models.QualityReviewCriterion{{Key: "greeting", AIScore: &ai, Decision: models.QualityDecisionNotApplicable}}}})
	require.NoError(t, err)
	require.True(t, effective.Criteria[0].NotApplicable)
	require.Nil(t, effective.Criteria[0].EffectiveScore)
}
