package scorecard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"verbatrace/monolit/internal/analyzer/scorecardflow"
	"verbatrace/monolit/internal/companystate"
	"verbatrace/monolit/internal/instructioncontent"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	maxCompileAttempts = 3
	compileLease       = 5 * time.Minute
	creditWaitDelay    = 15 * time.Minute
	frozenWaitDelay    = time.Hour
)

// retryDelays space provider failures: a second try a minute later, a third
// after five.
var retryDelays = []time.Duration{time.Minute, 5 * time.Minute}

// CompileDue compiles the scorecards whose time has come and returns how many it
// took. A compile left by a crashed worker is taken again once its lease ends.
func (s *Service) CompileDue(ctx context.Context, limit int) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		UPDATE instruction_scorecards
		SET status = 'compiling', lease_until = now() + make_interval(secs => $2), attempts = attempts + 1, updated_at = now()
		WHERE scorecard_uuid IN (
			SELECT scorecard_uuid FROM instruction_scorecards
			WHERE (status = 'queued' AND compile_after <= now())
			   OR (status = 'compiling' AND lease_until < now())
			ORDER BY compile_after NULLS FIRST
			FOR UPDATE SKIP LOCKED
			LIMIT $1)
		RETURNING scorecard_uuid`, limit, int(compileLease.Seconds()))
	if err != nil {
		return 0, fmt.Errorf("claim scorecard compiles: %w", err)
	}
	var claimed []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		claimed = append(claimed, id)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range claimed {
		if err := s.compile(ctx, id); err != nil {
			s.log.Error(ctx, "scorecard compile failed", zap.String("scorecard_id", id.String()), zap.Error(err))
		}
	}
	return len(claimed), nil
}

func (s *Service) compile(ctx context.Context, cardID uuid.UUID) error {
	card, err := loadCard(ctx, s.db, cardID)
	if err != nil {
		return err
	}
	instruction, err := loadInstruction(ctx, s.db, card.InstructionID)
	if err != nil {
		return err
	}
	if instruction.Deleted {
		_, err := s.db.ExecContext(ctx, `DELETE FROM instruction_scorecards WHERE scorecard_uuid = $1 AND NOT is_current`, card.ID)
		return err
	}
	if instruction.CompanyID.Valid {
		if err := companystate.EnsureActive(ctx, s.db, instruction.CompanyID.UUID); err != nil {
			if errors.Is(err, models.ErrCompanyFrozen) {
				return s.requeue(ctx, card, models.ScorecardErrorCompanyFrozen, "Компания заморожена", frozenWaitDelay, false)
			}
			return err
		}
	}
	version, err := loadVersion(ctx, s.db, card.VersionID)
	if err != nil {
		return err
	}
	previous, err := previousCard(ctx, s.db, card.InstructionID, card.ID, card.CreatedAt)
	hasPrevious := err == nil
	if err != nil && !errors.Is(err, models.ErrScorecardNotFound) {
		return err
	}

	// A version whose text did not change, such as a rename, repeats the
	// previous scorecard and costs nothing. A recompile of the same version is
	// an explicit request to compile again, so it is not copied.
	if hasPrevious && previous.ContentSHA256 == version.ContentSHA256 && previous.VersionID != card.VersionID {
		return s.activate(ctx, card, instruction, copyCriteria(previous.Criteria), models.ScorecardOriginCopied, previous.CompilerVersion, previous.Model)
	}

	text, err := s.versionText(ctx, card.VersionID, version)
	if err != nil {
		return s.fail(ctx, card, instruction, models.ScorecardErrorEmptyText, "Не удалось прочитать текст инструкции", err)
	}
	if strings.TrimSpace(text) == "" {
		return s.fail(ctx, card, instruction, models.ScorecardErrorEmptyText, "В инструкции нет текста", nil)
	}
	input := scorecardflow.Input{
		Instruction:     scorecardflow.InstructionRef{ID: instruction.ID.String(), Title: version.Title, Scope: string(instruction.Scope)},
		InstructionText: text,
	}
	if hasPrevious {
		for _, criterion := range previous.Criteria {
			input.PreviousCriteria = append(input.PreviousCriteria, scorecardflow.PreviousCriterion{Key: criterion.Key.String(), Title: criterion.Title, Requirement: criterion.Requirement})
		}
	}
	result, err := scorecardflow.Compile(ctx, s.executor(card, instruction.owner()), input)
	// Every rejected answer was a paid call; the reasons show what to tune.
	for i, rejection := range result.Rejections {
		s.log.Warn(ctx, "scorecard compile answer rejected", zap.String("scorecard_id", card.ID.String()), zap.Int("attempt", i+1), zap.String("problems", rejection))
	}
	switch {
	case err == nil:
	case errors.Is(err, scorecardflow.ErrInvalidOutput):
		return s.fail(ctx, card, instruction, models.ScorecardErrorInvalidOutput, "Модель трижды вернула некорректные критерии", err)
	case models.IsCreditWaitError(err), errors.Is(err, models.ErrSubscriptionNotFound), errors.Is(err, models.ErrSubscriptionRequired):
		// Like a call that ran out of credits, the compile waits rather than fails.
		return s.requeue(ctx, card, models.ScorecardErrorAwaitingCredits, "Недостаточно кредитов", creditWaitDelay, false)
	case card.Attempts >= maxCompileAttempts:
		return s.fail(ctx, card, instruction, models.ScorecardErrorProvider, "Сервис анализа недоступен", err)
	default:
		return s.requeue(ctx, card, models.ScorecardErrorProvider, "Сервис анализа недоступен, повторим позже", retryDelays[min(card.Attempts-1, len(retryDelays)-1)], true)
	}
	if len(result.Output.Criteria) == 0 {
		return s.fail(ctx, card, instruction, models.ScorecardErrorNoCriteria, result.Output.NoCriteriaReason, nil)
	}
	var before []models.ScorecardCriterion
	if hasPrevious {
		before = previous.Criteria
	}
	model := result.Model
	return s.activate(ctx, card, instruction, assignKeys(result.Output.Criteria, before, uuid.New), models.ScorecardOriginCompiled, scorecardflow.CompilerVersion, &model)
}

// executor meters each compile attempt like an analysis step: the maximum is
// reserved before the call and the actual cost settled after it. The key ties
// the charge to the scorecard, its queue attempt and the try within it.
func (s *Service) executor(card models.Scorecard, owner models.InstructionOwner) scorecardflow.Executor {
	return func(ctx context.Context, key string, task models.AnalysisTask) (models.AnalysisResult, error) {
		var operationID uuid.UUID
		if s.meter != nil {
			var err error
			operationID, err = s.meter.ReserveInstructionCompile(ctx, owner, fmt.Sprintf("scorecard:%s:%d:%s", card.ID, card.Attempts, key), task.System+"\n"+task.Input, int64(task.MaxTokens))
			if err != nil {
				return models.AnalysisResult{}, err
			}
			_, _ = s.db.ExecContext(ctx, `UPDATE instruction_scorecards SET credit_operation_uuid = $2 WHERE scorecard_uuid = $1`, card.ID, operationID)
		}
		stepCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		result, err := s.analyzer.Analyze(stepCtx, models.AnalysisRequest{Task: &task})
		cancel()
		if s.meter != nil {
			switch {
			case result.Usage != nil:
				if settleErr := s.meter.SettleAnalysis(ctx, operationID, result.Usage); settleErr != nil && err == nil {
					err = settleErr
				}
			case err != nil:
				_ = s.meter.MarkCreditOperationReconciling(context.Background(), operationID, "scorecard_provider_error")
			}
		}
		return result, err
	}
}

// versionText is the text of a version, extracted once and kept on the version.
func (s *Service) versionText(ctx context.Context, versionID uuid.UUID, version versionText) (string, error) {
	if version.Text != nil {
		return *version.Text, nil
	}
	if s.storage == nil {
		return "", models.ErrInstructionFileNotFound
	}
	file, err := s.storage.Open(ctx, version.FilePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	name := version.OriginalFilename
	if strings.TrimSpace(name) == "" {
		name = version.FilePath
	}
	text, err := instructioncontent.Extract(name, data)
	if err != nil {
		return "", err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE analysis_instruction_versions SET content_text = $2 WHERE instruction_version_uuid = $1 AND content_text IS NULL`, versionID, text)
	return text, nil
}

// activate stores the criteria and puts the scorecard in force, or leaves it
// waiting when the instruction asks for confirmation. A scorecard of an older
// version that finishes after a newer one never takes its place.
func (s *Service) activate(ctx context.Context, card models.Scorecard, instruction instructionRow, criteria []models.ScorecardCriterion, origin, compilerVersion string, model *string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin scorecard activation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM instruction_scorecard_criteria WHERE scorecard_uuid = $1`, card.ID); err != nil {
		return err
	}
	if err := insertCriteria(ctx, tx, card.ID, criteria); err != nil {
		return err
	}
	var newer bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM instruction_scorecards s
			JOIN analysis_instruction_versions v ON v.instruction_version_uuid = s.instruction_version_uuid
			WHERE s.instruction_uuid = $1 AND s.status = 'ready' AND v.version > $2)`, card.InstructionID, card.InstructionVersion).Scan(&newer); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE instruction_scorecards
		SET status = 'ready', origin = $2, compiler_version = $3, model = $4, error_code = NULL, error_message = NULL,
		    lease_until = NULL, compile_after = NULL, updated_at = now()
		WHERE scorecard_uuid = $1`, card.ID, origin, compilerVersion, model); err != nil {
		return fmt.Errorf("mark scorecard ready: %w", err)
	}
	waiting := false
	switch {
	case newer:
		_, err = tx.ExecContext(ctx, `UPDATE instruction_scorecards SET superseded_at = now() WHERE scorecard_uuid = $1`, card.ID)
	case instruction.ConfirmRequired:
		waiting = true
		if _, err = tx.ExecContext(ctx, `UPDATE instruction_scorecards SET awaiting_confirmation = false WHERE instruction_uuid = $1 AND scorecard_uuid <> $2 AND awaiting_confirmation`, card.InstructionID, card.ID); err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE instruction_scorecards SET awaiting_confirmation = true WHERE scorecard_uuid = $1`, card.ID)
		}
	default:
		if _, err = tx.ExecContext(ctx, `UPDATE instruction_scorecards SET is_current = false, awaiting_confirmation = false, superseded_at = now(), updated_at = now() WHERE instruction_uuid = $1 AND (is_current OR awaiting_confirmation) AND scorecard_uuid <> $2`, card.InstructionID, card.ID); err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE instruction_scorecards SET is_current = true WHERE scorecard_uuid = $1`, card.ID)
		}
	}
	if err != nil {
		return fmt.Errorf("apply compiled scorecard: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit scorecard activation: %w", err)
	}
	if waiting {
		s.notify(ctx, instruction, models.NotificationTypeScorecardReviewNeeded, "Проверьте критерии инструкции",
			instruction.label()+" изменилась. Новые критерии начнут действовать после подтверждения")
	}
	return nil
}

func (s *Service) requeue(ctx context.Context, card models.Scorecard, code, message string, delay time.Duration, countAttempt bool) error {
	attempts := card.Attempts
	if !countAttempt && attempts > 0 {
		attempts--
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE instruction_scorecards
		SET status = 'queued', compile_after = now() + make_interval(secs => $2), attempts = $3, error_code = $4,
		    error_message = $5, lease_until = NULL, updated_at = now()
		WHERE scorecard_uuid = $1`, card.ID, int(delay.Seconds()), attempts, code, message)
	return err
}

func (s *Service) fail(ctx context.Context, card models.Scorecard, instruction instructionRow, code, message string, cause error) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE instruction_scorecards
		SET status = 'failed', error_code = $2, error_message = $3, lease_until = NULL, compile_after = NULL, updated_at = now()
		WHERE scorecard_uuid = $1`, card.ID, code, message); err != nil {
		return err
	}
	if cause != nil {
		s.log.Warn(ctx, "scorecard compile gave up", zap.String("scorecard_id", card.ID.String()), zap.String("code", code), zap.Error(cause))
	}
	s.notify(ctx, instruction, models.NotificationTypeScorecardFailed, "Не удалось подготовить критерии",
		fmt.Sprintf("%s: %s. Откройте вкладку «Критерии оценки» и повторите", instruction.label(), readableReason(message)))
	return nil
}

// label names the instruction in a notification. Two instructions may share a
// title, so the file it came from tells them apart when it says more.
func (i instructionRow) label() string {
	label := fmt.Sprintf("Инструкция «%s»", i.Title)
	file := strings.TrimSpace(i.FileName)
	if file != "" && !strings.EqualFold(file, i.Title+".md") && !strings.EqualFold(file, i.Title) {
		label += fmt.Sprintf(" (файл %s)", file)
	}
	return label
}

// readableReason keeps the model's explanation from leaking the name of its
// input field into a message for people, and drops the final full stop the
// sentence around it adds.
func readableReason(message string) string {
	message = strings.ReplaceAll(message, "instruction_text", "инструкции")
	return strings.TrimRight(strings.TrimSpace(message), ".!;, ")
}

// notify tells the author of the instruction; an author who left the company is
// replaced by the deputy and then by the owner, who are answerable for it.
func (s *Service) notify(ctx context.Context, instruction instructionRow, kind models.NotificationType, title, body string) {
	if s.notifications == nil {
		return
	}
	recipient, err := s.recipient(ctx, instruction)
	if err != nil || recipient == uuid.Nil {
		if err != nil {
			s.log.Warn(ctx, "scorecard notification recipient not found", zap.Error(err))
		}
		return
	}
	entity := "instruction"
	if _, err := s.notifications.Create(ctx, models.CreateNotificationInput{
		UserUUID: recipient, Type: kind, Title: title, Body: body,
		EntityType: &entity, EntityUUID: uuid.NullUUID{UUID: instruction.ID, Valid: true}, CreatedAt: s.now(),
	}); err != nil {
		s.log.Warn(ctx, "scorecard notification failed", zap.Error(err))
	}
}

func (s *Service) recipient(ctx context.Context, instruction instructionRow) (uuid.UUID, error) {
	if !instruction.CompanyID.Valid {
		if instruction.UserID.Valid {
			return instruction.UserID.UUID, nil
		}
		return instruction.CreatedBy.UUID, nil
	}
	var recipient uuid.UUID
	err := s.db.QueryRowContext(ctx, `
		SELECT user_uuid FROM (
			SELECT m.user_uuid, CASE WHEN m.user_uuid = $2 THEN 0 WHEN m.role = 'company_deputy' THEN 1 WHEN m.role = 'company_manager' THEN 2 ELSE 3 END AS rank
			FROM company_members m
			WHERE m.company_uuid = $1 AND m.status = 'active'
			  AND (m.user_uuid = $2 OR m.role IN ('company_deputy','company_manager'))
		) candidates ORDER BY rank LIMIT 1`, instruction.CompanyID.UUID, instruction.CreatedBy.UUID).Scan(&recipient)
	if err != nil {
		return uuid.Nil, err
	}
	return recipient, nil
}
