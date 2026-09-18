package teamanalytics

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// Speech is an employee's speech numbers over the calls of a period where they
// were bound to a speaker. Calls where the employee is only the uploader,
// without a speaker, have no speech numbers and do not count.
type Speech struct {
	TalkShare               *float64 `json:"talk_share"`
	LongestMonologueSeconds *int     `json:"longest_monologue_seconds"`
	WordsPerMinute          *int     `json:"words_per_minute"`
	QuestionsPerHour        *float64 `json:"questions_per_hour"`
	ResponsePauseMedianMs   *int     `json:"response_pause_median_ms"`
	N                       int      `json:"n"`
}

// speechRows joins a scope's calls to the metrics of their employees' speakers.
const speechRows = `
	FROM analytics_call_facts f
	JOIN calls c ON c.call_uuid = f.call_uuid
	JOIN call_subjects cs ON cs.call_uuid = f.call_uuid AND cs.speaker_key IS NOT NULL
	JOIN call_speaker_speech_metrics m ON m.call_uuid = f.call_uuid AND m.speaker_key = cs.speaker_key`

// speechByEmployee averages each employee's speech over the period.
func (s *Service) speechByEmployee(ctx context.Context, scope Scope, from, to time.Time) (map[uuid.UUID]Speech, error) {
	q := &query{}
	scope.callsOf(q, from, to)
	rows, err := s.db.QueryContext(ctx, `
		SELECT cs.user_uuid, avg(m.talk_share)::float8, avg(m.longest_monologue_seconds)::float8, avg(m.words_per_minute)::float8,
		       avg(m.questions_per_hour)::float8, avg(m.response_pause_median_ms)::float8, count(*)`+speechRows+`
		WHERE `+q.sql()+` GROUP BY cs.user_uuid`, q.args...)
	if err != nil {
		return nil, fmt.Errorf("read speech by employee: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[uuid.UUID]Speech{}
	for rows.Next() {
		var id uuid.UUID
		var share, monologue, wpm, questions, pause sql.NullFloat64
		var n int
		if err := rows.Scan(&id, &share, &monologue, &wpm, &questions, &pause, &n); err != nil {
			return nil, err
		}
		out[id] = speechOf(share, monologue, wpm, questions, pause, n)
	}
	return out, rows.Err()
}

// speechMedian is the team's median per metric over every employee call of the
// period: what an employee's numbers are shown beside.
func (s *Service) speechMedian(ctx context.Context, scope Scope, from, to time.Time) (*Speech, error) {
	q := &query{}
	scope.callsOf(q, from, to)
	var share, monologue, wpm, questions, pause sql.NullFloat64
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY m.talk_share), percentile_cont(0.5) WITHIN GROUP (ORDER BY m.longest_monologue_seconds),
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY m.words_per_minute), percentile_cont(0.5) WITHIN GROUP (ORDER BY m.questions_per_hour),
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY m.response_pause_median_ms), count(*)`+speechRows+`
		WHERE `+q.sql(), q.args...).Scan(&share, &monologue, &wpm, &questions, &pause, &n)
	if err != nil {
		return nil, fmt.Errorf("read speech median: %w", err)
	}
	if n == 0 {
		return nil, nil
	}
	median := speechOf(share, monologue, wpm, questions, pause, n)
	return &median, nil
}

func speechOf(share, monologue, wpm, questions, pause sql.NullFloat64, n int) Speech {
	out := Speech{N: n}
	if share.Valid {
		// Stored in percent; the API speaks in fractions.
		v := math.Round(share.Float64*10) / 1000
		out.TalkShare = &v
	}
	if monologue.Valid {
		out.LongestMonologueSeconds = intPtr(int(math.Round(monologue.Float64)))
	}
	if wpm.Valid {
		out.WordsPerMinute = intPtr(int(math.Round(wpm.Float64)))
	}
	if questions.Valid {
		v := math.Round(questions.Float64*10) / 10
		out.QuestionsPerHour = &v
	}
	if pause.Valid {
		out.ResponsePauseMedianMs = intPtr(int(math.Round(pause.Float64)))
	}
	return out
}
