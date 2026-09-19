//go:build integration

package teamanalytics

import (
	"context"
	"encoding/json"
	"testing"

	transcriptionRepo "verbatrace/monolit/internal/repository/transcription"
	"verbatrace/monolit/internal/service/speech"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// transcribe gives a call a transcript where the employee (speaker A) holds
// the given share of a two-speaker conversation, one second per word.
func (tm *team) transcribe(call uuid.UUID, employeeWords, clientWords int) {
	type word struct {
		Text    string  `json:"text"`
		Speaker string  `json:"speaker"`
		Start   float64 `json:"start_seconds"`
		End     float64 `json:"end_seconds"`
	}
	var words []word
	at := 0.0
	for i := 0; i < employeeWords; i++ {
		words = append(words, word{Text: "слово", Speaker: "A", Start: at, End: at + 0.9})
		at++
	}
	words[len(words)-1].Text = "вопрос?"
	at += 0.5
	for i := 0; i < clientWords; i++ {
		words = append(words, word{Text: "ответ", Speaker: "B", Start: at, End: at + 0.9})
		at++
	}
	raw, _ := json.Marshal(words)
	tm.exec(`INSERT INTO call_transcriptions (transcription_uuid, call_uuid, status, text, provider, words) VALUES ($1,$2,'transcribed','текст','mock',$3::jsonb)`, uuid.New(), call, string(raw))
	tm.exec(`UPDATE call_subjects SET speaker_key = 'A' WHERE call_uuid = $1`, call)
}

func TestSpeechIsMeasuredAndReadBesideTheTeam(t *testing.T) {
	tm := newTeam(t, "business_plus")
	ctx := context.Background()
	meter := speech.NewService(tm.db, transcriptionRepo.NewRepository(tm.db), nil)

	ivanCalls := []uuid.UUID{tm.call(1, tm.sales, 70, 50, tm.ivan), tm.call(2, tm.sales, 70, 50, tm.ivan)}
	olgaCall := tm.call(1, tm.sales, 80, 100, tm.olga)
	tm.transcribe(ivanCalls[0], 30, 10)
	tm.transcribe(ivanCalls[1], 10, 30)
	tm.transcribe(olgaCall, 20, 20)
	require.Equal(t, 3, meter.Backfill(ctx, 10))
	require.Zero(t, meter.Backfill(ctx, 10), "a measured revision is not measured again")

	block, err := meter.ForCall(ctx, ivanCalls[0])
	require.NoError(t, err)
	require.NotNil(t, block)
	require.Len(t, block.Speakers, 2)
	require.Equal(t, "A", block.Speakers[0].SpeakerKey)
	require.True(t, block.Speakers[0].IsSubject)
	require.InDelta(t, 0.75, block.Speakers[0].TalkShare, 0.01)
	require.Equal(t, 1, block.Speakers[0].Questions)

	employees, err := tm.service.Employees(ctx, tm.req(tm.owner))
	require.NoError(t, err)
	var ivan *EmployeeRow
	for i := range employees.Employees {
		if employees.Employees[i].UserUUID == tm.ivan.String() {
			ivan = &employees.Employees[i]
		}
	}
	require.NotNil(t, ivan)
	sp, ok := ivan.Speech.(Speech)
	require.True(t, ok)
	require.Equal(t, 2, sp.N)
	require.InDelta(t, 0.5, *sp.TalkShare, 0.01, "the mean of 75 % and 25 %")
	require.NotNil(t, employees.Team.Speech)
	require.Equal(t, 3, employees.Team.Speech.N)
	require.InDelta(t, 0.5, *employees.Team.Speech.TalkShare, 0.01, "the median of 75, 25 and 50 %")

	profile, err := tm.service.Profile(ctx, tm.req(tm.ivan), uuid.Nil)
	require.NoError(t, err)
	require.NotNil(t, profile.Speech.Own)
	require.Equal(t, 2, profile.Speech.Own.N)
	require.Nil(t, profile.Speech.TeamMedian, "two people in the department: the median would give a colleague away")
}
