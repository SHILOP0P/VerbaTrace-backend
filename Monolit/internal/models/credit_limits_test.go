package models_test

import (
	"testing"
	"time"

	"verbatrace/monolit/internal/models"
)

func TestCreditPeriodFollowsTheSubscriptionRatherThanTheCalendar(t *testing.T) {
	start := time.Date(2026, 1, 20, 9, 30, 0, 0, time.UTC)

	cases := []struct {
		name      string
		now       time.Time
		wantStart time.Time
	}{
		{
			name:      "first day of the plan",
			now:       start.Add(time.Hour),
			wantStart: start,
		},
		{
			name:      "last hour before renewal",
			now:       start.Add(models.CreditPeriodLength - time.Hour),
			wantStart: start,
		},
		{
			// A calendar month would have reset on the first of February; the plan
			// does not, and the limit has to follow the plan.
			name:      "across the turn of the month",
			now:       time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC),
			wantStart: start,
		},
		{
			name:      "second window",
			now:       start.Add(models.CreditPeriodLength + time.Minute),
			wantStart: start.Add(models.CreditPeriodLength),
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			period := models.CreditPeriodFor(start, testCase.now)
			if !period.Start.Equal(testCase.wantStart) {
				t.Fatalf("period start = %s, want %s", period.Start, testCase.wantStart)
			}
			if !period.End.Equal(testCase.wantStart.Add(models.CreditPeriodLength)) {
				t.Fatalf("period end = %s, want %s", period.End, testCase.wantStart.Add(models.CreditPeriodLength))
			}
			if testCase.now.Before(period.Start) || !testCase.now.Before(period.End) {
				t.Fatalf("now %s is outside its own period %s..%s", testCase.now, period.Start, period.End)
			}
		})
	}
}

func TestCreditPeriodWithoutASubscriptionIsEmpty(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	period := models.CreditPeriodFor(time.Time{}, now)
	if !period.Start.Equal(now) {
		t.Fatalf("period start = %s, want %s", period.Start, now)
	}
}

func TestCreditWaitErrorsAreNotFailures(t *testing.T) {
	for _, err := range []error{
		models.ErrInsufficientCredits,
		models.ErrCompanyCreditLimitExceeded,
		models.ErrDepartmentCreditLimitExceeded,
	} {
		if !models.IsCreditWaitError(err) {
			t.Fatalf("%v should park a call rather than fail it", err)
		}
	}

	if models.IsCreditWaitError(models.ErrAudioFileUnreadable) {
		t.Fatal("a broken file is a real failure and must not wait for credits")
	}
}
