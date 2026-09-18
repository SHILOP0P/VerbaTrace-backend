package callsubject

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"verbatrace/monolit/internal/models"
)

func TestDecideFindsTheEmployeesOfACall(t *testing.T) {
	ivan, olga, petr, namesake := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	members := map[uuid.UUID]member{
		ivan:     {UserID: ivan, FirstName: "Иван", LastName: "Петров", Username: "ivan"},
		olga:     {UserID: olga, FirstName: "Ольга", LastName: "Смирнова"},
		petr:     {UserID: petr, FirstName: "Пётр", LastName: "Сидоров"},
		namesake: {UserID: namesake, FirstName: "Анна", LastName: "Котова"},
	}
	uploader := uuid.NullUUID{UUID: ivan, Valid: true}
	twoVoices := []speaker{{Key: "A", Words: 300}, {Key: "B", Words: 100}}
	contact := func(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }

	cases := []struct {
		name     string
		in       decideInput
		subjects map[uuid.UUID]bool // user → grants access
		primary  uuid.UUID
		internal bool
		shared   bool
	}{
		{
			name:     "a personal call is the uploader's",
			in:       decideInput{Personal: true, Uploader: uploader, Speakers: twoVoices},
			subjects: map[uuid.UUID]bool{ivan: false}, primary: ivan,
		},
		{
			name:     "nothing known about the speakers falls back to the uploader",
			in:       decideInput{Uploader: uploader, Speakers: twoVoices, Members: members},
			subjects: map[uuid.UUID]bool{ivan: false}, primary: ivan,
		},
		{
			name: "a speaker assigned to an employee by hand is bound and may see the call",
			in: decideInput{Uploader: uploader, Speakers: twoVoices, Members: members, Assignments: map[string]assignment{
				"A": {Role: "manager", Contact: contact(olga)}, "B": {Role: "client"},
			}},
			subjects: map[uuid.UUID]bool{olga: true}, primary: olga,
		},
		{
			name: "a member who took part as a partner is not an employee of the call",
			in: decideInput{Uploader: uploader, Speakers: twoVoices, Members: members, Assignments: map[string]assignment{
				"A": {Role: "manager", Contact: contact(ivan)}, "B": {Role: "partner", Contact: contact(olga)},
			}},
			subjects: map[uuid.UUID]bool{ivan: true}, primary: ivan,
		},
		{
			name: "a name read from the label counts for statistics but gives no access",
			in: decideInput{Uploader: uploader, Speakers: twoVoices, Members: members, Assignments: map[string]assignment{
				"A": {DisplayName: "Анна Котова", Role: "manager"}, "B": {Role: "client"},
			}},
			subjects: map[uuid.UUID]bool{namesake: false}, primary: namesake,
		},
		{
			name: "two members with the same first name are a tie and bind nobody",
			in: decideInput{Uploader: uuid.NullUUID{UUID: olga, Valid: true}, Speakers: twoVoices, Members: map[uuid.UUID]member{
				ivan: members[ivan], olga: members[olga], petr: {UserID: petr, FirstName: "Иван", LastName: "Сидоров"},
			}, Assignments: map[string]assignment{"A": {DisplayName: "Иван", Role: "manager"}}},
			subjects: map[uuid.UUID]bool{olga: false}, primary: olga,
		},
		{
			name: "the uploader wins a name shared with a namesake",
			in: decideInput{Uploader: uploader, Speakers: twoVoices, Members: map[uuid.UUID]member{
				ivan: members[ivan], petr: {UserID: petr, FirstName: "Иван", LastName: "Петров"},
			}, Assignments: map[string]assignment{"A": {DisplayName: "Иван Петров", Role: "manager"}}},
			subjects: map[uuid.UUID]bool{ivan: true}, primary: ivan,
		},
		{
			name: "a participant listed before transcription is bound by name",
			in: decideInput{Uploader: uploader, Speakers: []speaker{{Key: "Ольга Смирнова", Words: 50}, {Key: "Клиент", Words: 40}}, Members: members,
				Hints: []hint{{Name: "Ольга Смирнова", UserID: olga, Role: "manager"}, {Name: "Клиент", UserID: petr, Role: "client"}}},
			subjects: map[uuid.UUID]bool{olga: true}, primary: olga,
		},
		{
			name: "an introduction at the start of a turn names the speaker",
			in: decideInput{Uploader: uploader, Members: members, Speakers: []speaker{
				{Key: "A", Words: 80, Opening: "Добрый день, меня зовут Ольга, компания Ромашка."}, {Key: "B", Words: 60, Opening: "Здравствуйте."},
			}},
			subjects: map[uuid.UUID]bool{olga: false}, primary: olga,
		},
		{
			name: "every speaker an employee makes the call internal, not shared",
			in: decideInput{Uploader: uploader, Speakers: twoVoices, Members: members, Assignments: map[string]assignment{
				"A": {Role: "manager", Contact: contact(ivan)}, "B": {Role: "unknown", Contact: contact(petr)},
			}},
			subjects: map[uuid.UUID]bool{ivan: true, petr: true}, primary: ivan, internal: true,
		},
		{
			name: "two employees and a client make the call shared; the one who spoke most is primary",
			in: decideInput{Uploader: uploader, Speakers: []speaker{{Key: "A", Words: 50}, {Key: "B", Words: 200}, {Key: "C", Words: 90}}, Members: members, Assignments: map[string]assignment{
				"A": {Role: "manager", Contact: contact(ivan)}, "B": {Role: "operator", Contact: contact(olga)}, "C": {Role: "client"},
			}},
			subjects: map[uuid.UUID]bool{ivan: true, olga: true}, primary: olga, shared: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decide(tc.in)
			subjects := map[uuid.UUID]bool{}
			var primary uuid.UUID
			for _, row := range got.Subjects {
				subjects[row.UserID] = row.GrantsAccess
				if row.IsPrimary {
					require.Equal(t, uuid.Nil, primary, "one primary")
					primary = row.UserID
				}
			}
			require.Equal(t, tc.subjects, subjects)
			require.Equal(t, tc.primary, primary)
			require.Equal(t, tc.internal, got.Internal)
			require.Equal(t, tc.shared, got.Shared)
		})
	}
}

func TestDecideMergesOnePersonSplitIntoTwoVoices(t *testing.T) {
	ivan := uuid.New()
	got := decide(decideInput{
		Uploader: uuid.NullUUID{UUID: ivan, Valid: true},
		Speakers: []speaker{{Key: "A", Words: 30}, {Key: "B", Words: 70}, {Key: "C", Words: 100}},
		Members:  map[uuid.UUID]member{ivan: {UserID: ivan, FirstName: "Иван"}},
		Assignments: map[string]assignment{
			"A": {Role: "manager", Contact: uuid.NullUUID{UUID: ivan, Valid: true}},
			"B": {Role: "manager", Contact: uuid.NullUUID{UUID: ivan, Valid: true}},
			"C": {Role: "client"},
		},
	})
	require.Len(t, got.Subjects, 1)
	require.Equal(t, "B", *got.Subjects[0].SpeakerKey)
	require.InDelta(t, 50.0, *got.Subjects[0].TalkShare, 0.01)
	require.Equal(t, []string{models.SubjectSignalManualAssignment, models.SubjectSignalUploader}, got.Subjects[0].Signals)
}
