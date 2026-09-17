package transcriptionedit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/companystate"
	"verbatrace/monolit/internal/models"
	repo "verbatrace/monolit/internal/repository"

	"github.com/google/uuid"
)

type WordEdit struct {
	WordIndex int     `json:"word_index"`
	Text      *string `json:"text,omitempty"`
	Speaker   *string `json:"speaker,omitempty"`
}

type UpdateInput struct {
	CallUUID         uuid.UUID
	UserUUID         uuid.UUID
	ExpectedRevision int
	Reason           string
	Edits            []WordEdit
}

type Revision struct {
	ID                   uuid.UUID
	CallUUID             uuid.UUID
	Revision             int
	Reason               string
	ChangedWordIndexes   []int
	RestoredFromRevision *int
	CreatedBy            *uuid.UUID
	CreatedAt            time.Time
	IsCurrent            bool
}

type RevisionContent struct {
	Revision  int
	IsCurrent bool
	Text      string
	Segments  []models.TranscriptionSegment
	Words     []models.TranscriptionWord
}

func (s *Service) GetRevision(ctx context.Context, callID, userID uuid.UUID, revision int) (RevisionContent, error) {
	if revision < 1 {
		return RevisionContent{}, models.ErrInvalidTranscriptionEdit
	}
	if _, err := s.callRepository.GetByUUID(ctx, callID, userID); err != nil {
		return RevisionContent{}, err
	}
	var raw []byte
	var current bool
	err := s.db.QueryRowContext(ctx, `SELECT c.payload, r.revision=COALESCE(s.active_revision, (SELECT max(r2.revision) FROM call_transcription_revisions r2 WHERE r2.transcription_uuid=t.transcription_uuid)) FROM call_transcription_revisions r JOIN call_transcriptions t ON t.transcription_uuid=r.transcription_uuid JOIN call_transcription_contents c ON c.transcription_content_uuid=r.transcription_content_uuid LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1 AND r.revision=$2`, callID, revision).Scan(&raw, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return RevisionContent{}, models.ErrTranscriptionNotFound
	}
	if err != nil {
		return RevisionContent{}, err
	}
	content := RevisionContent{Revision: revision, IsCurrent: current}
	if err = json.Unmarshal(raw, &content); err != nil {
		return RevisionContent{}, err
	}
	return content, nil
}

func (s *Service) Restore(ctx context.Context, callID, userID uuid.UUID, expectedRevision, targetRevision int, reason string) (models.Transcription, Revision, error) {
	if targetRevision < 1 {
		return models.Transcription{}, Revision{}, models.ErrInvalidTranscriptionEdit
	}
	// Everybody who may read the call may correct its transcript; the read
	// itself is the permission check.
	if _, err := s.callRepository.GetByUUID(ctx, callID, userID); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if err := s.ensureCompanyActive(ctx, callID); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if err := s.ensureNotUnderReview(ctx, callID); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if expectedRevision < 1 {
		expectedRevision = 1
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var transcriptionID uuid.UUID
	var activeRevision int
	var currentText string
	err = tx.QueryRowContext(ctx, `SELECT t.transcription_uuid, COALESCE(s.active_revision, (SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid), 1), t.text FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1 AND t.status='transcribed' FOR UPDATE OF t`, callID).Scan(&transcriptionID, &activeRevision, &currentText)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if activeRevision != expectedRevision {
		return models.Transcription{}, Revision{}, models.ErrTranscriptionRevisionConflict
	}
	// Selecting an old revision must not undo an existing privacy mask.
	var losesMask bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (
        SELECT 1 FROM call_transcription_redaction_spans current_span
        WHERE current_span.transcription_uuid=$1 AND current_span.revision=$2
        AND NOT EXISTS (
            SELECT 1 FROM call_transcription_redaction_spans target_span
            WHERE target_span.transcription_uuid=$1 AND target_span.revision=$3
            AND target_span.start_seconds=current_span.start_seconds
            AND target_span.end_seconds=current_span.end_seconds
            AND target_span.entity_type=current_span.entity_type
            AND target_span.marker=current_span.marker
        ))`, transcriptionID, activeRevision, targetRevision).Scan(&losesMask)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if losesMask {
		return models.Transcription{}, Revision{}, models.ErrRedactedWordEditForbidden
	}
	var raw []byte
	var rev Revision
	var actor uuid.NullUUID
	var changed []byte
	err = tx.QueryRowContext(ctx, `SELECT r.transcription_revision_uuid, r.reason, r.changed_word_indexes, r.created_by_user_uuid, r.created_at, c.payload FROM call_transcription_revisions r JOIN call_transcription_contents c ON c.transcription_content_uuid=r.transcription_content_uuid WHERE r.transcription_uuid=$1 AND r.revision=$2`, transcriptionID, targetRevision).Scan(&rev.ID, &rev.Reason, &changed, &actor, &rev.CreatedAt, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Transcription{}, Revision{}, models.ErrInvalidTranscriptionEdit
	}
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	_ = json.Unmarshal(changed, &rev.ChangedWordIndexes)
	if actor.Valid {
		rev.CreatedBy = &actor.UUID
	}
	var payload struct {
		Text     string                        `json:"text"`
		Segments []models.TranscriptionSegment `json:"segments"`
		Words    []models.TranscriptionWord    `json:"words"`
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_transcriptions SET text=$2, segments=$3::jsonb, words=$4::jsonb, updated_at=now() WHERE transcription_uuid=$1`, transcriptionID, payload.Text, mustJSON(payload.Segments), mustJSON(payload.Words)); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_revision_state (transcription_uuid, active_revision, updated_by_user_uuid, updated_at) VALUES ($1,$2,$3,now()) ON CONFLICT (transcription_uuid) DO UPDATE SET active_revision=EXCLUDED.active_revision, updated_by_user_uuid=EXCLUDED.updated_by_user_uuid, updated_at=EXCLUDED.updated_at`, transcriptionID, targetRevision, userID); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_privacy_states SET transcription_revision=$2,detected_spans=(SELECT count(*) FROM call_transcription_redaction_spans WHERE transcription_uuid=$3 AND revision=$2),updated_at=now(),lock_version=lock_version+1 WHERE call_uuid=$1 AND status='ready'`, callID, targetRevision, transcriptionID); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_edit_audit (audit_uuid, transcription_uuid, revision, actor_user_uuid, operation, reason, created_at) VALUES ($1,$2,$3,$4,'select_revision',$5,now())`, uuid.New(), transcriptionID, targetRevision, userID, "Выбрана версия транскрипции"); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if payload.Text != currentText {
		if err = markAnalysisStale(ctx, tx, callID); err != nil {
			return models.Transcription{}, Revision{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	updated, err := s.transcription.GetByCallUUID(ctx, callID)
	rev.CallUUID, rev.Revision, rev.IsCurrent = callID, targetRevision, true
	return updated, rev, err
}

type Service struct {
	db             *sql.DB
	callRepository repo.CallRepository
	transcription  repo.TranscriptionRepository
}

// ensureNotUnderReview keeps the transcript frozen while a quality review is
// open: the reviewer must judge the same words the reviewed person saw.
func (s *Service) ensureNotUnderReview(ctx context.Context, callID uuid.UUID) error {
	if s.db == nil {
		return nil
	}
	var locked bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM call_quality_reviews WHERE call_uuid=$1 AND status IN ('unassigned','assigned','in_review','appealed'))`, callID).Scan(&locked)
	if err != nil {
		return err
	}
	if locked {
		return models.ErrTranscriptionLockedByReview
	}
	return nil
}

// ensureCompanyActive keeps the transcript of a frozen company readable but
// unchangeable, like everything else inside it.
func (s *Service) ensureCompanyActive(ctx context.Context, callID uuid.UUID) error {
	if s.db == nil {
		return nil
	}
	var companyID uuid.NullUUID
	if err := s.db.QueryRowContext(ctx, `SELECT company_uuid FROM calls WHERE call_uuid=$1`, callID).Scan(&companyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrCallNotFound
		}
		return err
	}

	return companystate.EnsureActiveNullable(ctx, s.db, companyID)
}

// markAnalysisStale is called when the words themselves changed: the analysis
// was built on the old text and must be re-run before anyone trusts it again.
// Renaming a speaker leaves the text intact, so it does not invalidate anything.
func markAnalysisStale(ctx context.Context, tx *sql.Tx, callID uuid.UUID) error {
	if _, err := tx.ExecContext(ctx, `UPDATE call_analyses SET status='stale',error_message=NULL,updated_at=now() WHERE call_uuid=$1 AND status='done'`, callID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE call_search_documents SET status='stale',updated_at=now() WHERE call_uuid=$1 AND status='ready'`, callID)
	return err
}

func (s *Service) CurrentRevision(ctx context.Context, callID, userID uuid.UUID) (int, error) {
	if _, err := s.callRepository.GetByUUID(ctx, callID, userID); err != nil {
		return 0, err
	}
	var revision int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(s.active_revision, (SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid), 1) FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1`, callID).Scan(&revision)
	return revision, err
}

func NewService(db *sql.DB, calls repo.CallRepository, transcriptions repo.TranscriptionRepository) *Service {
	return &Service{db: db, callRepository: calls, transcription: transcriptions}
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (models.Transcription, Revision, error) {
	if input.CallUUID == uuid.Nil || input.UserUUID == uuid.Nil || input.ExpectedRevision < 1 || len(input.Edits) == 0 {
		return models.Transcription{}, Revision{}, models.ErrInvalidTranscriptionEdit
	}
	if strings.TrimSpace(input.Reason) == "" {
		input.Reason = "Исправление транскрипции"
	}
	if err := validateReason(input.Reason); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if _, err := s.callRepository.GetByUUID(ctx, input.CallUUID, input.UserUUID); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if err := s.ensureCompanyActive(ctx, input.CallUUID); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if err := s.ensureNotUnderReview(ctx, input.CallUUID); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if s.db == nil {
		return models.Transcription{}, Revision{}, errors.New("transcription edit database is not configured")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var currentRevision, maxRevision int
	var transcriptionID uuid.UUID
	row := tx.QueryRowContext(ctx, `SELECT t.transcription_uuid, COALESCE(s.active_revision, (SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid), 1), COALESCE((SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid), 1) FROM call_transcriptions t LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1 AND t.status='transcribed' FOR UPDATE OF t`, input.CallUUID)
	if err := row.Scan(&transcriptionID, &currentRevision, &maxRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Transcription{}, Revision{}, models.ErrTranscriptionNotFound
		}
		return models.Transcription{}, Revision{}, err
	}
	if currentRevision != input.ExpectedRevision {
		return models.Transcription{}, Revision{}, models.ErrTranscriptionRevisionConflict
	}
	redactedIndexes := map[int]struct{}{}
	spanRows, spanErr := tx.QueryContext(ctx, `SELECT word_start_index,word_end_index FROM call_transcription_redaction_spans WHERE transcription_uuid=$1 AND revision=$2`, transcriptionID, currentRevision)
	if spanErr != nil {
		return models.Transcription{}, Revision{}, spanErr
	}
	for spanRows.Next() {
		var start, end int
		if spanErr = spanRows.Scan(&start, &end); spanErr != nil {
			_ = spanRows.Close()
			return models.Transcription{}, Revision{}, spanErr
		}
		for index := start; index <= end; index++ {
			redactedIndexes[index] = struct{}{}
		}
	}
	if spanErr = spanRows.Close(); spanErr != nil {
		return models.Transcription{}, Revision{}, spanErr
	}
	transcription, err := s.transcription.GetByCallUUID(ctx, input.CallUUID)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if len(transcription.Words) == 0 {
		return models.Transcription{}, Revision{}, models.ErrTranscriptionNotEditable
	}
	words := append([]models.TranscriptionWord(nil), transcription.Words...)
	seen := make(map[int]struct{}, len(input.Edits))
	changed := make([]int, 0, len(input.Edits))
	// Both a corrected word and a word moved to another speaker change what the
	// analysis was built on. Only renaming a speaker leaves the content alone.
	contentChanged := false
	for _, edit := range input.Edits {
		if edit.WordIndex < 0 || edit.WordIndex >= len(words) {
			return models.Transcription{}, Revision{}, models.ErrInvalidTranscriptionEdit
		}
		if _, ok := seen[edit.WordIndex]; ok {
			return models.Transcription{}, Revision{}, models.ErrInvalidTranscriptionEdit
		}
		if _, protected := redactedIndexes[edit.WordIndex]; protected {
			return models.Transcription{}, Revision{}, models.ErrRedactedWordEditForbidden
		}
		seen[edit.WordIndex] = struct{}{}
		word := &words[edit.WordIndex]
		changedHere := false
		if edit.Text != nil {
			value := strings.TrimSpace(*edit.Text)
			if value == "" || len([]rune(value)) > 200 || strings.ContainsAny(value, "\r\n") {
				return models.Transcription{}, Revision{}, models.ErrInvalidTranscriptionEdit
			}
			if value != word.Text {
				word.Text = value
				changedHere = true
				contentChanged = true
			}
		}
		if edit.Speaker != nil {
			value := strings.TrimSpace(*edit.Speaker)
			if len([]rune(value)) > 100 {
				return models.Transcription{}, Revision{}, models.ErrInvalidTranscriptionEdit
			}
			if value != word.Speaker {
				word.Speaker = value
				changedHere = true
				contentChanged = true
			}
		}
		if changedHere {
			changed = append(changed, edit.WordIndex)
		}
	}
	if len(changed) == 0 {
		return models.Transcription{}, Revision{}, models.ErrNoTranscriptionChanges
	}
	text := renderWords(words)
	segments := renderSegments(words)
	payload := map[string]any{"text": text, "segments": segments, "words": words}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	hash := sha256.Sum256(canonical)
	var contentID uuid.UUID
	row = tx.QueryRowContext(ctx, `SELECT transcription_content_uuid FROM call_transcription_contents WHERE transcription_uuid=$1 AND content_sha256=$2 AND canonical_size_bytes=$3 AND payload=$4::jsonb LIMIT 1`, transcriptionID, hash[:], len(canonical), string(canonical))
	if err := row.Scan(&contentID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return models.Transcription{}, Revision{}, err
		}
		contentID = uuid.New()
		_, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_contents (transcription_content_uuid, transcription_uuid, content_sha256, canonical_size_bytes, payload, created_at) VALUES ($1,$2,$3,$4,$5::jsonb,now())`, contentID, transcriptionID, hash[:], len(canonical), string(canonical))
		if err != nil {
			return models.Transcription{}, Revision{}, err
		}
	}
	newRevision := maxRevision + 1
	revisionID := uuid.New()
	changedJSON, _ := json.Marshal(changed)
	_, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_revisions (transcription_revision_uuid, transcription_uuid, transcription_content_uuid, revision, reason, changed_word_indexes, created_by_user_uuid, created_at) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,now())`, revisionID, transcriptionID, contentID, newRevision, strings.TrimSpace(input.Reason), string(changedJSON), input.UserUUID)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_revision_state (transcription_uuid, active_revision, updated_by_user_uuid, updated_at) VALUES ($1,$2,$3,now()) ON CONFLICT (transcription_uuid) DO UPDATE SET active_revision=EXCLUDED.active_revision, updated_by_user_uuid=EXCLUDED.updated_by_user_uuid, updated_at=EXCLUDED.updated_at`, transcriptionID, newRevision, input.UserUUID)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_redaction_spans(redaction_span_uuid,transcription_uuid,revision,entity_type,marker,word_start_index,word_end_index,start_seconds,end_seconds,source,provider_policy,created_by_user_uuid,created_at)
		SELECT gen_random_uuid(),transcription_uuid,$3,entity_type,marker,word_start_index,word_end_index,start_seconds,end_seconds,source,provider_policy,created_by_user_uuid,now()
		FROM call_transcription_redaction_spans WHERE transcription_uuid=$1 AND revision=$2`, transcriptionID, currentRevision, newRevision)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE call_privacy_states SET transcription_revision=$2,updated_at=now(),lock_version=lock_version+1 WHERE call_uuid=$1 AND status='ready'`, input.CallUUID, newRevision)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_edit_audit (audit_uuid, transcription_uuid, revision, actor_user_uuid, operation, reason, created_at) VALUES ($1,$2,$3,$4,'update',$5,now())`, uuid.New(), transcriptionID, newRevision, input.UserUUID, strings.TrimSpace(input.Reason))
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE call_transcriptions SET text=$2, segments=$3::jsonb, words=$4::jsonb, updated_at=now() WHERE transcription_uuid=$1`, transcriptionID, text, mustJSON(segments), mustJSON(words))
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	if contentChanged {
		if err = markAnalysisStale(ctx, tx, input.CallUUID); err != nil {
			return models.Transcription{}, Revision{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return models.Transcription{}, Revision{}, err
	}
	updated, err := s.transcription.GetByCallUUID(ctx, input.CallUUID)
	if err != nil {
		return models.Transcription{}, Revision{}, err
	}
	return updated, Revision{ID: revisionID, CallUUID: input.CallUUID, Revision: newRevision, Reason: strings.TrimSpace(input.Reason), ChangedWordIndexes: changed, CreatedBy: &input.UserUUID, CreatedAt: time.Now().UTC(), IsCurrent: true}, nil
}

func (s *Service) List(ctx context.Context, callID, userID uuid.UUID, limit, offset int) ([]Revision, int, error) {
	if _, err := s.callRepository.GetByUUID(ctx, callID, userID); err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.transcription_revision_uuid, t.call_uuid, r.revision, r.reason, r.changed_word_indexes, r.restored_from_revision, r.created_by_user_uuid, r.created_at, r.revision=COALESCE(s.active_revision, (SELECT max(r2.revision) FROM call_transcription_revisions r2 WHERE r2.transcription_uuid=t.transcription_uuid)), count(*) over() FROM call_transcription_revisions r JOIN call_transcriptions t ON t.transcription_uuid=r.transcription_uuid LEFT JOIN call_transcription_revision_state s ON s.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1 ORDER BY r.revision DESC LIMIT $2 OFFSET $3`, callID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := []Revision{}
	total := 0
	for rows.Next() {
		var item Revision
		var raw []byte
		var actor uuid.NullUUID
		var restored sql.NullInt64
		if err := rows.Scan(&item.ID, &item.CallUUID, &item.Revision, &item.Reason, &raw, &restored, &actor, &item.CreatedAt, &item.IsCurrent, &total); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(raw, &item.ChangedWordIndexes)
		if restored.Valid {
			v := int(restored.Int64)
			item.RestoredFromRevision = &v
		}
		if actor.Valid {
			item.CreatedBy = &actor.UUID
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func validateReason(reason string) error {
	n := len([]rune(strings.TrimSpace(reason)))
	if n < 3 || n > 500 {
		return models.ErrInvalidTranscriptionEdit
	}
	return nil
}
func mustJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
func renderWords(words []models.TranscriptionWord) string {
	var b strings.Builder
	for i, w := range words {
		if i > 0 && needsSpace(words[i-1].Text, w.Text) {
			b.WriteByte(' ')
		}
		b.WriteString(w.Text)
	}
	return b.String()
}
func needsSpace(prev, next string) bool {
	if prev == "" || next == "" {
		return false
	}
	return !strings.ContainsRune(".,!?;:%)]}", []rune(next)[0]) && !strings.ContainsRune("([{", []rune(prev)[len([]rune(prev))-1])
}
func renderSegments(words []models.TranscriptionWord) []models.TranscriptionSegment {
	out := []models.TranscriptionSegment{}
	for _, w := range words {
		if len(out) == 0 || out[len(out)-1].Speaker != w.Speaker {
			start, end := w.StartSeconds, w.EndSeconds
			out = append(out, models.TranscriptionSegment{Speaker: w.Speaker, StartSeconds: &start, EndSeconds: &end, Text: w.Text})
		} else {
			seg := &out[len(out)-1]
			seg.Text = renderWords([]models.TranscriptionWord{{Text: seg.Text}, {Text: w.Text}})
			seg.EndSeconds = &w.EndSeconds
		}
	}
	return out
}

var _ = fmt.Sprintf
