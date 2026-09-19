package bitrix24

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestCRMSummaryIsReadable(t *testing.T) {
	names := map[string]string{"B": "Клиент Пётр"}
	cases := []struct {
		name    string
		result  string
		outcome string
		work    []string
	}{
		{
			name:    "speakers named",
			result:  `{"schema_version":3,"outcome":"{{speaker:B}} согласился на демонстрацию","work_on":["Назвать срок","Уточнить бюджет","Лишнее"]}`,
			outcome: "Клиент Пётр согласился на демонстрацию",
			work:    []string{"Назвать срок", "Уточнить бюджет"},
		},
		{
			name: "reference IDs go or become titles",
			result: `{"schema_version":3,"outcome":"Клиент подтвердил итог брони (r16,r17).",
				"work_on":["Подтвердить номер клиента (рекомендация rec1).","Как в рекомендации rec1, назвать цену в s4.1.","Третье"],
				"items":[{"id":"r16","title":"Итог"}],"recommendations":[{"id":"rec1","title":"Назвать цену заранее (u1.7)"}]}`,
			outcome: "Клиент подтвердил итог брони.",
			work:    []string{"Подтвердить номер клиента.", "Как в рекомендации «Назвать цену заранее», назвать цену."},
		},
		{
			name:    "unknown speaker and an entry of only IDs",
			result:  `{"outcome":"{{speaker:C}} перезвонит (u1.2)","work_on":["(u1.2, r1)","Уточнить бюджет"]}`,
			outcome: "участник перезвонит",
			work:    []string{"Уточнить бюджет"},
		},
		{name: "no analysis text", result: `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome, work := crmSummary([]byte(tc.result), names)
			require.Equal(t, tc.outcome, outcome)
			require.Equal(t, tc.work, work)
		})
	}
}

func TestCRMSummaryClipsTheOutcomeAfterCleaning(t *testing.T) {
	long := strings.Repeat("слово ", 80) + "(u1.2, r3)"
	outcome, _ := crmSummary([]byte(`{"outcome":"`+long+`"}`), nil)
	require.LessOrEqual(t, utf8.RuneCountInString(outcome), crmOutcomeRunes+1)
	require.True(t, strings.HasSuffix(outcome, "…"))
	require.NotContains(t, outcome, "u1.2")
}
