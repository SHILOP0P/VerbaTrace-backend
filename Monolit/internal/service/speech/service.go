package speech

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"verbatrace/monolit/internal/logger"
	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// TranscriptReader gives the active revision of a call's transcript.
type TranscriptReader interface {
	GetReportTranscription(ctx context.Context, callID uuid.UUID, revision int) (models.Transcription, int, error)
}

type Service struct {
	db          *sql.DB
	transcripts TranscriptReader
	log         logger.Logger
}

func NewService(db *sql.DB, transcripts TranscriptReader, log logger.Logger) *Service {
	if log == nil {
		log = logger.NewNop()
	}
	return &Service{db: db, transcripts: transcripts, log: log}
}

// Refresh measures the active revision of a call's transcript. A revision
// already measured is left alone; roles of speakers do not change the numbers,
// only which speaker is the employee, and that is read at query time.
func (s *Service) Refresh(ctx context.Context, callID uuid.UUID) {
	if err := s.refresh(ctx, callID); err != nil {
		s.log.Warn(ctx, "speech metrics not computed", zap.String("call_id", callID.String()), zap.Error(err))
	}
}

func (s *Service) refresh(ctx context.Context, callID uuid.UUID) error {
	transcript, revision, err := s.transcripts.GetReportTranscription(ctx, callID, 0)
	if errors.Is(err, models.ErrTranscriptionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var stored int
	err = s.db.QueryRowContext(ctx, `SELECT transcription_revision FROM call_speech_metrics WHERE call_uuid = $1`, callID).Scan(&stored)
	if err == nil && stored == revision {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	call, speakers := Compute(transcript.Words)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM call_speech_metrics WHERE call_uuid = $1`, callID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO call_speech_metrics (call_uuid, transcription_revision, speaker_switches_per_5min, pauses_over_threshold, longest_pause_seconds)
		VALUES ($1, $2, $3, $4, $5)`, callID, revision, call.SpeakerSwitchesPer5Min, call.PausesOverThreshold, call.LongestPauseSeconds); err != nil {
		return fmt.Errorf("store call speech: %w", err)
	}
	for _, sp := range speakers {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO call_speaker_speech_metrics (call_uuid, speaker_key, talk_seconds, talk_share, words, words_per_minute, longest_monologue_seconds, questions, questions_per_hour, response_pause_median_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			callID, sp.Key, sp.TalkSeconds, sp.TalkShare, sp.Words, sp.WordsPerMinute, sp.LongestMonologueSeconds, sp.Questions, sp.QuestionsPerHour, sp.ResponsePauseMedianMs); err != nil {
			return fmt.Errorf("store speaker speech: %w", err)
		}
	}
	return tx.Commit()
}

// staleCalls are transcribed calls whose metrics are missing or were measured
// on another revision.
const staleCalls = `
	SELECT t.call_uuid
	FROM call_transcriptions t
	JOIN calls c ON c.call_uuid = t.call_uuid AND c.deleted_at IS NULL
	LEFT JOIN call_transcription_revision_state rs ON rs.transcription_uuid = t.transcription_uuid
	LEFT JOIN call_speech_metrics m ON m.call_uuid = t.call_uuid
	WHERE t.status = 'transcribed'
	  AND (m.call_uuid IS NULL OR m.transcription_revision <> COALESCE(rs.active_revision,
	       (SELECT max(r.revision) FROM call_transcription_revisions r WHERE r.transcription_uuid = t.transcription_uuid), 1))
	ORDER BY t.call_uuid
	LIMIT $1`

// Backfill measures calls that have no metrics or stale ones.
func (s *Service) Backfill(ctx context.Context, limit int) int {
	rows, err := s.db.QueryContext(ctx, staleCalls, limit)
	if err != nil {
		s.log.Warn(ctx, "speech backfill selection failed", zap.Error(err))
		return 0
	}
	var calls []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			calls = append(calls, id)
		}
	}
	_ = rows.Close()
	for _, id := range calls {
		if ctx.Err() != nil {
			break
		}
		s.Refresh(ctx, id)
	}
	return len(calls)
}

// RunBackfill keeps the metrics of transcript edits, restores and old calls up
// to date. Measuring is cheap and local, so a slow pace is enough.
func (s *Service) RunBackfill(ctx context.Context, interval time.Duration) <-chan struct{} {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			s.Backfill(ctx, 100)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return done
}

// CallBlock is the «Речь» block of a call page.
type CallBlock struct {
	SpeakerSwitchesPer5Min *float64      `json:"speaker_switches_per_5min"`
	PausesOverThreshold    int           `json:"pauses_over_threshold"`
	LongestPauseSeconds    int           `json:"longest_pause_seconds"`
	Speakers               []SpeakerView `json:"speakers"`
}

type SpeakerView struct {
	SpeakerKey              string   `json:"speaker_key"`
	DisplayName             string   `json:"display_name"`
	IsSubject               bool     `json:"is_subject"`
	TalkSeconds             int      `json:"talk_seconds"`
	TalkShare               float64  `json:"talk_share"`
	Words                   int      `json:"words"`
	WordsPerMinute          *int     `json:"words_per_minute"`
	LongestMonologueSeconds int      `json:"longest_monologue_seconds"`
	Questions               int      `json:"questions"`
	QuestionsPerHour        *float64 `json:"questions_per_hour"`
	ResponsePauseMedianMs   *int     `json:"response_pause_median_ms"`
}

// ForCall reads the block; nil when the call has not been measured.
func (s *Service) ForCall(ctx context.Context, callID uuid.UUID) (*CallBlock, error) {
	var block CallBlock
	err := s.db.QueryRowContext(ctx, `SELECT speaker_switches_per_5min, pauses_over_threshold, longest_pause_seconds FROM call_speech_metrics WHERE call_uuid = $1`, callID).
		Scan(&block.SpeakerSwitchesPer5Min, &block.PausesOverThreshold, &block.LongestPauseSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.speaker_key, COALESCE(a.display_name, ''),
		       EXISTS (SELECT 1 FROM call_subjects cs WHERE cs.call_uuid = m.call_uuid AND cs.speaker_key = m.speaker_key),
		       m.talk_seconds, m.talk_share, m.words, m.words_per_minute, m.longest_monologue_seconds, m.questions, m.questions_per_hour, m.response_pause_median_ms
		FROM call_speaker_speech_metrics m
		LEFT JOIN call_transcription_speaker_assignments a ON a.call_uuid = m.call_uuid AND a.speaker_key = m.speaker_key
		WHERE m.call_uuid = $1
		ORDER BY m.talk_share DESC, m.speaker_key`, callID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	block.Speakers = []SpeakerView{}
	for rows.Next() {
		var v SpeakerView
		if err := rows.Scan(&v.SpeakerKey, &v.DisplayName, &v.IsSubject, &v.TalkSeconds, &v.TalkShare, &v.Words, &v.WordsPerMinute, &v.LongestMonologueSeconds, &v.Questions, &v.QuestionsPerHour, &v.ResponsePauseMedianMs); err != nil {
			return nil, err
		}
		// The API speaks of shares as fractions, like the analytics pages.
		v.TalkShare /= 100
		block.Speakers = append(block.Speakers, v)
	}
	return &block, rows.Err()
}
