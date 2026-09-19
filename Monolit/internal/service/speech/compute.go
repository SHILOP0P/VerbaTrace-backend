// Package speech measures how a conversation went from its word timings: who
// spoke how much, how long the monologues ran, the pace, questions, pauses and
// how often the speaker changed. No model is called and no credit spent. The
// definitions follow published ones (spec, section 7) and are shown as
// observations beside the team median, never as a grade.
package speech

import (
	"math"
	"sort"
	"strings"

	"verbatrace/monolit/internal/models"
)

const (
	// A turn is one speaker's words in a row; a gap of three seconds or more
	// starts a new one (AWS Post Call Analytics).
	turnGapSeconds = 3.0
	// A long pause is a gap between turns of four seconds or more (UIS).
	longPauseSeconds = 4.0
	// Turns shorter than three words ("да", "угу") are not a change of speaker
	// for interactivity (Gong).
	backchannelWords = 3
	// maxRate fits NUMERIC(5,1).
	maxRate = 9999.9
)

type turn struct {
	speaker    string
	start, end float64
	words      int
	questions  int
}

// Call is the whole conversation's numbers.
type Call struct {
	SpeakerSwitchesPer5Min *float64
	PausesOverThreshold    int
	LongestPauseSeconds    int
}

// Speaker is one speaker's numbers.
type Speaker struct {
	Key                     string
	TalkSeconds             int
	TalkShare               float64 // percent, 0–100
	Words                   int
	WordsPerMinute          *int
	LongestMonologueSeconds int
	Questions               int
	QuestionsPerHour        *float64
	ResponsePauseMedianMs   *int
}

// Compute measures a transcript. Words without a speaker are left out: they
// cannot be attributed.
func Compute(words []models.TranscriptionWord) (Call, []Speaker) {
	turns := turnsOf(words)
	if len(turns) == 0 {
		return Call{}, nil
	}
	duration := turns[len(turns)-1].end - turns[0].start
	var call Call
	var total float64
	bySpeaker := map[string]*Speaker{}
	pauses := map[string][]float64{}
	var order []string
	for i, t := range turns {
		length := t.end - t.start
		total += length
		s, ok := bySpeaker[t.speaker]
		if !ok {
			s = &Speaker{Key: t.speaker}
			bySpeaker[t.speaker] = s
			order = append(order, t.speaker)
		}
		s.Words += t.words
		s.Questions += t.questions
		if seconds := int(math.Round(length)); seconds > s.LongestMonologueSeconds {
			s.LongestMonologueSeconds = seconds
		}
		if i == 0 {
			continue
		}
		gap := t.start - turns[i-1].end
		if gap >= longPauseSeconds {
			call.PausesOverThreshold++
		}
		if seconds := int(math.Round(math.Max(gap, 0))); seconds > call.LongestPauseSeconds {
			call.LongestPauseSeconds = seconds
		}
		// Patience: how long this speaker waited after the other one finished.
		if turns[i-1].speaker != t.speaker {
			pauses[t.speaker] = append(pauses[t.speaker], math.Max(gap, 0))
		}
	}
	talk := map[string]float64{}
	for _, t := range turns {
		talk[t.speaker] += t.end - t.start
	}
	speakers := make([]Speaker, 0, len(order))
	for _, key := range order {
		s := bySpeaker[key]
		s.TalkSeconds = int(math.Round(talk[key]))
		if total > 0 {
			s.TalkShare = math.Round(talk[key]/total*10000) / 100
		}
		if talk[key] > 0 {
			wpm := int(math.Round(float64(s.Words) / (talk[key] / 60)))
			s.WordsPerMinute = &wpm
		}
		if duration > 0 {
			// Rates of a few seconds of talk can be absurd; the cap keeps them in the column.
			perHour := math.Min(math.Round(float64(s.Questions)/(duration/3600)*10)/10, maxRate)
			s.QuestionsPerHour = &perHour
		}
		if median, ok := medianMs(pauses[key]); ok {
			s.ResponsePauseMedianMs = &median
		}
		speakers = append(speakers, *s)
	}
	if duration > 0 {
		switches := 0
		last := ""
		for _, t := range turns {
			if t.words < backchannelWords {
				continue
			}
			if last != "" && t.speaker != last {
				switches++
			}
			last = t.speaker
		}
		per5 := math.Min(math.Round(float64(switches)/(duration/300)*10)/10, maxRate)
		call.SpeakerSwitchesPer5Min = &per5
	}
	return call, speakers
}

func turnsOf(words []models.TranscriptionWord) []turn {
	sorted := make([]models.TranscriptionWord, 0, len(words))
	for _, w := range words {
		if strings.TrimSpace(w.Speaker) != "" && w.EndSeconds >= w.StartSeconds {
			sorted = append(sorted, w)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].StartSeconds < sorted[j].StartSeconds })
	var turns []turn
	for _, w := range sorted {
		question := 0
		if strings.Contains(w.Text, "?") {
			question = 1
		}
		if n := len(turns); n > 0 && turns[n-1].speaker == w.Speaker && w.StartSeconds-turns[n-1].end < turnGapSeconds {
			turns[n-1].end = math.Max(turns[n-1].end, w.EndSeconds)
			turns[n-1].words++
			turns[n-1].questions += question
			continue
		}
		turns = append(turns, turn{speaker: w.Speaker, start: w.StartSeconds, end: w.EndSeconds, words: 1, questions: question})
	}
	return turns
}

func medianMs(values []float64) (int, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	median := sorted[middle]
	if len(sorted)%2 == 0 {
		median = (sorted[middle-1] + sorted[middle]) / 2
	}
	return int(math.Round(median * 1000)), true
}
