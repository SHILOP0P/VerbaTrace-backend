package transcription

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	model "verbatrace/monolit/internal/models"
	"verbatrace/monolit/internal/repository/converter"

	"github.com/google/uuid"
)

// MarkTranscribedWithPrivacy publishes every user-visible transcription
// representation, the initial revision, redaction spans and call/privacy status
// in one transaction.
func (r *Repository) MarkTranscribedWithPrivacy(ctx context.Context, id, callID uuid.UUID, result model.TranscriptionResult, state model.CallPrivacyState, provider string) (model.Transcription, error) {
	segments, err := converter.TranscriptionSegmentsToNullString(result.Segments)
	if err != nil {
		return model.Transcription{}, err
	}
	words, err := converter.TranscriptionWordsToNullString(result.Words)
	if err != nil {
		return model.Transcription{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Transcription{}, err
	}
	defer func() { _ = tx.Rollback() }()
	row := tx.QueryRowContext(ctx, `UPDATE call_transcriptions SET status=$2,text=$3,segments=$4::jsonb,words=$5::jsonb,
		language=$6,error_message=NULL,updated_at=now() WHERE transcription_uuid=$1 RETURNING `+transcriptionReturningColumns,
		id, string(model.TranscriptionStatusTranscribed), result.Text, segments, words, result.Language)
	transcription, err := scanUpdatedTranscription(row, "mark privacy transcription transcribed")
	if err != nil {
		return model.Transcription{}, err
	}
	payload := struct {
		Text     string                       `json:"text"`
		Segments []model.TranscriptionSegment `json:"segments"`
		Words    []model.TranscriptionWord    `json:"words"`
	}{result.Text, result.Segments, result.Words}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return model.Transcription{}, err
	}
	contentHash := sha256.Sum256(canonical)
	contentID, err := uuid.NewV7()
	if err != nil {
		return model.Transcription{}, err
	}
	revisionID, err := uuid.NewV7()
	if err != nil {
		return model.Transcription{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_contents(transcription_content_uuid,transcription_uuid,content_sha256,canonical_size_bytes,payload,created_at)
		VALUES($1,$2,$3,$4,$5::jsonb,now()) ON CONFLICT DO NOTHING`, contentID, id, contentHash[:], len(canonical), string(canonical)); err != nil {
		return model.Transcription{}, err
	}
	// If a retry reused identical content, resolve its existing content UUID.
	if err = tx.QueryRowContext(ctx, `SELECT transcription_content_uuid FROM call_transcription_contents WHERE transcription_uuid=$1 AND content_sha256=$2 AND canonical_size_bytes=$3 AND payload=$4::jsonb LIMIT 1`, id, contentHash[:], len(canonical), string(canonical)).Scan(&contentID); err != nil {
		return model.Transcription{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_revisions(transcription_revision_uuid,transcription_uuid,transcription_content_uuid,revision,reason,changed_word_indexes,created_at)
		VALUES($1,$2,$3,1,'Исходная защищённая транскрипция ASR','[]'::jsonb,now()) ON CONFLICT(transcription_uuid,revision) DO NOTHING`, revisionID, id, contentID); err != nil {
		return model.Transcription{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_revision_state(transcription_uuid,active_revision,updated_at) VALUES($1,1,now())
		ON CONFLICT(transcription_uuid) DO UPDATE SET active_revision=1,updated_by_user_uuid=NULL,updated_at=now()`, id); err != nil {
		return model.Transcription{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM call_transcription_redaction_spans WHERE transcription_uuid=$1 AND revision=1`, id); err != nil {
		return model.Transcription{}, err
	}
	for _, span := range result.RedactionSpans {
		spanID, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			return model.Transcription{}, uuidErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO call_transcription_redaction_spans(redaction_span_uuid,transcription_uuid,revision,entity_type,marker,word_start_index,word_end_index,start_seconds,end_seconds,source,provider_policy)
			VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10)`, spanID, id, span.EntityType, span.Marker, span.WordStartIndex, span.WordEndIndex, span.StartSeconds, span.EndSeconds, span.Source, span.ProviderPolicy); err != nil {
			return model.Transcription{}, err
		}
	}
	if state.PolicySnapshot.Enabled {
		if _, err = tx.ExecContext(ctx, `UPDATE call_privacy_states SET status='ready',transcription_revision=1,detected_spans=$2,last_error_code=NULL,last_error_message_safe=NULL,completed_at=now(),updated_at=now(),lock_version=lock_version+1 WHERE call_uuid=$1`, callID, len(result.RedactionSpans)); err != nil {
			return model.Transcription{}, err
		}
		if result.ProviderJobID != "" {
			attemptID, uuidErr := uuid.NewV7()
			if uuidErr != nil {
				return model.Transcription{}, uuidErr
			}
			snapshot, _ := json.Marshal(state.PolicySnapshot)
			requestHash := sha256.Sum256(snapshot)
			if _, err = tx.ExecContext(ctx, `INSERT INTO transcription_provider_attempts(provider_attempt_uuid,call_uuid,attempt_no,provider,request_sha256,provider_job_id,status,attempts,submitted_at,completed_at)
				VALUES($1,$2,1,$3,$4,$5,'delete_pending',1,now(),now()) ON CONFLICT(provider,provider_job_id) WHERE provider_job_id IS NOT NULL DO NOTHING`, attemptID, callID, provider, requestHash[:], result.ProviderJobID); err != nil {
				return model.Transcription{}, err
			}
		}
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE call_privacy_states SET status='not_requested',detected_spans=0,last_error_code=NULL,last_error_message_safe=NULL,updated_at=now(),lock_version=lock_version+1 WHERE call_uuid=$1`, callID); err != nil {
			return model.Transcription{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE calls SET status='transcribed' WHERE call_uuid=$1`, callID); err != nil {
		return model.Transcription{}, fmt.Errorf("mark call transcribed: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return model.Transcription{}, err
	}
	transcription.UpdatedAt = time.Now().UTC()
	return transcription, nil
}
