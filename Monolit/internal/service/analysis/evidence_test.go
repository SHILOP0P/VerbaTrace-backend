package analysis

import (
	"encoding/json"
	"testing"

	"verbatrace/monolit/internal/models"
)

func TestEnrichEvidenceMatchesUniqueQuote(t *testing.T) {
	start, end := 1.2, 2.4
	result, err := enrichEvidence([]byte(`{"criteria_results":[{"quote":"Да, созвонимся во вторник"}]}`), []models.TranscriptionWord{
		{Text: "Да,", StartSeconds: start, EndSeconds: 1.4, Speaker: "Менеджер"},
		{Text: "созвонимся", StartSeconds: 1.5, EndSeconds: 1.9, Speaker: "Менеджер"},
		{Text: "во", StartSeconds: 2.0, EndSeconds: 2.1, Speaker: "Менеджер"},
		{Text: "вторник", StartSeconds: 2.2, EndSeconds: end, Speaker: "Менеджер"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	evidence := payload["criteria_results"].([]any)[0].(map[string]any)["evidence"].([]any)[0].(map[string]any)
	if evidence["match_status"] != evidenceMatched || evidence["start_seconds"] != start || evidence["end_seconds"] != end {
		t.Fatalf("evidence = %+v", evidence)
	}
}

func TestMatchEvidenceDoesNotChooseAmbiguousQuote(t *testing.T) {
	result := matchEvidence("готово", []models.TranscriptionWord{
		{Text: "готово", StartSeconds: 1, EndSeconds: 1.5},
		{Text: "и", StartSeconds: 2, EndSeconds: 2.1},
		{Text: "готово", StartSeconds: 3, EndSeconds: 3.5},
	})
	if result.MatchStatus != evidenceAmbiguous || result.StartSeconds != nil || result.WordStartIndex != nil {
		t.Fatalf("result = %+v", result)
	}
}
