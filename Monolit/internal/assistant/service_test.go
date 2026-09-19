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

func TestReadableChunkCleansOnlyAnalysisChunks(t *testing.T) {
	old := "Сохранённый анализ звонка. Общий вывод: {{speaker:A}} подтвердил бронь (u1.7,u1.4)."
	if got := readableChunk("analysis", old); got != "Сохранённый анализ звонка. Общий вывод: Спикер A подтвердил бронь." {
		t.Fatalf("analysis chunk = %q", got)
	}
	said := "Тариф S1, файл в s4.1 (u1.2)"
	if got := readableChunk("transcription", said); got != said {
		t.Fatalf("a transcript chunk is what was said, got %q", got)
	}
}

func TestAnalysisSearchTextCarriesNoReferenceIDs(t *testing.T) {
	text := analysisSearchText([]byte(`{"schema_version":3,
		"summary":"Бронь оформлена и подтверждена (u1.7,u1.4).","outcome":"{{speaker:B}} подтвердил итог (r16,r17).",
		"items":[
			{"id":"u1.4","title":"Какая дата? (s4.1)","answer_summary":"Назвал дату в s4.1.","explanation":"{{speaker:A}} ответил, см. u1.7.","improvement":null},
			{"id":"u1.7","title":"Подтверждение","explanation":"Частично.","improvement":"Сверить с u1.4 (рекомендация rec1)."},
			{"id":"r1","title":"Тариф S1 назван","criterion_key":"k","explanation":"Назван (r1)."}
		],
		"recommendations":[{"id":"rec1","title":"Назвать цену"}]}`))
	for _, want := range []string{
		"Общий вывод: Бронь оформлена и подтверждена.",
		"Результат: Спикер B подтвердил итог.",
		"Пункт анализа: Какая дата?",
		"Назвал дату.",
		"Спикер A ответил, см. «Подтверждение».",
		"Сверить с «Какая дата?».",
		"Пункт анализа: Тариф S1 назван",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("index text misses %q: %s", want, text)
		}
	}
	for _, id := range []string{"u1.", "r16", "rec1", "s4.1", "(r1)", "{{speaker"} {
		if strings.Contains(text, id) {
			t.Fatalf("index text carries %q: %s", id, text)
		}
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
