package assistant

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"verbatrace/monolit/internal/models"
)

func TestMessageRequestHashIncludesScopeAndFilters(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	base := models.CreateAssistantMessageInput{UserUUID: uuid.New(), CompanyUUID: uuid.New(), ChatUUID: uuid.New(), Text: " вопрос ", ClientMessageID: "message", IdempotencyKey: "request", ResponseDetail: "auto", FolderIDs: []uuid.UUID{a, b}}
	hash := messageRequestHash(base)
	same := base
	same.FolderIDs = []uuid.UUID{b, a, a}
	same.Text = "вопрос"
	same.IdempotencyKey = "transport retry"
	if hash != messageRequestHash(same) {
		t.Fatal("equivalent request changed hash")
	}
	cases := []models.CreateAssistantMessageInput{}
	changed := base
	changed.FolderIDs = []uuid.UUID{a}
	cases = append(cases, changed)
	changed = base
	changed.CompanyUUID = uuid.New()
	cases = append(cases, changed)
	changed = base
	changed.DepartmentIDs = []uuid.UUID{a}
	cases = append(cases, changed)
	changed = base
	now := time.Now()
	changed.From = &now
	cases = append(cases, changed)
	changed = base
	changed.ResponseDetail = "brief"
	cases = append(cases, changed)
	for i, c := range cases {
		if hash == messageRequestHash(c) {
			t.Fatalf("semantic change %d ignored", i)
		}
	}
	if base.FolderIDs[0] != a {
		t.Fatal("hash mutated request")
	}
}

func TestNarrativeBudgetScalesAndCaps(t *testing.T) {
	small, _ := narrativeBudget("auto", 1, 20)
	large, _ := narrativeBudget("auto", 30, 2000)
	if small >= large || large > 9000 {
		t.Fatalf("invalid scalable limits: %d %d", small, large)
	}
	maximum, _ := narrativeBudget("auto", 100000, 100000)
	if maximum != 9000 {
		t.Fatal(maximum)
	}
	brief, _ := narrativeBudget("brief", 100000, 100000)
	if brief != 1600 {
		t.Fatal(brief)
	}
}

func TestRequestsFullConversationReview(t *testing.T) {
	for _, query := range []string{"Сделай полный анализ звонка", "Разбери весь разговор", "Проанализируй каждый вопрос", "Найди нарушения во всех репликах", "Review every answer"} {
		if !requestsFullConversationReview(query) {
			t.Fatalf("full review intent not detected: %q", query)
		}
	}
	if requestsFullConversationReview("Что Леонид сказал про PostgreSQL?") {
		t.Fatal("targeted question detected as full review")
	}
}

func TestAnalysisSearchTextSupportsUniversalAndLegacyResults(t *testing.T) {
	universal := analysisSearchText([]byte(`{"schema_version":3,"summary":"Интервью","outcome":"Следующий этап","items":[{"title":"Опыт","answer_summary":"Три года","explanation":"Ответ полный","improvement":"Добавить пример"}]}`))
	for _, expected := range []string{"Интервью", "Следующий этап", "Опыт", "Три года", "Добавить пример"} {
		if !strings.Contains(universal, expected) {
			t.Fatalf("universal index text misses %q: %s", expected, universal)
		}
	}
	legacy := analysisSearchText([]byte(`{"summary":"Продажа","criteria_results":[{"title":"Приветствие","explanation":"Выполнено","recommendation":"Не требуется"}]}`))
	if !strings.Contains(legacy, "Приветствие") || !strings.Contains(legacy, "Не требуется") {
		t.Fatalf("legacy index text = %s", legacy)
	}
}

func TestLongSegmentChunkingIsBounded(t *testing.T) {
	text := strings.Repeat("я", 5000)
	chunks := chunkPayload(revisionPayload{Segments: []models.TranscriptionSegment{{Text: text, Speaker: "A"}}})
	var rebuilt strings.Builder
	for _, ch := range chunks {
		if utf8.RuneCountInString(ch.Text) > maxChunkRunes {
			t.Fatal("oversized chunk")
		}
		if ch.Speaker != "A" {
			t.Fatal("speaker lost")
		}
		rebuilt.WriteString(ch.Text)
	}
	if rebuilt.String() != text {
		t.Fatal("text lost during chunking")
	}
}
