package models

import (
	"time"

	"github.com/google/uuid"
)

type PlanType string
type PlanCode string
type SubscriptionStatus string
type AnalysisLevel string

const (
	PlanTypePersonal PlanType = "personal"
	PlanTypeBusiness PlanType = "business"
)

const (
	PlanCodePersonalStart PlanCode = "personal_start"
	PlanCodePersonalPlus  PlanCode = "personal_plus"
	PlanCodePersonalPro   PlanCode = "personal_pro"
	PlanCodeBusinessStart PlanCode = "business_start"
	PlanCodeBusinessPlus  PlanCode = "business_plus"
	PlanCodeBusinessPro   PlanCode = "business_pro"
)

const (
	SubscriptionStatusActive   SubscriptionStatus = "active"
	SubscriptionStatusCanceled SubscriptionStatus = "canceled"
	SubscriptionStatusExpired  SubscriptionStatus = "expired"
)

const (
	AnalysisLevelBasic    AnalysisLevel = "basic"
	AnalysisLevelPlus     AnalysisLevel = "plus"
	AnalysisLevelPro      AnalysisLevel = "pro"
	AnalysisLevelPriority AnalysisLevel = "priority"
)

type Plan struct {
	ID                             uuid.UUID
	Code                           PlanCode
	Type                           PlanType
	Name                           string
	MonthlyPriceMinor              int64
	Currency                       string
	MarketingHoursHint             int
	MonthlyMinutesLimit            int
	MonthlyCreditAllowance         int64
	ActiveInstructionLimit         int
	CompanyLimit                   *int
	DepartmentsPerCompanyLimit     *int
	MembersPerCompanyLimit         *int
	InstructionsPerDepartmentLimit *int
	// PendingCreditCallsLimit caps how many calls may wait for credits at once.
	// Nil means no cap, as everywhere else; zero forbids waiting entirely, so an
	// upload is refused the moment the budget runs out.
	PendingCreditCallsLimit *int
	AnalysisLevel           AnalysisLevel
	HistoryRetentionDays    int
	ExportEnabled           bool
	TeamAnalyticsEnabled    bool
	APIAccessEnabled        bool
	WebhooksEnabled         bool
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type Subscription struct {
	ID          uuid.UUID
	Plan        Plan
	UserUUID    uuid.NullUUID
	CompanyUUID uuid.NullUUID
	Status      SubscriptionStatus
	StartsAt    time.Time
	EndsAt      *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UsageCounter struct {
	ID               uuid.UUID
	SubscriptionUUID uuid.UUID
	PeriodStart      time.Time
	PeriodEnd        time.Time
	UsedMinutes      int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type SubscriptionUsage struct {
	Subscription            Subscription
	PeriodStart             time.Time
	PeriodEnd               time.Time
	UsedMinutes             int
	LimitMinutes            int
	RemainingMinutes        int
	Percent                 float64
	RemainingPercent        float64
	DaysUntilReset          int
	ResetsAt                time.Time
	AllowanceExhausted      bool
	WalletCredits           *int64
	MembersLimit            *int
	MembersUsed             *int
	DepartmentsLimit        *int
	DepartmentsUsed         *int
	ActiveInstructionsLimit *int
	ActiveInstructionsUsed  *int
}

type CreditUsage struct {
	AllowanceCredits   int64
	AllowanceRemaining int64
	WalletCredits      int64
	ResetsAt           time.Time
}

type CreditActivityDay struct {
	Date          time.Time
	Credits       int64
	Transcription int64
	Analysis      int64
	Calls         int
}

type CreditWalletEntry struct {
	TransactionUUID uuid.UUID
	Type            string
	Credits         int64
	Reason          string
	CreatedAt       time.Time
}

type SandboxWalletDashboard struct {
	ApplicationUUID uuid.UUID
	ApplicationName string
	BalanceCredits  int64
	Entries         []CreditWalletEntry
}

type CreditDashboard struct {
	AllowanceCredits    int64
	AllowanceRemaining  int64
	RemainingPercent    float64
	DaysUntilReset      int
	ResetsAt            time.Time
	AllowanceExhausted  bool
	WalletCredits       *int64
	Activity            []CreditActivityDay
	WalletEntries       []CreditWalletEntry
	VisibleToMembers    bool
	CanManageVisibility bool
	// CallsAwaitingCredits and PendingCreditCallsLimit answer the question an
	// exhausted limit raises: how many calls are parked and how many more fit.
	// A nil limit means the queue has no cap.
	CallsAwaitingCredits    int
	PendingCreditCallsLimit *int
}

type UpdateCompanyCreditVisibilityInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	Visible     bool
}

type ReserveCreditsInput struct {
	OperationUUID   uuid.UUID
	ApplicationUUID uuid.NullUUID
	CallUUID        uuid.NullUUID
	// CompanyUUID and DepartmentUUID say whose budget this spending belongs to:
	// credits are shared across the owner's companies, so the limits are the
	// only thing keeping one company or department from eating the whole pot.
	CompanyUUID    uuid.NullUUID
	DepartmentUUID uuid.NullUUID
	OperationType  string
	Environment    string
	Provider       string
	Model          string
	Mode           string
	IdempotencyKey string
	MaximumCharge  int64
}

type CreditOperation struct {
	ID                         uuid.UUID
	BillingAccountUUID         uuid.UUID
	Status                     string
	MaximumChargeCredits       int64
	ReservedCredits            int64
	SettledCredits             int64
	InternalPricingLossCredits int64
}

type SettleCreditsInput struct {
	OperationUUID       uuid.UUID
	ActualChargeCredits int64
	ProviderCostNanoUSD int64
	ProviderUsageJSON   []byte
}

type DeveloperApplication struct {
	ID                     uuid.UUID
	OwnerType              string
	UserUUID               uuid.NullUUID
	CompanyUUID            uuid.NullUUID
	BillingAccountUUID     uuid.UUID
	Name                   string
	Environment            string
	Status                 string
	Capabilities           []string
	DailyCreditLimit       *int64
	MonthlyCreditLimit     *int64
	MaxCreditsPerOperation *int64
	LockVersion            int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type CreateDeveloperApplicationInput struct {
	OwnerType              string
	OwnerUUID              uuid.UUID
	CreatedByUserUUID      uuid.UUID
	Name                   string
	Environment            string
	Capabilities           []string
	DailyCreditLimit       *int64
	MonthlyCreditLimit     *int64
	MaxCreditsPerOperation *int64
}

type UpdateDeveloperApplicationInput struct {
	ApplicationUUID        uuid.UUID
	ActorUUID              uuid.UUID
	Name                   string
	Capabilities           []string
	DailyCreditLimit       *int64
	MonthlyCreditLimit     *int64
	MaxCreditsPerOperation *int64
	ExpectedLockVersion    int64
}

type IntegrationAPIKey struct {
	ID                     uuid.UUID  `json:"key_uuid"`
	ServiceAccountID       uuid.UUID  `json:"service_account_uuid"`
	Name                   string     `json:"name"`
	Prefix                 string     `json:"prefix"`
	Scopes                 []string   `json:"scopes"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
	LastUsed               *time.Time `json:"last_used_at,omitempty"`
	RevokedAt              *time.Time `json:"revoked_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	PermanentCreditLimit   *int64     `json:"permanent_credit_limit,omitempty"`
	TemporaryCreditLimit   *int64     `json:"temporary_credit_limit,omitempty"`
	TemporaryLimitStartsAt *time.Time `json:"temporary_limit_starts_at,omitempty"`
	TemporaryLimitEndsAt   *time.Time `json:"temporary_limit_ends_at,omitempty"`
}

type CreateIntegrationAPIKeyInput struct {
	Name                   string
	Scopes                 []string
	ExpiresAt              *time.Time
	PermanentCreditLimit   *int64
	TemporaryCreditLimit   *int64
	TemporaryLimitStartsAt *time.Time
	TemporaryLimitEndsAt   *time.Time
}

type IntegrationPrincipal struct {
	KeyUUID            uuid.UUID
	ApplicationUUID    uuid.UUID
	ConnectionUUID     uuid.UUID
	ServiceAccountUUID uuid.UUID
	BillingAccountUUID uuid.UUID
	Environment        string
	Scopes             []string
}

type UpsertSubscriptionInput struct {
	ID          uuid.UUID
	PlanCode    PlanCode
	UserUUID    uuid.NullUUID
	CompanyUUID uuid.NullUUID
	Status      SubscriptionStatus
	StartsAt    time.Time
	EndsAt      *time.Time
}

type CancelCompanySubscriptionInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
}

type GetCompanySubscriptionInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
}

type GetPersonalSubscriptionUsageInput struct {
	UserUUID    uuid.UUID
	PeriodStart *time.Time
}

type GetCompanySubscriptionUsageInput struct {
	CompanyUUID uuid.UUID
	RequestUser uuid.UUID
	PeriodStart *time.Time
}
