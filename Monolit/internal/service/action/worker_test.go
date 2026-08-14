package action

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReminderForSelectsOnlyCurrentThreshold(t *testing.T) {
	tests := []struct {
		name      string
		remaining time.Duration
		want      string
	}{
		{"seven days", 6 * 24 * time.Hour, "action_reminder_7d"},
		{"five days", 4 * 24 * time.Hour, "action_reminder_5d"},
		{"two days", 36 * time.Hour, "action_reminder_2d"},
		{"one day", 12 * time.Hour, "action_reminder_1d"},
		{"grace started", -time.Hour, "action_grace_started"},
		{"grace twelve hours", -13 * time.Hour, "action_grace_12h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, title := reminderFor(tt.remaining)
			require.Equal(t, tt.want, got)
			require.NotEmpty(t, title)
		})
	}
}
