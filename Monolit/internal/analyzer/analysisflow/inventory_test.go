package analysisflow

import (
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

func TestMergeQuestionResponsesKeepsStandaloneEpisodesAndCombinesAnswerTurns(t *testing.T) {
	units := []Unit{
		{ID: "intro", Kind: "episode", SegmentIDs: []string{"s0"}, Parts: []string{"Вступление"}},
		{ID: "q", Kind: "question", SegmentIDs: []string{"s1"}, Parts: []string{"Что такое канал?"}},
		{ID: "answer", Kind: "episode", SegmentIDs: []string{"s2"}, Parts: []string{"Ответ"}},
		{ID: "detail", Kind: "episode", SegmentIDs: []string{"s3"}, Parts: []string{"Уточнение ответа"}},
		{ID: "q2", Kind: "question", SegmentIDs: []string{"s4"}, Parts: []string{"Следующий вопрос"}},
	}

	merged := mergeQuestionResponses(units)
	require.Len(t, merged, 3)
	require.Equal(t, "episode", merged[0].Kind)
	require.Equal(t, []string{"s1", "s2", "s3"}, merged[1].SegmentIDs)
	require.Equal(t, []string{"Что такое канал?", "Ответ", "Уточнение ответа"}, merged[1].Parts)
	require.Equal(t, "question", merged[2].Kind)
}
