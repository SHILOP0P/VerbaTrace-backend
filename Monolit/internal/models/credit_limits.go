package models

import (
	"time"

	"github.com/google/uuid"
)

// CompanyLifecycleState says whether a company works, waits for its owner to
// pay again, or is on its way out.
type CompanyLifecycleState string

const (
	CompanyLifecycleActive      CompanyLifecycleState = "active"
	CompanyLifecycleFrozen      CompanyLifecycleState = "frozen"
	CompanyLifecycleSoftDeleted CompanyLifecycleState = "soft_deleted"
)

// CompanyFreezeGrace is how long a frozen company waits before it is soft
// deleted, and a soft deleted company before it is purged.
const CompanyFreezeGrace = 30 * 24 * time.Hour

// CompanyFreezeReason says why a company stopped. Both reasons lead to the same
// state, but they are undone differently: a company frozen by a downgrade is
// switched back on, whereas one that was deleted has to have its deletion
// explicitly called off.
type CompanyFreezeReason string

const (
	CompanyFreezeReasonDowngrade CompanyFreezeReason = "downgrade"
	CompanyFreezeReasonDeletion  CompanyFreezeReason = "deletion"
)

// CompanyLifecycle is where a company stands: working, waiting out a freeze, or
// waiting out a soft deletion before it is purged.
type CompanyLifecycle struct {
	CompanyUUID   uuid.UUID
	State         CompanyLifecycleState
	FreezeReason  CompanyFreezeReason
	FrozenAt      *time.Time
	SoftDeletedAt *time.Time
	PurgeAfter    *time.Time
	RestoreUsed   bool
}

// CreditPeriodLength is how long a subscription runs before it renews, and with
// it the window that credit limits are measured over. It is counted from the
// moment the plan was bought rather than from the first of the month, so that
// the cap and the allowance it caps reset together.
const CreditPeriodLength = 30 * 24 * time.Hour

// CreditPeriod is one such window.
type CreditPeriod struct {
	Start time.Time
	End   time.Time
}

// CreditPeriodFor returns the window that contains now. A subscription that has
// not started yet, or none at all, falls back to a window ending now: nothing
// has been spent under it.
func CreditPeriodFor(subscriptionStart time.Time, now time.Time) CreditPeriod {
	now = now.UTC()
	start := subscriptionStart.UTC()
	if start.IsZero() || now.Before(start) {
		return CreditPeriod{Start: now, End: now.Add(CreditPeriodLength)}
	}

	elapsed := now.Sub(start)
	windows := elapsed / CreditPeriodLength
	windowStart := start.Add(windows * CreditPeriodLength)

	return CreditPeriod{Start: windowStart, End: windowStart.Add(CreditPeriodLength)}
}

// CreditLimit is a cap on spending. A nil value means no cap of its own, zero
// forbids spending entirely.
type CreditLimit struct {
	SubjectUUID       uuid.UUID
	CompanyUUID       uuid.UUID
	LimitCredits      *int64
	UpdatedByUserUUID uuid.UUID
	UpdatedAt         time.Time
}

type SetCreditLimitInput struct {
	CompanyUUID    uuid.UUID
	DepartmentUUID uuid.NullUUID
	UserUUID       uuid.UUID
	LimitCredits   *int64
}

// CreditSpending is what a company or a department spent in the current period
// and what that pace projects to by the end of it.
type CreditSpending struct {
	SubjectUUID     uuid.UUID
	SubjectName     string
	LimitCredits    *int64
	UsedCredits     int64
	ForecastCredits int64
	PeriodStart     time.Time
	PeriodEnd       time.Time
}

// CompanyCreditForecast is the picture the owner and the deputy see: the whole
// company plus a line per department. A leader sees only their own department.
type CompanyCreditForecast struct {
	Company     CreditSpending
	Departments []CreditSpending
}
