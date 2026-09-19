package analysis

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/models"
)

func TestNormalizeUniversalAnalysisCleansReferenceIDs(t *testing.T) {
	raw := `{
		"schema_version":3,"prompt_version":"universal-v3.2",
		"summary":"Бронь оформлена и подтверждена (u1.7,u1.4).",
		"purpose":"Бронирование (s2.1)","outcome":"Клиент подтвердил итог брони (r16,r17).",
		"conversation_types":["Продажа","(u1.4)"],
		"strengths":["Сотрудник представился (u1.2,r1)."],
		"work_on":["Подтвердить номер клиента (рекомендация rec1).","Как в рекомендации rec1, назвать цену."],
		"coverage":{"status":"complete","actual_question_count":2,"analyzed_actual_question_count":2,"limitations":[]},
		"items":[
			{"id":"u1.4","kind":"question","title":"Какая дата? (s4.1)","topic":"Дата (u1.4)","status":"met",
			 "question_parts":["Дата брони (s4.1)"],"answer_summary":"Назвал дату в s4.1.",
			 "explanation":"{{speaker:A}} ответил полностью, см. u1.7.","strengths":["Ясно (u1.4)"],
			 "gaps":[{"text":"Не уточнил время (s4.2)","explanation":"Нужно для u1.7"}],"improvement":null,
			 "evidence":[{"segment_id":"s4.1","quote":"Бронь на завтра (u1.4)","speaker":"A"}]},
			{"id":"u1.7","kind":"question","title":"Подтверждение","status":"partially_met",
			 "explanation":"Частично.","improvement":"Сверить с u1.4.","gaps":[]},
			{"id":"r1","kind":"requirement","title":"Тариф S1 назван","topic":"Скрипт","status":"met",
			 "criterion_key":"8f0c7c2e-3c55-4f5a-9d0e-4e8b7f2a1c10","explanation":"Назван (r1).","gaps":[]}
		],
		"recommendations":[{"id":"rec1","title":"Назвать цену заранее (u1.7)","action":"Назовите цену до брони (u1.7).",
			"reason":"Клиент спросил в сегменте s5.1 дважды.","expected_result":"Меньше вопросов, как в rec1.",
			"item_ids":["u1.7"],"affects_score":true,"importance":2,"impact":2,"repetition":1}],
		"priority_recommendation_ids":[]
	}`
	result, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(raw)})
	require.NoError(t, err)
	require.NotNil(t, result.ResultText)
	require.Equal(t, "Бронь оформлена и подтверждена.", *result.ResultText)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &payload))
	items := payload["items"].([]any)
	question, card, criterion := items[0].(map[string]any), items[1].(map[string]any), items[2].(map[string]any)
	recommendation := payload["recommendations"].([]any)[0].(map[string]any)
	gap := question["gaps"].([]any)[0].(map[string]any)

	cases := []struct {
		name string
		got  any
		want any
	}{
		{name: "summary", got: payload["summary"], want: "Бронь оформлена и подтверждена."},
		{name: "purpose", got: payload["purpose"], want: "Бронирование"},
		{name: "outcome", got: payload["outcome"], want: "Клиент подтвердил итог брони."},
		{name: "a list entry of only IDs goes", got: payload["conversation_types"], want: []any{"Продажа"}},
		{name: "strengths", got: payload["strengths"], want: []any{"Сотрудник представился."}},
		{name: "work on names the recommendation", got: payload["work_on"], want: []any{"Подтвердить номер клиента.", "Как в рекомендации «Назвать цену заранее», назвать цену."}},
		{name: "card title", got: question["title"], want: "Какая дата?"},
		{name: "card topic", got: question["topic"], want: "Дата"},
		{name: "question parts", got: question["question_parts"], want: []any{"Дата брони"}},
		{name: "answer drops the segment and its pointer", got: question["answer_summary"], want: "Назвал дату."},
		{name: "explanation names the card and keeps the marker", got: question["explanation"], want: "{{speaker:A}} ответил полностью, см. «Подтверждение»."},
		{name: "card strengths", got: question["strengths"], want: []any{"Ясно"}},
		{name: "gap text", got: gap["text"], want: "Не уточнил время"},
		{name: "gap explanation", got: gap["explanation"], want: "Нужно для «Подтверждение»"},
		{name: "null stays null", got: question["improvement"], want: nil},
		{name: "improvement", got: card["improvement"], want: "Сверить с «Какая дата?»."},
		{name: "scorecard title is the author's", got: criterion["title"], want: "Тариф S1 назван"},
		{name: "scorecard topic is the author's", got: criterion["topic"], want: "Скрипт"},
		{name: "scorecard explanation is cleaned", got: criterion["explanation"], want: "Назван."},
		{name: "recommendation title", got: recommendation["title"], want: "Назвать цену заранее"},
		{name: "recommendation action", got: recommendation["action"], want: "Назовите цену до брони."},
		{name: "recommendation reason", got: recommendation["reason"], want: "Клиент спросил дважды."},
		{name: "recommendation expected result", got: recommendation["expected_result"], want: "Меньше вопросов, как в «Назвать цену заранее»."},
		// Keys and quotes are data, never prose.
		{name: "item id", got: question["id"], want: "u1.4"},
		{name: "recommendation id", got: recommendation["id"], want: "rec1"},
		{name: "recommendation item ids", got: recommendation["item_ids"], want: []any{"u1.7"}},
		{name: "priority ids", got: payload["priority_recommendation_ids"], want: []any{"rec1"}},
		{name: "criterion key", got: criterion["criterion_key"], want: "8f0c7c2e-3c55-4f5a-9d0e-4e8b7f2a1c10"},
		{name: "evidence quote", got: question["evidence"].([]any)[0].(map[string]any)["quote"], want: "Бронь на завтра (u1.4)"},
		{name: "evidence segment", got: question["evidence"].([]any)[0].(map[string]any)["segment_id"], want: "s4.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.got)
		})
	}
}

func TestNormalizeUniversalAnalysisKeepsTextMadeOnlyOfIDs(t *testing.T) {
	result, err := normalizeAnalysisResult(models.AnalysisResult{ResultJSON: []byte(`{
		"schema_version":3,"prompt_version":"universal-v3.2","summary":"(u1.1)",
		"coverage":{"status":"complete","actual_question_count":1,"analyzed_actual_question_count":1,"limitations":[]},
		"items":[{"id":"u1.1","kind":"question","title":"Вопрос","status":"met","explanation":"(r1)"}],
		"recommendations":[]}`)})
	require.NoError(t, err, "an analysis already paid for must not fail on its wording")
	var payload map[string]any
	require.NoError(t, json.Unmarshal(result.ResultJSON, &payload))
	require.Equal(t, "(u1.1)", payload["summary"])
	require.Equal(t, "(r1)", payload["items"].([]any)[0].(map[string]any)["explanation"])
}
