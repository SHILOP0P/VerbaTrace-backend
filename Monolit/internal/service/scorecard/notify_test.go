package scorecard

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Two instructions may share a title; the notification names the file so the
// owner can tell which one failed.
func TestInstructionLabelNamesTheFileWhenItSaysMore(t *testing.T) {
	cases := []struct {
		name  string
		row   instructionRow
		label string
	}{
		{"uploaded file", instructionRow{Title: "ексель", FileName: "Спринт_2_Вопросы.pdf"}, "Инструкция «ексель» (файл Спринт_2_Вопросы.pdf)"},
		{"written on the site", instructionRow{Title: "Стандарт продаж", FileName: "Стандарт продаж.md"}, "Инструкция «Стандарт продаж»"},
		{"no file", instructionRow{Title: "Стандарт"}, "Инструкция «Стандарт»"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.label, tc.row.label())
		})
	}
}

func TestReadableReasonHidesFieldNamesAndTheFinalStop(t *testing.T) {
	require.Equal(t, "В тексте инструкции нет проверяемых требований",
		readableReason("В тексте instruction_text нет проверяемых требований. "))
	require.Equal(t, "Модель трижды вернула некорректные критерии", readableReason("Модель трижды вернула некорректные критерии"))
}
