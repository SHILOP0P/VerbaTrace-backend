package delivery

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The mock sender logs the whole letter, so its wording and links can be
// checked, but a reset token never reaches the log.
func TestMockLetterHidesLinkTokens(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		shown string
	}{
		{
			name:  "reset link",
			text:  "Ссылка действует 30 минут: https://app.example/reset-password?token=Ab-c_9%2F. Если вы этого не делали, удалите письмо.",
			shown: "Ссылка действует 30 минут: https://app.example/reset-password?token=[скрыто]. Если вы этого не делали, удалите письмо.",
		},
		{
			name:  "token among other parameters",
			text:  "https://app.example/x?call=1&token=secret&item=r2",
			shown: "https://app.example/x?call=1&token=[скрыто]&item=r2",
		},
		{
			name:  "a letter without secrets stays as it is",
			text:  "Павел приглашает вас в компанию Ромашка. Принять: https://app.example/invitations",
			shown: "Павел приглашает вас в компанию Ромашка. Принять: https://app.example/invitations",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"title": "Письмо", "text": tc.text, "sensitive": true})
			require.NoError(t, err)
			title, text := mockLetter(payload)
			require.Equal(t, "Письмо", title)
			require.Equal(t, tc.shown, text)
		})
	}
}
