package processing

import (
	"testing"

	"calllens/monolit/internal/models"
)

func TestSpeakerCandidatesKeepPeopleAndRolesIndependent(t *testing.T) {
	candidates := speakerCandidates(models.Call{
		SpeakerHints:     []models.SpeakerHint{{Name: "Евгения Иванова", Username: "evgenia", Note: "Проводит собеседование"}},
		DiarizationRoles: []models.DiarizationRole{{Name: "Председатель комиссии", Description: "Объявляет решения комиссии"}},
	})
	if len(candidates) != 2 || candidates[0].Label != "Евгения Иванова" || candidates[0].Kind != "person" || candidates[1].Label != "Председатель комиссии" || candidates[1].Kind != "role" {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}
}
