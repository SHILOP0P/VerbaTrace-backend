package analysisflow_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/analyzer/analysisflow"
	"verbatrace/monolit/internal/analyzer/openrouter"
	"verbatrace/monolit/internal/models"
)

// Every step names itself, so a provider schema name and a deterministic
// analyzer can tell the steps apart without parsing prompts.
func TestEveryStepCarriesItsKindAsTaskName(t *testing.T) {
	provider, _ := openrouter.New("test", "test")
	var mu sync.Mutex
	names := map[string]map[string]bool{}
	execute := fixtureExecutor(t, false, false)
	runner := analysisflow.Runner{
		Segments: []analysisflow.Segment{{ID: "s0", Speaker: "A", Text: "Вопрос 0?"}},
		Schema:   provider.AnalysisSchema(),
		Execute: func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
			stage := strings.SplitN(key, "/", 2)[0]
			if strings.Contains(key, "/audit") {
				stage += "/audit"
			}
			mu.Lock()
			if names[stage] == nil {
				names[stage] = map[string]bool{}
			}
			names[stage][task.Name] = true
			mu.Unlock()
			return execute(ctx, key, task)
		},
	}
	_, err := runner.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]map[string]bool{
		"inventory":       {analysisflow.StepInventory: true},
		"inventory/audit": {analysisflow.StepInventoryAudit: true},
		"assess":          {analysisflow.StepAssessment: true},
		"assess/audit":    {analysisflow.StepAssessmentAudit: true},
		"summary":         {analysisflow.StepSummary: true},
	}, names)
}
