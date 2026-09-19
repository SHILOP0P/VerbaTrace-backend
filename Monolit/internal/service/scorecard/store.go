package scorecard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const cardColumns = `s.scorecard_uuid, s.instruction_uuid, i.title, s.instruction_version_uuid, v.version, s.revision,
	s.status, s.origin, s.is_current, s.awaiting_confirmation, i.scorecard_confirm_required, s.content_sha256,
	s.compiler_version, s.model, s.attempts, s.compile_after, s.error_code, s.error_message, s.lock_version,
	s.confirmed_at, s.created_at, s.updated_at`

const cardFrom = ` FROM instruction_scorecards s
	JOIN analysis_instruction_versions v ON v.instruction_version_uuid = s.instruction_version_uuid
	JOIN analysis_instructions i ON i.instruction_uuid = s.instruction_uuid`

func scanCard(row interface{ Scan(...any) error }) (models.Scorecard, error) {
	var card models.Scorecard
	var status string
	var model, errorCode, errorMessage sql.NullString
	var compileAfter, confirmedAt sql.NullTime
	err := row.Scan(&card.ID, &card.InstructionID, &card.InstructionTitle, &card.VersionID, &card.InstructionVersion, &card.Revision,
		&status, &card.Origin, &card.IsCurrent, &card.AwaitingConfirmation, &card.ConfirmRequired, &card.ContentSHA256,
		&card.CompilerVersion, &model, &card.Attempts, &compileAfter, &errorCode, &errorMessage, &card.LockVersion,
		&confirmedAt, &card.CreatedAt, &card.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return card, models.ErrScorecardNotFound
	}
	if err != nil {
		return card, fmt.Errorf("scan scorecard: %w", err)
	}
	card.Status = models.ScorecardStatus(status)
	card.Model = nullString(model)
	card.ErrorCode = nullString(errorCode)
	card.ErrorMessage = nullString(errorMessage)
	if compileAfter.Valid {
		card.CompileAfter = &compileAfter.Time
	}
	if confirmedAt.Valid {
		card.ConfirmedAt = &confirmedAt.Time
	}
	return card, nil
}

func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func loadCard(ctx context.Context, q queryer, cardID uuid.UUID) (models.Scorecard, error) {
	card, err := scanCard(q.QueryRowContext(ctx, `SELECT `+cardColumns+cardFrom+` WHERE s.scorecard_uuid = $1`, cardID))
	if err != nil {
		return card, err
	}
	card.Criteria, err = loadCriteria(ctx, q, card.ID)
	return card, err
}

// latestCardOfVersion is the newest revision of a version's scorecard.
func latestCardOfVersion(ctx context.Context, q queryer, versionID uuid.UUID) (models.Scorecard, error) {
	var cardID uuid.UUID
	err := q.QueryRowContext(ctx, `SELECT scorecard_uuid FROM instruction_scorecards WHERE instruction_version_uuid = $1 ORDER BY revision DESC LIMIT 1`, versionID).Scan(&cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Scorecard{}, models.ErrScorecardNotFound
	}
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("find scorecard of version: %w", err)
	}
	return loadCard(ctx, q, cardID)
}

// currentCard is the scorecard in force for an instruction.
func currentCard(ctx context.Context, q queryer, instructionID uuid.UUID) (models.Scorecard, error) {
	var cardID uuid.UUID
	err := q.QueryRowContext(ctx, `SELECT scorecard_uuid FROM instruction_scorecards WHERE instruction_uuid = $1 AND is_current`, instructionID).Scan(&cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Scorecard{}, models.ErrScorecardNotFound
	}
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("find current scorecard: %w", err)
	}
	return loadCard(ctx, q, cardID)
}

// previousCard is the scorecard a new one is compared with: the newest ready
// scorecard of the instruction other than the given one. It may belong to a
// version several saves back, which is how keys survive draft saves that were
// never compiled.
func previousCard(ctx context.Context, q queryer, instructionID, exceptID uuid.UUID, before time.Time) (models.Scorecard, error) {
	var cardID uuid.UUID
	err := q.QueryRowContext(ctx, `
		SELECT scorecard_uuid FROM instruction_scorecards
		WHERE instruction_uuid = $1 AND scorecard_uuid <> $2 AND status = 'ready' AND created_at <= $3
		ORDER BY created_at DESC, revision DESC LIMIT 1`, instructionID, exceptID, before).Scan(&cardID)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Scorecard{}, models.ErrScorecardNotFound
	}
	if err != nil {
		return models.Scorecard{}, fmt.Errorf("find previous scorecard: %w", err)
	}
	return loadCard(ctx, q, cardID)
}

type latestVersion struct {
	ID      uuid.UUID
	Version int
}

func latestVersionOf(ctx context.Context, q queryer, instructionID uuid.UUID) (latestVersion, error) {
	var version latestVersion
	err := q.QueryRowContext(ctx, `SELECT instruction_version_uuid, version FROM analysis_instruction_versions WHERE instruction_uuid = $1 ORDER BY version DESC LIMIT 1`, instructionID).Scan(&version.ID, &version.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return version, models.ErrScorecardNotFound
	}
	if err != nil {
		return version, fmt.Errorf("find latest instruction version: %w", err)
	}
	return version, nil
}

func loadCriteria(ctx context.Context, q queryer, cardID uuid.UUID) ([]models.ScorecardCriterion, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT criterion_key, position, title, requirement, source_excerpt, applicability, depth,
		       required_question, cross_cutting, weight, is_critical, enabled,
		       to_json(edited_fields)::text, change_kind, to_json(warnings)::text
		FROM instruction_scorecard_criteria WHERE scorecard_uuid = $1 ORDER BY position`, cardID)
	if err != nil {
		return nil, fmt.Errorf("list scorecard criteria: %w", err)
	}
	defer func() { _ = rows.Close() }()
	criteria := []models.ScorecardCriterion{}
	for rows.Next() {
		var c models.ScorecardCriterion
		var edited, warnings string
		if err := rows.Scan(&c.Key, &c.Position, &c.Title, &c.Requirement, &c.SourceExcerpt, &c.Applicability, &c.Depth,
			&c.RequiredQuestion, &c.CrossCutting, &c.Weight, &c.IsCritical, &c.Enabled, &edited, &c.ChangeKind, &warnings); err != nil {
			return nil, fmt.Errorf("scan scorecard criterion: %w", err)
		}
		if err := json.Unmarshal([]byte(edited), &c.EditedFields); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(warnings), &c.Warnings); err != nil {
			return nil, err
		}
		criteria = append(criteria, c)
	}
	return criteria, rows.Err()
}

func insertCriteria(ctx context.Context, q queryer, cardID uuid.UUID, criteria []models.ScorecardCriterion) error {
	for i, c := range criteria {
		if _, err := q.ExecContext(ctx, `
			INSERT INTO instruction_scorecard_criteria (
				scorecard_uuid, criterion_key, position, title, requirement, source_excerpt, applicability, depth,
				required_question, cross_cutting, weight, is_critical, enabled, edited_fields, change_kind, warnings
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			cardID, c.Key, i+1, c.Title, c.Requirement, c.SourceExcerpt, c.Applicability, c.Depth,
			c.RequiredQuestion, c.CrossCutting, c.Weight, c.IsCritical, c.Enabled, nonNil(c.EditedFields), c.ChangeKind, nonNil(c.Warnings)); err != nil {
			return fmt.Errorf("insert scorecard criterion: %w", err)
		}
	}
	return nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// aliasedKeys is the set of canonical keys that some newer key already stands
// in for, so they are no longer reported as removed.
func aliasedKeys(ctx context.Context, q queryer, instructionID uuid.UUID) (map[uuid.UUID]bool, map[uuid.UUID]uuid.UUID, error) {
	rows, err := q.QueryContext(ctx, `SELECT alias_key, canonical_key FROM criterion_key_aliases WHERE instruction_uuid = $1`, instructionID)
	if err != nil {
		return nil, nil, fmt.Errorf("list criterion aliases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	canonical := map[uuid.UUID]bool{}
	byAlias := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var alias, target uuid.UUID
		if err := rows.Scan(&alias, &target); err != nil {
			return nil, nil, err
		}
		canonical[target] = true
		byAlias[alias] = target
	}
	return canonical, byAlias, rows.Err()
}

type versionText struct {
	InstructionID    uuid.UUID
	Version          int
	Title            string
	FilePath         string
	OriginalFilename string
	ContentSHA256    string
	Text             *string
}

func loadVersion(ctx context.Context, q queryer, versionID uuid.UUID) (versionText, error) {
	var v versionText
	var text sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT instruction_uuid, version, title_snapshot, file_path, original_filename, content_sha256, content_text
		FROM analysis_instruction_versions WHERE instruction_version_uuid = $1`, versionID).
		Scan(&v.InstructionID, &v.Version, &v.Title, &v.FilePath, &v.OriginalFilename, &v.ContentSHA256, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return v, models.ErrAnalysisInstructionNotFound
	}
	if err != nil {
		return v, fmt.Errorf("load instruction version: %w", err)
	}
	v.Text = nullString(text)
	return v, nil
}

type instructionRow struct {
	ID              uuid.UUID
	Scope           models.AnalysisInstructionScope
	Title           string
	UserID          uuid.NullUUID
	CompanyID       uuid.NullUUID
	DepartmentID    uuid.NullUUID
	CreatedBy       uuid.NullUUID
	ConfirmRequired bool
	Deleted         bool
}

func loadInstruction(ctx context.Context, q queryer, instructionID uuid.UUID) (instructionRow, error) {
	var i instructionRow
	var scope string
	err := q.QueryRowContext(ctx, `
		SELECT instruction_uuid, scope, title, user_uuid, company_uuid, department_uuid, created_by_user_uuid,
		       scorecard_confirm_required, deleted_at IS NOT NULL
		FROM analysis_instructions WHERE instruction_uuid = $1`, instructionID).
		Scan(&i.ID, &scope, &i.Title, &i.UserID, &i.CompanyID, &i.DepartmentID, &i.CreatedBy, &i.ConfirmRequired, &i.Deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return i, models.ErrAnalysisInstructionNotFound
	}
	if err != nil {
		return i, fmt.Errorf("load instruction: %w", err)
	}
	i.Scope = models.AnalysisInstructionScope(scope)
	return i, nil
}

func (i instructionRow) owner() models.InstructionOwner {
	owner := models.InstructionOwner{}
	if i.UserID.Valid {
		owner.UserID = i.UserID.UUID
	} else if i.CreatedBy.Valid {
		owner.UserID = i.CreatedBy.UUID
	}
	if i.CompanyID.Valid {
		owner.CompanyID = i.CompanyID.UUID
	}
	if i.DepartmentID.Valid {
		owner.DepartmentID = i.DepartmentID.UUID
	}
	return owner
}

func collapse(value string) string { return strings.Join(strings.Fields(value), " ") }
