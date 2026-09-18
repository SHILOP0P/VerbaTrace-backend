//go:build integration

package teamanalytics

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func (tm *team) notApplicable(call, key uuid.UUID) {
	tm.exec(`UPDATE analytics_criterion_facts SET ai_status = 'not_applicable', ai_score = NULL WHERE call_uuid = $1 AND criterion_key = $2`, call, key)
}

func progressOf(t *testing.T, progress CallProgress, key uuid.UUID) ProgressCriterion {
	t.Helper()
	for _, row := range progress.Criteria {
		if row.CriterionKey == key.String() {
			return row
		}
	}
	t.Fatalf("criterion %s is not in the progress", key)
	return ProgressCriterion{}
}

func TestWorkOnMistakesFollowsTheEmployeesChain(t *testing.T) {
	tm := newTeam(t, "business_plus")
	ctx := context.Background()
	// Ivan's budget question, oldest first: 25, n/a, 25, a shared call, 100, 100, 100.
	first := tm.call(10, tm.sales, 50, 25, tm.ivan)
	skipped := tm.call(9, tm.sales, 50, 25, tm.ivan)
	tm.notApplicable(skipped, tm.budget)
	repeated := tm.call(8, tm.sales, 50, 25, tm.ivan)
	shared := tm.call(7, tm.sales, 50, 0, tm.ivan, tm.olga)
	fixed := tm.call(6, tm.sales, 80, 100, tm.ivan)
	tm.call(5, tm.sales, 80, 100, tm.ivan)
	tm.call(4, tm.sales, 80, 100, tm.ivan)

	progress, err := tm.service.CallProgress(ctx, tm.ivan, first)
	require.NoError(t, err)
	require.True(t, progress.Available)
	require.Equal(t, VerdictFirstTime, progressOf(t, progress, tm.budget).Verdict)
	require.Equal(t, tm.ivan.String(), progress.Employee.UserUUID)

	progress, err = tm.service.CallProgress(ctx, tm.ivan, repeated)
	require.NoError(t, err)
	budget := progressOf(t, progress, tm.budget)
	require.Equal(t, VerdictRepeated, budget.Verdict)
	require.Equal(t, first.String(), budget.Previous.CallUUID, "n/a does not break the chain")
	require.Equal(t, 2, budget.RepeatStreak)
	require.True(t, budget.Previous.CanOpen)
	require.Equal(t, VerdictHolding, progressOf(t, progress, tm.nextStep).Verdict)
	require.Equal(t, 1, progress.Counts.Repeated)
	require.Equal(t, 1, progress.Counts.Holding)

	progress, err = tm.service.CallProgress(ctx, tm.ivan, fixed)
	require.NoError(t, err)
	budget = progressOf(t, progress, tm.budget)
	require.Equal(t, VerdictFixed, budget.Verdict)
	require.Equal(t, repeated.String(), budget.Previous.CallUUID, "a shared call is not a link")
	require.Zero(t, budget.RepeatStreak)

	progress, err = tm.service.CallProgress(ctx, tm.owner, shared)
	require.NoError(t, err)
	require.False(t, progress.Available)
	require.Equal(t, ProgressSharedCall, *progress.UnavailableReason)

	// Three passing calls in a row close the mistake.
	own, err := tm.service.EmployeeProgress(ctx, tm.req(tm.ivan), uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, own.Open)
	require.Len(t, own.ClosedInPeriod, 1)
	require.Equal(t, tm.budget.String(), own.ClosedInPeriod[0].CriterionKey)
	require.Equal(t, "Выяснил бюджет", own.ClosedInPeriod[0].Title)

	// A new failure opens it again.
	last := tm.call(3, tm.sales, 40, 0, tm.ivan)
	own, err = tm.service.EmployeeProgress(ctx, tm.req(tm.ivan), uuid.Nil)
	require.NoError(t, err)
	require.Len(t, own.Open, 1)
	require.Equal(t, 0, own.Open[0].LastScore)
	require.Equal(t, 1, own.Open[0].RepeatStreak)
	require.Equal(t, last.String(), own.Open[0].LastCallUUID)
	require.Empty(t, own.ClosedInPeriod, "reopened is not closed")
	progress, err = tm.service.CallProgress(ctx, tm.ivan, last)
	require.NoError(t, err)
	require.Equal(t, VerdictNew, progressOf(t, progress, tm.budget).Verdict)

	// The leader of the department and the owner see it; a colleague does not.
	_, err = tm.service.CallProgress(ctx, tm.leader, last)
	require.NoError(t, err)
	byOwner, err := tm.service.EmployeeProgress(ctx, tm.req(tm.owner), tm.ivan)
	require.NoError(t, err)
	require.Len(t, byOwner.Open, 1)
	_, err = tm.service.CallProgress(ctx, tm.petr, last)
	require.ErrorIs(t, err, ErrNotFound, "a call the viewer does not see")
	_, err = tm.service.EmployeeProgress(ctx, tm.req(tm.olga), tm.ivan)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestWorkOnMistakesOfOthersFollowsThePlan(t *testing.T) {
	tm := newTeam(t, "business_start")
	ctx := context.Background()
	call := tm.call(1, tm.sales, 70, 50, tm.ivan)
	_, err := tm.service.CallProgress(ctx, tm.owner, call)
	require.ErrorIs(t, err, ErrTeamAnalyticsDenied)
	progress, err := tm.service.CallProgress(ctx, tm.ivan, call)
	require.NoError(t, err, "an employee sees their own progress on any plan")
	require.True(t, progress.Available)
}
