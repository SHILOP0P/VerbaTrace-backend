package privacy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

var (
	ErrCorrectionInvalid  = errors.New("privacy correction invalid")
	ErrCorrectionConflict = errors.New("privacy correction revision conflict")
)

type CorrectionInput struct {
	Operation        string
	ExpectedRevision int
	Reason           string
	WordStartIndex   *int
	WordEndIndex     *int
	SpanID           *uuid.UUID
	EntityType       string
	ReplacementText  string
	RemoveConfirmed  bool
}

type CorrectionPreview struct {
	Operation        string `json:"operation"`
	CurrentRevision  int    `json:"current_revision"`
	Before           string `json:"before"`
	After            string `json:"after"`
	EntityLabel      string `json:"entity_label"`
	Marker           string `json:"marker"`
	RequiresConfirm  bool   `json:"requires_confirmation"`
	ReanalysisNeeded bool   `json:"reanalysis_required"`
}

type correctionState struct {
	TranscriptionID uuid.UUID
	Revision        int
	MaxRevision     int
	Words           []models.TranscriptionWord
	Spans           []models.RedactionSpan
}

func (s *Service) PreviewCorrection(ctx context.Context, call models.Call, actorID uuid.UUID, input CorrectionInput) (CorrectionPreview, error) {
	if err := s.validateCorrectionAccess(ctx, call, actorID, input); err != nil {
		return CorrectionPreview{}, err
	}
	state, err := s.loadCorrectionState(ctx, s.db, call.ID, false)
	if err != nil {
		return CorrectionPreview{}, err
	}
	if state.Revision != input.ExpectedRevision {
		return CorrectionPreview{}, ErrCorrectionConflict
	}
	before := renderPrivacyWords(state.Words)
	words, correctedSpans, _, err := applyCorrection(state.Words, state.Spans, input, actorID)
	if err != nil {
		return CorrectionPreview{}, err
	}
	return CorrectionPreview{Operation: input.Operation, CurrentRevision: state.Revision, Before: before, After: renderPrivacyWords(words), EntityLabel: privacyEntityLabel(input.EntityType), Marker: markerForCorrection(input, correctedSpans), RequiresConfirm: input.Operation == "remove_mask", ReanalysisNeeded: true}, nil
}

func (s *Service) ApplyCorrection(ctx context.Context, call models.Call, actorID uuid.UUID, input CorrectionInput) (int, error) {
	if err := s.validateCorrectionAccess(ctx, call, actorID, input); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := s.loadCorrectionState(ctx, tx, call.ID, true)
	if err != nil {
		return 0, err
	}
	if state.Revision != input.ExpectedRevision {
		return 0, ErrCorrectionConflict
	}
	words, spans, changed, err := applyCorrection(state.Words, state.Spans, input, actorID)
	if err != nil {
		return 0, err
	}
	textValue := renderPrivacyWords(words)
	segments := renderPrivacySegments(words)
	payload := struct {
		Text     string                        `json:"text"`
		Segments []models.TranscriptionSegment `json:"segments"`
		Words    []models.TranscriptionWord    `json:"words"`
	}{textValue, segments, words}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	hash := sha256.Sum256(canonical)
	contentID, _ := uuid.NewV7()
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_contents(transcription_content_uuid,transcription_uuid,content_sha256,canonical_size_bytes,payload,created_at) VALUES($1,$2,$3,$4,$5::jsonb,now()) ON CONFLICT DO NOTHING`, contentID, state.TranscriptionID, hash[:], len(canonical), canonical); err != nil {
		return 0, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT transcription_content_uuid FROM call_transcription_contents WHERE transcription_uuid=$1 AND content_sha256=$2 AND canonical_size_bytes=$3 AND payload=$4::jsonb LIMIT 1`, state.TranscriptionID, hash[:], len(canonical), canonical).Scan(&contentID); err != nil {
		return 0, err
	}
	newRevision := state.MaxRevision + 1
	revisionID, _ := uuid.NewV7()
	changedJSON, _ := json.Marshal(changed)
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_revisions(transcription_revision_uuid,transcription_uuid,transcription_content_uuid,revision,reason,changed_word_indexes,created_by_user_uuid,created_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7,now())`, revisionID, state.TranscriptionID, contentID, newRevision, strings.TrimSpace(input.Reason), changedJSON, actorID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_transcription_revision_state SET active_revision=$2,updated_by_user_uuid=$3,updated_at=now() WHERE transcription_uuid=$1`, state.TranscriptionID, newRevision, actorID); err != nil {
		return 0, err
	}
	for _, span := range spans {
		spanID, _ := uuid.NewV7()
		if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_redaction_spans(redaction_span_uuid,transcription_uuid,revision,entity_type,marker,word_start_index,word_end_index,start_seconds,end_seconds,source,provider_policy,created_by_user_uuid,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,now())`, spanID, state.TranscriptionID, newRevision, span.EntityType, span.Marker, span.WordStartIndex, span.WordEndIndex, span.StartSeconds, span.EndSeconds, span.Source, span.ProviderPolicy, span.CreatedByUserID); err != nil {
			return 0, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_transcriptions SET text=$2,segments=$3::jsonb,words=$4::jsonb,updated_at=now() WHERE transcription_uuid=$1`, state.TranscriptionID, textValue, mustPrivacyJSON(segments), mustPrivacyJSON(words)); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_privacy_states SET transcription_revision=$2,detected_spans=$3,updated_at=now(),lock_version=lock_version+1 WHERE call_uuid=$1 AND status='ready'`, call.ID, newRevision, len(spans)); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE calls SET status='transcribed' WHERE call_uuid=$1 AND status='analyzed'`, call.ID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE call_analyses SET status='stale',error_message=NULL,updated_at=now() WHERE call_uuid=$1 AND status='done'`, call.ID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_edit_audit(audit_uuid,transcription_uuid,revision,actor_user_uuid,operation,reason,created_at) VALUES($1,$2,$3,$4,'privacy_correction',$5,now())`, uuid.New(), state.TranscriptionID, newRevision, actorID, strings.TrimSpace(input.Reason)); err != nil {
		return 0, err
	}
	if err = insertAudit(ctx, tx, scopeForCall(call), scopeIDForCall(call), uuid.NullUUID{UUID: call.ID, Valid: true}, actorID, "redaction_corrected", "call_transcription", state.TranscriptionID, map[string]any{"operation": input.Operation, "revision": newRevision, "entity_type": input.EntityType}); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return newRevision, nil
}

func (s *Service) validateCorrectionAccess(ctx context.Context, call models.Call, actorID uuid.UUID, input CorrectionInput) error {
	if input.Operation != "add_mask" {
		return ErrCorrectionInvalid
	}
	capabilities, err := s.Capabilities(ctx, call, actorID)
	if err != nil {
		return err
	}
	if !capabilities.CanReviewRedactions {
		return ErrPolicyForbidden
	}
	if input.ExpectedRevision < 1 || len([]rune(strings.TrimSpace(input.Reason))) < 10 || len([]rune(strings.TrimSpace(input.Reason))) > 500 {
		return ErrCorrectionInvalid
	}
	switch input.Operation {
	case "add_mask":
		if input.WordStartIndex == nil || input.WordEndIndex == nil || *input.WordStartIndex < 0 || *input.WordEndIndex < *input.WordStartIndex || models.PrivacyMarkersRUv1[input.EntityType] == "" {
			return ErrCorrectionInvalid
		}
	default:
		return ErrCorrectionInvalid
	}
	return nil
}

type correctionQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *Service) loadCorrectionState(ctx context.Context, q correctionQuerier, callID uuid.UUID, lock bool) (correctionState, error) {
	var state correctionState
	var raw []byte
	query := `SELECT t.transcription_uuid,COALESCE(rs.active_revision,1),COALESCE((SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid=t.transcription_uuid),1),t.words FROM call_transcriptions t LEFT JOIN call_transcription_revision_state rs ON rs.transcription_uuid=t.transcription_uuid WHERE t.call_uuid=$1 AND t.status='transcribed'`
	if lock {
		query += ` FOR UPDATE OF t`
	}
	if err := q.QueryRowContext(ctx, query, callID).Scan(&state.TranscriptionID, &state.Revision, &state.MaxRevision, &raw); err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state.Words); err != nil {
		return state, err
	}
	rows, err := q.QueryContext(ctx, `SELECT redaction_span_uuid,transcription_uuid,revision,entity_type,marker,word_start_index,word_end_index,start_seconds,end_seconds,source,COALESCE(provider_policy,''),created_by_user_uuid FROM call_transcription_redaction_spans WHERE transcription_uuid=$1 AND revision=$2 ORDER BY word_start_index`, state.TranscriptionID, state.Revision)
	if err != nil {
		return state, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var span models.RedactionSpan
		if err = rows.Scan(&span.ID, &span.TranscriptionID, &span.Revision, &span.EntityType, &span.Marker, &span.WordStartIndex, &span.WordEndIndex, &span.StartSeconds, &span.EndSeconds, &span.Source, &span.ProviderPolicy, &span.CreatedByUserID); err != nil {
			return state, err
		}
		state.Spans = append(state.Spans, span)
	}
	return state, rows.Err()
}

func applyCorrection(words []models.TranscriptionWord, spans []models.RedactionSpan, input CorrectionInput, actorID uuid.UUID) ([]models.TranscriptionWord, []models.RedactionSpan, []int, error) {
	if input.Operation != "add_mask" {
		return nil, nil, nil, ErrCorrectionInvalid
	}
	resultWords := append([]models.TranscriptionWord(nil), words...)
	resultSpans := append([]models.RedactionSpan(nil), spans...)
	switch input.Operation {
	case "add_mask":
		start, end := *input.WordStartIndex, *input.WordEndIndex
		if end >= len(resultWords) {
			return nil, nil, nil, ErrCorrectionInvalid
		}
		for _, span := range resultSpans {
			if start <= span.WordEndIndex && end >= span.WordStartIndex {
				return nil, nil, nil, ErrCorrectionInvalid
			}
		}
		marker := models.PrivacyMarkersRUv1[input.EntityType]
		merged := resultWords[start]
		merged.Text = marker
		merged.EndSeconds = resultWords[end].EndSeconds
		merged.Confidence = nil
		resultWords = append(append(resultWords[:start:start], merged), resultWords[end+1:]...)
		delta := end - start
		for i := range resultSpans {
			if resultSpans[i].WordStartIndex > end {
				resultSpans[i].WordStartIndex -= delta
				resultSpans[i].WordEndIndex -= delta
			}
		}
		resultSpans = append(resultSpans, models.RedactionSpan{EntityType: input.EntityType, Marker: marker, WordStartIndex: start, WordEndIndex: start, StartSeconds: merged.StartSeconds, EndSeconds: merged.EndSeconds, Source: "manual", CreatedByUserID: uuid.NullUUID{UUID: actorID, Valid: true}})
		return resultWords, resultSpans, []int{start}, nil

	}
	return nil, nil, nil, ErrCorrectionInvalid
}

func renderPrivacyWords(words []models.TranscriptionWord) string {
	var b strings.Builder
	for i, w := range words {
		if i > 0 && needsPrivacySpace(words[i-1].Text, w.Text) {
			b.WriteByte(' ')
		}
		b.WriteString(w.Text)
	}
	return b.String()
}
func needsPrivacySpace(prev, next string) bool {
	if prev == "" || next == "" {
		return false
	}
	nr, pr := []rune(next), []rune(prev)
	return !strings.ContainsRune(".,!?;:%)]}", nr[0]) && !strings.ContainsRune("([{", pr[len(pr)-1])
}
func renderPrivacySegments(words []models.TranscriptionWord) []models.TranscriptionSegment {
	out := []models.TranscriptionSegment{}
	for _, w := range words {
		if len(out) == 0 || out[len(out)-1].Speaker != w.Speaker {
			start, end := w.StartSeconds, w.EndSeconds
			out = append(out, models.TranscriptionSegment{Speaker: w.Speaker, StartSeconds: &start, EndSeconds: &end, Text: w.Text})
		} else {
			seg := &out[len(out)-1]
			seg.Text = renderPrivacyWords([]models.TranscriptionWord{{Text: seg.Text}, {Text: w.Text}})
			seg.EndSeconds = &w.EndSeconds
		}
	}
	return out
}
func mustPrivacyJSON(value any) string { raw, _ := json.Marshal(value); return string(raw) }
func markerForCorrection(input CorrectionInput, spans []models.RedactionSpan) string {
	if marker := models.PrivacyMarkersRUv1[input.EntityType]; marker != "" {
		return marker
	}
	if input.SpanID != nil {
		for _, span := range spans {
			if span.ID == *input.SpanID {
				return span.Marker
			}
		}
	}
	return ""
}
func privacyEntityLabel(entity string) string {
	labels := map[string]string{"person_name": "Имя", "phone_number": "Телефон", "email_address": "Электронная почта", "address": "Адрес", "date_of_birth": "Дата рождения", "passport_number": "Номер паспорта", "drivers_license": "Водительское удостоверение", "account_number": "Номер счёта", "banking_information": "Банковские данные", "credit_card_number": "Номер карты", "credit_card_cvv": "Код карты", "credit_card_expiration": "Срок действия карты", "password": "Секрет", "ip_address": "Сетевой адрес", "username": "Имя пользователя", "medical_condition": "Медицинские данные", "money_amount": "Денежная сумма", "organization": "Организация"}
	if value := labels[entity]; value != "" {
		return value
	}
	return "Скрытые данные"
}
