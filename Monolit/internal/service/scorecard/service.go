// Package scorecard keeps the scorecards of analysis instructions: it compiles
// each instruction version into criteria once, lets the owner adjust and apply
// them, and hands the criteria in force to every analysis.
package scorecard

import (
	"context"
	"database/sql"
	"time"

	"verbatrace/monolit/internal/analyzer"
	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/storage"

	"github.com/google/uuid"
)

// InstructionAccess answers who may read and edit an instruction; a scorecard
// follows its instruction.
type InstructionAccess interface {
	AuthorizeRead(ctx context.Context, instructionID uuid.UUID, userID uuid.UUID) (models.AnalysisInstruction, error)
	AuthorizeEdit(ctx context.Context, instructionID uuid.UUID, userID uuid.UUID) (models.AnalysisInstruction, error)
}

type CreditMeter interface {
	ReserveInstructionCompile(ctx context.Context, owner models.InstructionOwner, key string, input string, maxOutputTokens int64) (uuid.UUID, error)
	SettleAnalysis(ctx context.Context, operationID uuid.UUID, usage *models.ProviderUsage) error
	MarkCreditOperationReconciling(ctx context.Context, operationID uuid.UUID, reason string) error
}

type NotificationSender interface {
	Create(ctx context.Context, input models.CreateNotificationInput) (models.Notification, error)
}

const (
	// A recompile on request is limited per instruction: each one is a paid call.
	recompileCooldown = time.Minute
	// An analysis waits this long for a scorecard that is still being compiled
	// before it falls back to breaking the instruction down itself.
	defaultPlanWait = 90 * time.Second
	defaultPlanPoll = 2 * time.Second
)

type Service struct {
	db            *sql.DB
	access        InstructionAccess
	analyzer      analyzer.Analyzer
	storage       storage.InstructionStorage
	meter         CreditMeter
	notifications NotificationSender
	log           logger.Logger
	now           func() time.Time
	planWait      time.Duration
	planPoll      time.Duration
	// onAlias lets analytics re-key what it already stored, in the same
	// transaction that records the alias.
	onAlias func(ctx context.Context, tx *sql.Tx, aliasKey, canonicalKey uuid.UUID) error
}

// SetAliasHook runs inside the transaction that ties a new criterion key to an
// older one.
func (s *Service) SetAliasHook(hook func(ctx context.Context, tx *sql.Tx, aliasKey, canonicalKey uuid.UUID) error) {
	s.onAlias = hook
}

func NewService(db *sql.DB, access InstructionAccess, provider analyzer.Analyzer, instructionStorage storage.InstructionStorage, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{
		db: db, access: access, analyzer: provider, storage: instructionStorage, log: log,
		now: func() time.Time { return time.Now().UTC() }, planWait: defaultPlanWait, planPoll: defaultPlanPoll,
	}
}

func (s *Service) SetCreditMeter(meter CreditMeter)                 { s.meter = meter }
func (s *Service) SetNotificationService(sender NotificationSender) { s.notifications = sender }
