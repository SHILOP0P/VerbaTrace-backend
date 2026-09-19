package teamanalytics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrowthTextIsReadable(t *testing.T) {
	cases := []struct {
		name, note, employee, want string
	}{
		{name: "marker names the employee", note: "{{speaker:A}} снова без примеров", employee: "Иван Петров", want: "Иван Петров снова без примеров"},
		{name: "no name yet", note: "{{speaker:A}} снова без примеров", want: "сотрудник снова без примеров"},
		{name: "older note loses its IDs", note: "{{speaker:A}} снова без примеров (u1.2, r3) в s4.1.", employee: "Иван", want: "Иван снова без примеров."},
		{name: "name that looks like an ID stays", note: "{{speaker:B}} ответил", employee: "Отдел S1", want: "Отдел S1 ответил"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, withName(tc.note, tc.employee))
		})
	}
	require.Equal(t, "Отвечает общими словами", readable("Отвечает общими словами (u1.2)"))
}
