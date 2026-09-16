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

// CompanyLifecycle is where a company stands: working, waiting out a freeze, or
// waiting out a soft deletion before it is purged.
type CompanyLifecycle struct {
	CompanyUUID   uuid.UUID
	State         CompanyLifecycleState
	FrozenAt      *time.Time
	SoftDeletedAt *time.Time
	PurgeAfter    *time.Time
	RestoreUsed   bool
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
