package scorecard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// Edit changes what an owner may change by hand. Criteria edits never touch the
// scorecard in place: they make a new revision, so calls already scored keep
// the revision they were scored by. The confirmation switch lives on the
// instruction; turning it off applies a scorecard that was waiting for it.
func (s *Service) Edit(ctx context.Context, input models.EditScorecardInput) (models.Scorecard, error) {
	instruction, err := s.access.AuthorizeEdit(ctx, input.InstructionID, input.UserID)
	if err != nil {
		return models.Scorecard{}, err
	}
	if len(input.Criteria) == 0 && input.ConfirmRequired == nil {
		return models.Scorecard{}, models.ErrScorecardInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("begin scorecard edit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if len(input.Criteria) > 0 {
		card, err := lockLatestCard(ctx, tx, instruction.ID)
		if err != nil {
			return models.Scorecard{}, err
		}
		if card.Status != models.ScorecardStatusReady {
			return models.Scorecard{}, models.ErrScorecardNotReady
		}
		if card.LockVersion != input.LockVersion {
			return models.Scorecard{}, models.ErrScorecardVersionConflict
		}
		criteria, err := applyEdits(card.Criteria, input.Criteria)
		if err != nil {
			return models.Scorecard{}, err
		}
		if _, err := s.newRevision(ctx, tx, card, criteria, input.UserID); err != nil {
			return models.Scorecard{}, err
		}
	}
	if input.ConfirmRequired != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE analysis_instructions SET scorecard_confirm_required = $2 WHERE instruction_uuid = $1`, instruction.ID, *input.ConfirmRequired); err != nil {
			return models.Scorecard{}, fmt.Errorf("update scorecard confirmation: %w", err)
		}
		if !*input.ConfirmRequired {
			if err := s.applyWaiting(ctx, tx, instruction.ID, input.UserID); err != nil {
				return models.Scorecard{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return models.Scorecard{}, fmt.Errorf("commit scorecard edit: %w", err)
	}
	return s.Get(ctx, instruction.ID, input.UserID)
}

// Confirm applies a scorecard that was compiled while the instruction asked
// for confirmation.
func (s *Service) Confirm(ctx context.Context, instructionID, scorecardID, userID uuid.UUID, lockVersion int) (models.Scorecard, error) {
	if _, err := s.access.AuthorizeEdit(ctx, instructionID, userID); err != nil {
		return models.Scorecard{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("begin scorecard confirm: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	card, err := lockLatestCard(ctx, tx, instructionID)
	if err != nil {
		return models.Scorecard{}, err
	}
	if card.ID != scorecardID || card.LockVersion != lockVersion {
		return models.Scorecard{}, models.ErrScorecardVersionConflict
	}
	if card.Status != models.ScorecardStatusReady || !card.AwaitingConfirmation {
		return models.Scorecard{}, models.ErrScorecardNotReady
	}
	if err := makeCurrent(ctx, tx, card, userID); err != nil {
		return models.Scorecard{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.Scorecard{}, fmt.Errorf("commit scorecard confirm: %w", err)
	}
	return s.Get(ctx, instructionID, userID)
}

// SameAs corrects an automatic match: a criterion the model took for new is
// the same as one the previous scorecard had. The history under the old key is
// read under the new one from then on; no stored result is rewritten.
func (s *Service) SameAs(ctx context.Context, instructionID, userID, criterionKey, canonicalKey uuid.UUID) (models.Scorecard, error) {
	if _, err := s.access.AuthorizeEdit(ctx, instructionID, userID); err != nil {
		return models.Scorecard{}, err
	}
	card, err := s.Get(ctx, instructionID, userID)
	if err != nil {
		return models.Scorecard{}, err
	}
	if card.Status != models.ScorecardStatusReady {
		return models.Scorecard{}, models.ErrScorecardNotReady
	}
	criterion, ok := findCriterion(card.Criteria, criterionKey)
	if !ok || criterion.ChangeKind != models.CriterionChangeNew || criterion.SameAs != nil {
		return models.Scorecard{}, models.ErrScorecardInvalid
	}
	removed := false
	for _, candidate := range card.RemovedCriteria {
		removed = removed || candidate.Key == canonicalKey
	}
	if !removed {
		return models.Scorecard{}, models.ErrScorecardInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("begin criterion alias: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO criterion_key_aliases (alias_key, canonical_key, instruction_uuid, created_by_user_uuid) VALUES ($1,$2,$3,$4)`, criterionKey, canonicalKey, instructionID, userID); err != nil {
		return models.Scorecard{}, fmt.Errorf("insert criterion alias: %w", err)
	}
	if s.onAlias != nil {
		if err := s.onAlias(ctx, tx, criterionKey, canonicalKey); err != nil {
			return models.Scorecard{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return models.Scorecard{}, fmt.Errorf("commit criterion alias: %w", err)
	}
	return s.Get(ctx, instructionID, userID)
}

// Split corrects the opposite mistake: a criterion the model tied to the old
// one is really a different criterion. It gets a new key in a new revision, so
// the old criterion's history ends where it was.
func (s *Service) Split(ctx context.Context, instructionID, userID, criterionKey uuid.UUID) (models.Scorecard, error) {
	if _, err := s.access.AuthorizeEdit(ctx, instructionID, userID); err != nil {
		return models.Scorecard{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("begin criterion split: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	card, err := lockLatestCard(ctx, tx, instructionID)
	if err != nil {
		return models.Scorecard{}, err
	}
	if card.Status != models.ScorecardStatusReady {
		return models.Scorecard{}, models.ErrScorecardNotReady
	}
	criteria := make([]models.ScorecardCriterion, len(card.Criteria))
	copy(criteria, card.Criteria)
	found := false
	for i := range criteria {
		if criteria[i].Key != criterionKey {
			continue
		}
		if criteria[i].ChangeKind == models.CriterionChangeNew {
			return models.Scorecard{}, models.ErrScorecardInvalid
		}
		criteria[i].Key = uuid.New()
		criteria[i].ChangeKind = models.CriterionChangeNew
		found = true
	}
	if !found {
		return models.Scorecard{}, models.ErrScorecardInvalid
	}
	if _, err := s.newRevision(ctx, tx, card, criteria, userID); err != nil {
		return models.Scorecard{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.Scorecard{}, fmt.Errorf("commit criterion split: %w", err)
	}
	return s.Get(ctx, instructionID, userID)
}

// Ensure starts the compile of the latest version at once instead of after the
// quiet period. The instruction page calls it when the scorecard tab opens.
// Only an editor may start it: the compile is paid by the instruction's owner.
func (s *Service) Ensure(ctx context.Context, instructionID, userID uuid.UUID) (models.Scorecard, error) {
	if _, err := s.access.AuthorizeEdit(ctx, instructionID, userID); err != nil {
		return models.Scorecard{}, err
	}
	version, err := latestVersionOf(ctx, s.db, instructionID)
	if err != nil {
		return models.Scorecard{}, err
	}
	if err := ensureCompile(ctx, s.db, instructionID, version.ID); err != nil {
		return models.Scorecard{}, err
	}
	return s.Get(ctx, instructionID, userID)
}

// Recompile compiles the latest version again, at most once a minute per
// instruction since every compile is a paid call.
func (s *Service) Recompile(ctx context.Context, instructionID, userID uuid.UUID) (models.Scorecard, error) {
	if _, err := s.access.AuthorizeEdit(ctx, instructionID, userID); err != nil {
		return models.Scorecard{}, err
	}
	version, err := latestVersionOf(ctx, s.db, instructionID)
	if err != nil {
		return models.Scorecard{}, err
	}
	card, err := latestCardOfVersion(ctx, s.db, version.ID)
	switch {
	case errors.Is(err, models.ErrScorecardNotFound):
		err = ensureCompile(ctx, s.db, instructionID, version.ID)
	case err != nil:
	case card.Status == models.ScorecardStatusReady:
		if s.now().Sub(card.CreatedAt) < recompileCooldown {
			return models.Scorecard{}, models.ErrScorecardRecompileLimited
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO instruction_scorecards (scorecard_uuid, instruction_uuid, instruction_version_uuid, revision, status, origin,
				content_sha256, compile_after, created_by_user_uuid, lock_version)
			VALUES ($1,$2,$3,$4,'queued','compiled',$5,now(),$6,$7)`,
			uuid.New(), instructionID, version.ID, card.Revision+1, card.ContentSHA256, userID, card.LockVersion+1)
	case card.Status == models.ScorecardStatusFailed:
		if s.now().Sub(card.UpdatedAt) < recompileCooldown {
			return models.Scorecard{}, models.ErrScorecardRecompileLimited
		}
		_, err = s.db.ExecContext(ctx, `
			UPDATE instruction_scorecards
			SET status = 'queued', attempts = 0, compile_after = now(), error_code = NULL, error_message = NULL,
			    lease_until = NULL, updated_at = now()
			WHERE scorecard_uuid = $1 AND status = 'failed'`, card.ID)
	default:
		err = ensureCompile(ctx, s.db, instructionID, version.ID)
	}
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("recompile scorecard: %w", err)
	}
	return s.Get(ctx, instructionID, userID)
}

// ensureCompile makes the version's scorecard compile now: it creates the queue
// row when a version predates scorecards and pulls a waiting one forward.
func ensureCompile(ctx context.Context, q queryer, instructionID, versionID uuid.UUID) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO instruction_scorecards (scorecard_uuid, instruction_uuid, instruction_version_uuid, revision, status, origin, content_sha256, compile_after)
		SELECT $1, $2, v.instruction_version_uuid, 1, 'queued', 'compiled', v.content_sha256, now()
		FROM analysis_instruction_versions v WHERE v.instruction_version_uuid = $3
		ON CONFLICT (instruction_version_uuid, revision) DO UPDATE
		SET compile_after = LEAST(instruction_scorecards.compile_after, now()), updated_at = now()
		WHERE instruction_scorecards.status = 'queued'`, uuid.New(), instructionID, versionID)
	if err != nil {
		return fmt.Errorf("queue scorecard compile: %w", err)
	}
	// A newer queued revision of the same version, from a recompile, is pulled
	// forward too.
	_, err = q.ExecContext(ctx, `UPDATE instruction_scorecards SET compile_after = now(), updated_at = now() WHERE instruction_version_uuid = $1 AND status = 'queued' AND compile_after > now()`, versionID)
	return err
}

// lockLatestCard reads the newest revision of the latest version and locks it,
// so two edits cannot both build on the same revision.
func lockLatestCard(ctx context.Context, tx *sql.Tx, instructionID uuid.UUID) (models.Scorecard, error) {
	version, err := latestVersionOf(ctx, tx, instructionID)
	if err != nil {
		return models.Scorecard{}, err
	}
	var cardID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT scorecard_uuid FROM instruction_scorecards WHERE instruction_version_uuid = $1 ORDER BY revision DESC LIMIT 1 FOR UPDATE`, version.ID).Scan(&cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Scorecard{}, models.ErrScorecardNotFound
	}
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("lock scorecard: %w", err)
	}
	return loadCard(ctx, tx, cardID)
}

// newRevision stores edited criteria as the next revision and moves the
// current and waiting flags over to it.
func (s *Service) newRevision(ctx context.Context, tx *sql.Tx, card models.Scorecard, criteria []models.ScorecardCriterion, userID uuid.UUID) (uuid.UUID, error) {
	revisionID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		UPDATE instruction_scorecards
		SET is_current = false, awaiting_confirmation = false, superseded_at = now(), updated_at = now()
		WHERE scorecard_uuid = $1`, card.ID); err != nil {
		return uuid.Nil, fmt.Errorf("supersede scorecard revision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO instruction_scorecards (scorecard_uuid, instruction_uuid, instruction_version_uuid, revision, status, origin,
			is_current, awaiting_confirmation, content_sha256, compiler_version, model, created_by_user_uuid, lock_version)
		VALUES ($1,$2,$3,$4,'ready','edited',$5,$6,$7,$8,$9,$10,$11)`,
		revisionID, card.InstructionID, card.VersionID, card.Revision+1, card.IsCurrent, card.AwaitingConfirmation,
		card.ContentSHA256, card.CompilerVersion, card.Model, userID, card.LockVersion+1); err != nil {
		return uuid.Nil, fmt.Errorf("insert scorecard revision: %w", err)
	}
	if err := insertCriteria(ctx, tx, revisionID, criteria); err != nil {
		return uuid.Nil, err
	}
	return revisionID, nil
}

func makeCurrent(ctx context.Context, tx *sql.Tx, card models.Scorecard, userID uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE instruction_scorecards SET is_current = false, superseded_at = now(), updated_at = now()
		WHERE instruction_uuid = $1 AND is_current AND scorecard_uuid <> $2`, card.InstructionID, card.ID); err != nil {
		return fmt.Errorf("retire current scorecard: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE instruction_scorecards
		SET is_current = true, awaiting_confirmation = false, confirmed_by_user_uuid = $2, confirmed_at = now(),
		    lock_version = lock_version + 1, updated_at = now()
		WHERE scorecard_uuid = $1`, card.ID, userID); err != nil {
		return fmt.Errorf("apply scorecard: %w", err)
	}
	return nil
}

// applyWaiting applies the scorecard left waiting when confirmation is turned
// off: without the switch there is nothing it could still be waiting for.
func (s *Service) applyWaiting(ctx context.Context, tx *sql.Tx, instructionID, userID uuid.UUID) error {
	card, err := lockLatestCard(ctx, tx, instructionID)
	if errors.Is(err, models.ErrScorecardNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if card.Status != models.ScorecardStatusReady || !card.AwaitingConfirmation {
		return nil
	}
	return makeCurrent(ctx, tx, card, userID)
}

func findCriterion(criteria []models.ScorecardCriterion, key uuid.UUID) (models.ScorecardCriterion, bool) {
	for _, criterion := range criteria {
		if criterion.Key == key {
			return criterion, true
		}
	}
	return models.ScorecardCriterion{}, false
}
