package analysistext

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripReferenceIDs(t *testing.T) {
	titles := map[string]string{
		"u1.3": "Опыт работы",
		"rec2": "Назвать цену заранее",
		"r1":   "Представиться",
	}
	cases := []struct {
		name   string
		text   string
		titles map[string]string
		want   string
	}{
		// Texts users saw.
		{name: "card list", text: "Бронь оформлена и подтверждена (u1.7,u1.4,u1.9,u1.12).", want: "Бронь оформлена и подтверждена."},
		{name: "cards and requirements", text: "Сотрудник представился (u1.2,r1,r2,r3).", want: "Сотрудник представился."},
		{name: "labelled recommendation in brackets", text: "Подтвердить номер клиента (рекомендация rec1).", want: "Подтвердить номер клиента."},
		{name: "recommendations with spaces", text: "Включать вопрос об удобстве (rec3, rec4).", want: "Включать вопрос об удобстве."},
		{name: "requirement list", text: "Клиент подтвердил итог брони (r16,r17).", want: "Клиент подтвердил итог брони."},

		// Lists.
		{name: "square brackets and semicolons", text: "Итог ясен [r1; r2 / r3].", want: "Итог ясен."},
		{name: "и as separator", text: "Оба ответа полные (u1.1 и u1.2).", want: "Оба ответа полные."},
		{name: "см. label", text: "Цена названа (см. u2.4).", want: "Цена названа."},
		{name: "label before every entry", text: "Шаги ясны (карточка u1.1, критерий r2, пункт u1.3).", want: "Шаги ясны."},
		{name: "segment list", text: "Клиент уточнил срок (s4.1, s4.2).", want: "Клиент уточнил срок."},
		{name: "upper case", text: "Бронь подтверждена (U1.7, REC2, S3.1).", want: "Бронь подтверждена."},
		{name: "list at the start keeps the rest", text: "(u1.2) Приветствие прозвучало.", want: "Приветствие прозвучало."},
		{name: "list of titled cards still goes", text: "Опыт описан (u1.3).", titles: titles, want: "Опыт описан."},

		// Single IDs.
		{name: "bare card with title", text: "Ответ на u1.3 был полным.", titles: titles, want: "Ответ на «Опыт работы» был полным."},
		{name: "label kept with title", text: "Как сказано в рекомендации rec2, цену стоит назвать.", titles: titles, want: "Как сказано в рекомендации «Назвать цену заранее», цену стоит назвать."},
		{name: "capitalised label kept", text: "Рекомендация rec2 важнее всего.", titles: titles, want: "Рекомендация «Назвать цену заранее» важнее всего."},
		{name: "title found by lower case", text: "Требование R1 выполнено.", titles: titles, want: "Требование «Представиться» выполнено."},
		{name: "unknown ID goes with its label", text: "Это важно, см. u9.9 ниже.", titles: titles, want: "Это важно, ниже."},
		{name: "bare ID without titles", text: "Итог разговора u1.2", want: "Итог разговора"},
		{name: "ID in an enumeration", text: "Сильные стороны: вежливость, u1.2, пунктуальность.", want: "Сильные стороны: вежливость, пунктуальность."},
		{name: "ID mixed with words in brackets", text: "Смотри (u1, выше).", want: "Смотри (выше)."},
		{name: "list with a titled word keeps the word", text: "Смотри (выше, u1).", want: "Смотри (выше)."},

		// Segment IDs take their pointer words.
		{name: "segment after preposition", text: "Все элементы присутствуют в s3.1.", want: "Все элементы присутствуют."},
		{name: "segment after noun", text: "Цитата в сегменте s2.1 подтверждает ответ.", want: "Цитата подтверждает ответ."},
		{name: "segment after во and noun", text: "Это видно во фрагменте S5 и дальше.", want: "Это видно и дальше."},
		{name: "segment after реплика", text: "Клиент согласился по реплике s7.2, без возражений.", want: "Клиент согласился, без возражений."},

		// Schema field names.
		{
			name: "field names become words",
			text: "Ирина представилась, что соответствует части assigned_unit. Все требуемые элементы assigned_unit присутствуют в s3.1.",
			want: "Ирина представилась, что соответствует части пункта. Все требуемые элементы пункта присутствуют.",
		},
		{name: "assigned_units alone", text: "Все assigned_units разобраны.", want: "Все пункты разобраны."},
		{name: "assigned_unit alone", text: "Этот assigned_unit закрыт.", want: "Этот пункт закрыт."},
		{name: "source_segments", text: "В source_segments нет ответа.", want: "В реплики нет ответа."},
		{name: "other snake_case key goes", text: "Поле question_parts пустое, а evidence_quote_2 есть.", want: "Поле пустое, а есть."},
		{name: "snake_case key at sentence end", text: "Статус взят из information_status.", want: "Статус взят из."},

		// Stays untouched.
		{name: "version number", text: "Версия 2.1 вышла.", want: "Версия 2.1 вышла."},
		{name: "product name", text: "Тариф Pro24 дороже.", want: "Тариф Pro24 дороже."},
		{name: "ID glued to letters", text: "Ключ u2f подключён.", want: "Ключ u2f подключён."},
		{name: "R&D", text: "Отдел R&D ответил.", want: "Отдел R&D ответил."},
		{name: "time", text: "Звонок в 14:02.", want: "Звонок в 14:02."},
		{name: "ID after a dot", text: "Раздел 2.u1 описан.", want: "Раздел 2.u1 описан."},
		{name: "plain brackets", text: "Ответ (без деталей) дан.", want: "Ответ (без деталей) дан."},
		{name: "e-mail and file name", text: "Почта ivan_petrov@mail.ru и файл report_final.pdf", want: "Почта ivan_petrov@mail.ru и файл report_final.pdf"},
		{name: "path", text: "Файл лежит в /tmp/call_notes/summary_v2", want: "Файл лежит в /tmp/call_notes/summary_v2"},
		{name: "capitalised snake case", text: "Константа MAX_SIZE задана.", want: "Константа MAX_SIZE задана."},
		{name: "empty", text: "", want: ""},

		// Speaker markers stay for the reader's side to name.
		{name: "marker kept, IDs go", text: "{{speaker:A}} ответил полностью (u1.2).", want: "{{speaker:A}} ответил полностью."},
		{name: "marker with snake_case key", text: "{{speaker:speaker_0}} назвал цену.", want: "{{speaker:speaker_0}} назвал цену."},
		{name: "marker with an ID-like key", text: "{{speaker:s1}} согласился.", want: "{{speaker:s1}} согласился."},
		{name: "ID right after a marker", text: "Ответил {{speaker:B}} в s2.1.", want: "Ответил {{speaker:B}}."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, StripReferenceIDs(tc.text, tc.titles))
		})
	}
}

func TestStripReferenceIDsIsIdempotent(t *testing.T) {
	titles := map[string]string{"u1.3": "Опыт работы"}
	for _, text := range []string{
		"Ответ на u1.3 был полным (u1.4, r2).",
		"Все требуемые элементы assigned_unit присутствуют в s3.1.",
	} {
		once := StripReferenceIDs(text, titles)
		require.Equal(t, once, StripReferenceIDs(once, titles))
	}
}

func TestResultTitles(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   map[string]string
	}{
		{
			name: "cards and recommendations",
			result: `{"items":[{"id":"u1.1","title":"Какая дата? (s4.1)"},{"id":"r1","title":"Тариф S1 назван","criterion_key":"k"},{"id":"u1.2","title":""}],
				"recommendations":[{"id":"rec1","title":"Назвать цену (u1.1)"}]}`,
			want: map[string]string{"u1.1": "Какая дата?", "r1": "Тариф S1 назван", "rec1": "Назвать цену"},
		},
		{name: "legacy result", result: `{"summary":"Итог","criteria_results":[]}`, want: map[string]string{}},
		{name: "broken field keeps the rest", result: `{"items":[{"id":1,"title":"Без ключа"},{"id":"u1.2","title":"Опыт"}]}`, want: map[string]string{"u1.2": "Опыт"}},
		{name: "not JSON", result: `{`, want: map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResultTitles([]byte(tc.result)))
		})
	}
}

func TestResolveSpeakerMarkers(t *testing.T) {
	names := map[string]string{"A": "Анна", "B": "  "}
	cases := []struct {
		name  string
		text  string
		names map[string]string
		want  string
	}{
		{name: "known name", text: "{{speaker:A}} назвала цену.", names: names, want: "Анна назвала цену."},
		{name: "blank name falls back", text: "{{speaker:B}} согласился.", names: names, want: "Спикер B согласился."},
		{name: "unknown key", text: "{{speaker:C}} молчал.", names: names, want: "Спикер C молчал."},
		{name: "key is trimmed", text: "{{speaker: A }} ответила.", names: names, want: "Анна ответила."},
		{name: "no names at all", text: "{{speaker:A}} и {{speaker:B}}", want: "Спикер A и Спикер B"},
		{name: "marker case", text: "{{Speaker:A}} ответила.", names: names, want: "Анна ответила."},
		{name: "empty key", text: "{{speaker:}} ответил.", want: "Спикер ответил."},
		{name: "no markers", text: "Текст без маркеров.", names: names, want: "Текст без маркеров."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResolveSpeakerMarkers(tc.text, tc.names))
		})
	}
}

func TestResolveSpeakerMarkersFunc(t *testing.T) {
	got := ResolveSpeakerMarkersFunc("{{speaker:A}} и {{speaker:speaker_1}}", func(key string) string { return "<" + key + ">" })
	require.Equal(t, "<A> и <speaker_1>", got)
}
