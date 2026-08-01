package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func TestCreateDeepAnalysisReturnsExistingWithoutSpendingLimit(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	input := models.CreateDeepAnalysisInput{
		UserID: userID, Scope: models.AggregateAnalysisScopePersonal, PeriodFrom: day(2026, 7, 1), PeriodTo: dayEnd(2026, 7, 7),
	}
	source := models.AggregateAnalysisSourceCall{CallUUID: uuid.New(), Title: "Call"}
	request := buildAggregateAnalysisRequest(input, []models.AggregateAnalysisSourceCall{source}, 1)
	existing := models.AggregateAnalysis{
		ID: uuid.New(), Scope: models.AggregateAnalysisScopePersonal, Status: models.AggregateAnalysisStatusDone,
		SourceCallsCount: 1, ResultJSON: aggregateSourceSummaryResult(t, request.Dataset.SourceSummary),
	}
	repo := &analyticsRepoStub{reusable: &existing, sourceTotal: 1, sources: []models.AggregateAnalysisSourceCall{source}}
	svc := NewService(repo)
	svc.SetAnalyzer(&aggregateAnalyzerStub{})

	got, err := svc.CreateDeepAnalysis(ctx, input)
	if err != nil {
		t.Fatalf("create deep analysis: %v", err)
	}
	if got.ID != existing.ID {
		t.Fatalf("returned analysis = %s, want %s", got.ID, existing.ID)
	}
	if repo.spent {
		t.Fatal("limit was spent for reusable analysis")
	}
}

func TestCreateDeepAnalysisDoesNotReuseStaleSourceSet(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	input := models.CreateDeepAnalysisInput{
		UserID: userID, Scope: models.AggregateAnalysisScopePersonal, PeriodFrom: day(2026, 7, 1), PeriodTo: dayEnd(2026, 7, 7),
	}
	oldSource := models.AggregateAnalysisSourceCall{CallUUID: uuid.New(), Title: "Old call"}
	newSource := models.AggregateAnalysisSourceCall{CallUUID: uuid.New(), Title: "New call"}
	oldRequest := buildAggregateAnalysisRequest(input, []models.AggregateAnalysisSourceCall{oldSource}, 1)
	existing := models.AggregateAnalysis{
		ID: uuid.New(), Scope: models.AggregateAnalysisScopePersonal, Status: models.AggregateAnalysisStatusDone,
		SourceCallsCount: 1, ResultJSON: aggregateSourceSummaryResult(t, oldRequest.Dataset.SourceSummary),
	}
	repo := &analyticsRepoStub{reusable: &existing, sourceTotal: 1, sources: []models.AggregateAnalysisSourceCall{newSource}}
	svc := NewService(repo)
	svc.SetAnalyzer(&aggregateAnalyzerStub{result: result("Новый анализ")})

	got, err := svc.CreateDeepAnalysis(ctx, input)
	if err != nil {
		t.Fatalf("create deep analysis: %v", err)
	}
	if got.ID == existing.ID {
		t.Fatal("stale analysis was reused")
	}
	if !repo.spent {
		t.Fatal("limit was not spent for fresh analysis")
	}
	waitFor(t, repo.lifecycleReached)
}

func TestCreateDeepAnalysisForceSpendsLimitAndMarksDone(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := &analyticsRepoStub{sourceTotal: 1, sources: []models.AggregateAnalysisSourceCall{{CallUUID: uuid.New(), Title: "Call"}}}
	svc := NewService(repo)
	svc.SetAnalyzer(&aggregateAnalyzerStub{result: result("Готово")})

	got, err := svc.CreateDeepAnalysis(ctx, models.CreateDeepAnalysisInput{
		UserID: userID, Scope: models.AggregateAnalysisScopePersonal, PeriodFrom: day(2026, 7, 1), PeriodTo: dayEnd(2026, 7, 7), Force: true,
	})
	if err != nil {
		t.Fatalf("create deep analysis: %v", err)
	}
	if !repo.spent {
		t.Fatal("limit was not spent")
	}
	if got.Status != models.AggregateAnalysisStatusPending {
		t.Fatalf("status = %s", got.Status)
	}
	waitFor(t, repo.lifecycleReached)
}

func TestCreateDeepAnalysisAggregatesAllSourcesBeyondOldHundredCap(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	sources := make([]models.AggregateAnalysisSourceCall, 0, 150)
	for i := 0; i < 150; i++ {
		score := float64(40 + i%45)
		sources = append(sources, models.AggregateAnalysisSourceCall{
			CallUUID:  uuid.New(),
			CreatedAt: day(2026, 7, 1).Add(time.Duration(i) * time.Minute),
			Title:     "Call",
			Score:     &score,
			Summary:   "Клиент сомневался из-за цены.",
			IssueCodes: []any{
				"unclear_pricing",
			},
			CriteriaResults: []any{map[string]any{
				"code": "pricing_clarity", "title": "Ясность цены", "status": "partially_met",
				"points_awarded": float64(5), "points_max": float64(10),
			}},
			CustomerObjections: []any{"Цена высокая"},
			Risks:              []any{"Клиент может уйти к конкуренту"},
			Topics:             []any{"Цена"},
			NextStepQuality:    map[string]any{"has_next_step": i%2 == 0, "specific": i%3 == 0},
		})
	}
	repo := &analyticsRepoStub{sourceTotal: len(sources), sources: sources}
	analyzer := &aggregateAnalyzerStub{result: result("Готово")}
	svc := NewService(repo)
	svc.SetAnalyzer(analyzer)

	_, err := svc.CreateDeepAnalysis(ctx, models.CreateDeepAnalysisInput{
		UserID: userID, Scope: models.AggregateAnalysisScopePersonal, PeriodFrom: day(2026, 7, 1), PeriodTo: dayEnd(2026, 7, 7), Force: true,
	})
	if err != nil {
		t.Fatalf("create deep analysis: %v", err)
	}
	waitFor(t, repo.lifecycleReached)
	waitFor(t, analyzer.called)

	request := analyzer.capturedRequest()
	if request.SourceCallsCount != 150 || request.Metrics.AggregatedCalls != 150 || request.Dataset.SourceSummary.IncludedInStatistics != 150 {
		t.Fatalf("source counts = %#v / %#v", request.Metrics, request.Dataset.SourceSummary)
	}
	if !request.Dataset.SourceSummary.AllAnalyzedCallsUsed {
		t.Fatalf("all analyzed calls flag = false: %#v", request.Dataset.SourceSummary)
	}
	if len(request.Sources) != aggregateRepresentativeCallLimit {
		t.Fatalf("representative calls = %d, want %d", len(request.Sources), aggregateRepresentativeCallLimit)
	}
	if len(request.Dataset.IssueCoverage) == 0 || request.Dataset.IssueCoverage[0].Code != "unclear_pricing" || request.Dataset.IssueCoverage[0].Count != 150 {
		t.Fatalf("issue coverage = %#v", request.Dataset.IssueCoverage)
	}
	if len(request.Dataset.WeakCriteria) == 0 || request.Dataset.WeakCriteria[0].Code != "pricing_clarity" || request.Dataset.WeakCriteria[0].WeakCalls != 150 {
		t.Fatalf("weak criteria = %#v", request.Dataset.WeakCriteria)
	}

	payload := repo.donePayload(t)
	sourceSummary, ok := payload["source_summary"].(map[string]any)
	if !ok {
		t.Fatalf("source_summary missing: %#v", payload)
	}
	if sourceSummary["included_in_statistics"] != float64(150) || sourceSummary["all_analyzed_calls_used"] != true {
		t.Fatalf("source_summary = %#v", sourceSummary)
	}
	if _, ok := payload["aggregate_statistics"].(map[string]any); !ok {
		t.Fatalf("aggregate_statistics missing: %#v", payload)
	}
}

func TestEnrichAggregateAnalysisResultMovesSingleIssueOutOfRecurring(t *testing.T) {
	callID := uuid.New()
	request := buildAggregateAnalysisRequest(models.CreateDeepAnalysisInput{
		UserID: uuid.New(), Scope: models.AggregateAnalysisScopePersonal, PeriodFrom: day(2026, 7, 1), PeriodTo: dayEnd(2026, 7, 1),
	}, []models.AggregateAnalysisSourceCall{{
		CallUUID: callID, CreatedAt: day(2026, 7, 1), Title: "Call", IssueCodes: []any{"single_issue"},
	}}, 1)
	raw, _ := json.Marshal(map[string]any{
		"summary": "Готово",
		"recurring_issues": []any{map[string]any{
			"code": "single_issue", "title": "Единичная проблема", "count": float64(1), "recommendation": "Проверить.",
		}},
	})
	result := enrichAggregateAnalysisResult(models.AnalysisResult{ResultJSON: raw}, request)

	var payload map[string]any
	if err := json.Unmarshal(result.ResultJSON, &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if recurring := payload["recurring_issues"].([]any); len(recurring) != 0 {
		t.Fatalf("recurring issues = %#v", recurring)
	}
	if singles := payload["single_call_observations"].([]any); len(singles) == 0 {
		t.Fatalf("single observations missing: %#v", payload)
	}
}

func TestCreateDeepAnalysisMapsLimitNoCallsAndProviderError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	input := models.CreateDeepAnalysisInput{UserID: userID, Scope: models.AggregateAnalysisScopePersonal, PeriodFrom: day(2026, 7, 1), PeriodTo: dayEnd(2026, 7, 7), Force: true}

	noCallsRepo := &analyticsRepoStub{}
	noCallsSvc := NewService(noCallsRepo)
	noCallsSvc.SetAnalyzer(&aggregateAnalyzerStub{})
	_, err := noCallsSvc.CreateDeepAnalysis(ctx, input)
	if !errors.Is(err, models.ErrNoAnalyzedCallsForDeepAnalysis) {
		t.Fatalf("no calls err = %v", err)
	}

	limitRepo := &analyticsRepoStub{sourceTotal: 1, sources: []models.AggregateAnalysisSourceCall{{CallUUID: uuid.New()}}, spendErr: models.ErrDeepAnalysisLimitExceeded}
	limitSvc := NewService(limitRepo)
	limitSvc.SetAnalyzer(&aggregateAnalyzerStub{})
	_, err = limitSvc.CreateDeepAnalysis(ctx, input)
	if !errors.Is(err, models.ErrDeepAnalysisLimitExceeded) {
		t.Fatalf("limit err = %v", err)
	}

	providerRepo := &analyticsRepoStub{sourceTotal: 1, sources: []models.AggregateAnalysisSourceCall{{CallUUID: uuid.New()}}}
	providerSvc := NewService(providerRepo)
	providerSvc.SetAnalyzer(&aggregateAnalyzerStub{err: errors.New("provider failed")})
	created, err := providerSvc.CreateDeepAnalysis(ctx, input)
	if err != nil {
		t.Fatalf("provider create err = %v", err)
	}
	if created.Status != models.AggregateAnalysisStatusPending {
		t.Fatalf("provider create status = %s", created.Status)
	}
	waitFor(t, providerRepo.failed)
}

type analyticsRepoStub struct {
	mu               sync.Mutex
	reusable         *models.AggregateAnalysis
	sourceTotal      int
	sources          []models.AggregateAnalysisSourceCall
	spendErr         error
	spent            bool
	markedProcessing bool
	markedDone       bool
	markedFailed     bool
	created          models.AggregateAnalysis
}

func (r *analyticsRepoStub) GetAnalyticsOverview(context.Context, models.AnalyticsOverviewInput) (models.AnalyticsOverview, error) {
	panic("not implemented")
}

func (r *analyticsRepoStub) CreateAggregateAnalysis(_ context.Context, analysis models.AggregateAnalysis) (models.AggregateAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = analysis
	return analysis, nil
}

func (r *analyticsRepoStub) GetAggregateAnalysisByUUID(context.Context, uuid.UUID) (models.AggregateAnalysis, error) {
	panic("not implemented")
}

func (r *analyticsRepoStub) FindReusableAggregateAnalysis(context.Context, models.CreateDeepAnalysisInput) (models.AggregateAnalysis, error) {
	if r.reusable != nil {
		return *r.reusable, nil
	}
	return models.AggregateAnalysis{}, models.ErrAggregateAnalysisNotFound
}

func (r *analyticsRepoStub) ListAggregateAnalyses(context.Context, models.ListDeepAnalysesInput) (models.ListAggregateAnalysesResult, error) {
	panic("not implemented")
}

func (r *analyticsRepoStub) MarkAggregateAnalysisProcessing(_ context.Context, id uuid.UUID) (models.AggregateAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markedProcessing = true
	r.created.ID = id
	r.created.Status = models.AggregateAnalysisStatusProcessing
	return r.created, nil
}

func (r *analyticsRepoStub) MarkAggregateAnalysisDone(_ context.Context, id uuid.UUID, result models.AnalysisResult, sourceCallsCount int) (models.AggregateAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markedDone = true
	r.created.ID = id
	r.created.Status = models.AggregateAnalysisStatusDone
	r.created.ResultJSON = result.ResultJSON
	r.created.SourceCallsCount = sourceCallsCount
	return r.created, nil
}

func (r *analyticsRepoStub) MarkAggregateAnalysisFailed(_ context.Context, id uuid.UUID, message string) (models.AggregateAnalysis, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markedFailed = true
	r.created.ID = id
	r.created.Status = models.AggregateAnalysisStatusFailed
	r.created.ErrorMessage = &message
	return r.created, nil
}

func (r *analyticsRepoStub) ListAggregateAnalysisSourceCalls(context.Context, models.AnalyticsOverviewInput) ([]models.AggregateAnalysisSourceCall, int, error) {
	return r.sources, r.sourceTotal, nil
}

func (r *analyticsRepoStub) SpendDeepAnalysisUsage(context.Context, models.DeepAnalysisSubjectType, uuid.UUID, time.Time, time.Time) error {
	if r.spendErr != nil {
		return r.spendErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spent = true
	return nil
}

func (r *analyticsRepoStub) lifecycleReached() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.markedProcessing && r.markedDone
}

func (r *analyticsRepoStub) donePayload(t *testing.T) map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var payload map[string]any
	if err := json.Unmarshal(r.created.ResultJSON, &payload); err != nil {
		t.Fatalf("decode done payload: %v", err)
	}
	return payload
}

func (r *analyticsRepoStub) failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.markedFailed
}

type aggregateAnalyzerStub struct {
	mu        sync.Mutex
	result    models.AnalysisResult
	err       error
	request   models.AggregateAnalysisRequest
	wasCalled bool
}

func (a *aggregateAnalyzerStub) Provider() string { return "test" }

func (a *aggregateAnalyzerStub) Analyze(context.Context, models.AnalysisRequest) (models.AnalysisResult, error) {
	panic("not implemented")
}

func (a *aggregateAnalyzerStub) AnalyzeAggregate(_ context.Context, request models.AggregateAnalysisRequest) (models.AnalysisResult, error) {
	a.mu.Lock()
	a.wasCalled = true
	a.request = request
	a.mu.Unlock()
	if a.err != nil {
		return models.AnalysisResult{}, a.err
	}
	if len(a.result.ResultJSON) == 0 {
		return result("Готово"), nil
	}
	return a.result, nil
}

func (a *aggregateAnalyzerStub) called() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wasCalled
}

func (a *aggregateAnalyzerStub) capturedRequest() models.AggregateAnalysisRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.request
}

func result(summary string) models.AnalysisResult {
	raw, _ := json.Marshal(map[string]any{"summary": summary})
	text := summary
	return models.AnalysisResult{ResultJSON: raw, ResultText: &text}
}

func aggregateSourceSummaryResult(t *testing.T, summary models.AggregateAnalysisSourceSummary) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"summary": "Готово", "source_summary": summary})
	if err != nil {
		t.Fatalf("marshal source summary: %v", err)
	}
	return raw
}

func day(year int, month time.Month, date int) time.Time {
	return time.Date(year, month, date, 0, 0, 0, 0, time.UTC)
}

func dayEnd(year int, month time.Month, date int) time.Time {
	return day(year, month, date).AddDate(0, 0, 1).Add(-time.Nanosecond)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
