package analysisflow

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeInventoryOwnershipMovesOwnedSegmentFirstAndDropsOverlapOnlyUnits(t *testing.T) {
	inventory := Inventory{Units: []Unit{
		{ID: "kept", Kind: "question", Title: "Вопрос", Topic: "тема", SegmentIDs: []string{"context-before", "owned"}, Parts: []string{"Вопрос"}},
		{ID: "dropped", Kind: "episode", Title: "Повтор", Topic: "тема", SegmentIDs: []string{"context-after"}, Parts: []string{"Повтор"}},
	}}
	inventory.Excluded = append(inventory.Excluded,
		struct {
			SegmentID string `json:"segment_id"`
			Reason    string `json:"reason"`
		}{SegmentID: "context-before", Reason: "контекст"},
	)

	normalizeInventoryOwnership(&inventory, []Segment{{ID: "owned"}})

	require.Len(t, inventory.Units, 1)
	require.Equal(t, []string{"owned", "context-before"}, inventory.Units[0].SegmentIDs)
	require.Empty(t, inventory.Excluded)
}

func TestNormalizeSpeakerMarkersReplacesSegmentIDsWithSpeakers(t *testing.T) {
	item := map[string]any{
		"explanation": "{{speaker:s218.1}} ответил, {{speaker:B}} уточнил, {{speaker:unknown}} не найден.",
		"gaps":        []any{map[string]any{"text": "{{speaker:s219.1}} не назвал срок."}},
	}

	normalizeSpeakerMarkers(item, []Segment{{ID: "s218.1", Speaker: "A"}, {ID: "s219.1", Speaker: "B"}})

	require.Equal(t, "{{speaker:A}} ответил, {{speaker:B}} уточнил, {{speaker:unknown}} не найден.", item["explanation"])
	require.Equal(t, "{{speaker:B}} не назвал срок.", item["gaps"].([]any)[0].(map[string]any)["text"])
}

func TestLayoutUnitsKeepsStandaloneEpisodesAndCombinesAnswerTurns(t *testing.T) {
	windows := [][]Unit{
		{
			{ID: "intro", Kind: "episode", SegmentIDs: []string{"s0"}, Parts: []string{"Вступление"}},
			{ID: "q", Kind: "question", SegmentIDs: []string{"s1"}, Parts: []string{"Что такое канал?"}},
			{ID: "answer", Kind: "episode", SegmentIDs: []string{"s2"}, Parts: []string{"Ответ"}},
		},
		{
			// An answer continuing across the window boundary joins the same card.
			{ID: "detail", Kind: "episode", SegmentIDs: []string{"s3"}, Parts: []string{"Уточнение ответа"}},
			{ID: "q2", Kind: "question", SegmentIDs: []string{"s4"}, Parts: []string{"Следующий вопрос"}},
		},
	}
	index := map[string]Segment{"s1": {ID: "s1", Speaker: "A"}, "s4": {ID: "s4", Speaker: "B"}}

	entries := layoutUnits(windows, []bool{true, true}, index)

	require.Len(t, entries, 3)
	require.Equal(t, "u1.1", entries[0].unit.ID)
	require.Equal(t, "episode", entries[0].unit.Kind)
	require.Equal(t, "u1.2", entries[1].unit.ID)
	require.Equal(t, []string{"s1", "s2", "s3"}, entries[1].unit.SegmentIDs)
	require.Equal(t, []string{"Что такое канал?", "Ответ", "Уточнение ответа"}, entries[1].unit.Parts)
	require.Equal(t, "A", entries[1].unit.QuestionSpeaker)
	require.Equal(t, "u2.2", entries[2].unit.ID)
	require.Equal(t, "B", entries[2].unit.QuestionSpeaker)
	for _, entry := range entries {
		require.True(t, entry.final)
	}
	// Layout never mutates the stored window inventory.
	require.Equal(t, []string{"s1"}, windows[0][1].SegmentIDs)
}

func TestLayoutUnitsDefersUnitsNextToUnfinishedWindows(t *testing.T) {
	question := func(segment string) Unit {
		return Unit{ID: segment, Kind: "question", SegmentIDs: []string{segment}, Parts: []string{segment}}
	}
	episode := func(segment string) Unit {
		return Unit{ID: segment, Kind: "episode", SegmentIDs: []string{segment}, Parts: []string{segment}}
	}
	windows := [][]Unit{
		{question("s0"), question("s1"), episode("s2")},
		nil,
		{episode("s5"), episode("s6"), question("s7")},
	}

	entries := layoutUnits(windows, []bool{true, false, true}, nil)

	require.Len(t, entries, 5)
	require.True(t, entries[0].final, "a question followed by a known question is final")
	require.False(t, entries[1].final, "the question before an unfinished window may absorb its episodes")
	require.Equal(t, []string{"s1", "s2"}, entries[1].unit.SegmentIDs)
	require.False(t, entries[2].final, "an episode after an unfinished window may merge backwards")
	require.True(t, unresolvedEpisode(entries[2]))
	require.True(t, unresolvedEpisode(entries[3]), "so may the episodes that follow it")
	require.True(t, entries[4].final)

	t.Run("finished window with trailing episodes resolves both sides", func(t *testing.T) {
		windows[1] = []Unit{episode("s3")}
		entries := layoutUnits(windows, []bool{true, true, true}, nil)
		require.Len(t, entries, 3)
		require.Equal(t, []string{"s1", "s2", "s3", "s5", "s6"}, entries[1].unit.SegmentIDs)
		require.Equal(t, "u1.2", entries[1].unit.ID)
		require.False(t, slices.ContainsFunc(entries, notFinal))
	})
	t.Run("finished window starting a new question takes the episodes", func(t *testing.T) {
		windows[1] = []Unit{question("s4")}
		entries := layoutUnits(windows, []bool{true, true, true}, nil)
		require.Len(t, entries, 4)
		require.Equal(t, []string{"s1", "s2"}, entries[1].unit.SegmentIDs)
		require.Equal(t, "u2.1", entries[2].unit.ID)
		require.Equal(t, []string{"s4", "s5", "s6"}, entries[2].unit.SegmentIDs)
	})
}
