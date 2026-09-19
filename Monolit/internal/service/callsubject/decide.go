package callsubject

import (
	"sort"
	"strings"
	"unicode"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

// speaker is one voice of the active transcript revision.
type speaker struct {
	Key   string
	Words int
	// Opening is what the speaker said first, where people introduce themselves.
	Opening string
}

type assignment struct {
	DisplayName string
	Role        string
	Contact     uuid.NullUUID
}

type hint struct {
	Name   string
	UserID uuid.UUID
	Role   string
}

type member struct {
	UserID    uuid.UUID
	FirstName string
	LastName  string
	Username  string
}

type decideInput struct {
	Personal    bool
	Uploader    uuid.NullUUID
	Speakers    []speaker
	Assignments map[string]assignment
	Hints       []hint
	Members     map[uuid.UUID]member
}

type subjectRow struct {
	UserID       uuid.UUID
	Source       string
	IsPrimary    bool
	SpeakerKey   *string
	TalkShare    *float64
	Signals      []string
	GrantsAccess bool
	SetBy        uuid.NullUUID
}

type decision struct {
	Subjects []subjectRow
	Internal bool
	Shared   bool
}

// Service roles: only a speaker in one of these is an employee of the call. A
// company member who took part as a client or a partner is not, so a partner
// meeting is not counted against anyone by mistake.
var (
	serviceAssignmentRoles = map[string]bool{"manager": true, "operator": true, "unknown": true}
	serviceHintRoles       = map[string]bool{"self": true, "manager": true}
	strongSignals          = map[string]bool{models.SubjectSignalManualAssignment: true, models.SubjectSignalHintName: true, models.SubjectSignalUploader: true}
)

// decide finds the employees of a call from what people and systems said about
// its speakers and from the transcript itself. No model is asked. Each signal
// is worth one point and a speaker goes to the member with most points; a tie
// binds nobody. A speaker assigned to a person by hand is bound at once.
func decide(in decideInput) decision {
	if in.Personal {
		return personal(in)
	}
	totalWords := 0
	for _, s := range in.Speakers {
		totalWords += s.Words
	}
	type bound struct {
		row   subjectRow
		words int
	}
	byUser := map[uuid.UUID]*bound{}
	var order []uuid.UUID
	boundSpeakers := 0
	for _, s := range in.Speakers {
		userID, signals, ok := bindSpeaker(s, in)
		if !ok {
			continue
		}
		boundSpeakers++
		key := s.Key
		if existing, seen := byUser[userID]; seen {
			// Diarization sometimes splits one person into two voices.
			existing.row.Signals = mergeSignals(existing.row.Signals, signals)
			if s.Words > existing.words {
				existing.row.SpeakerKey = &key
			}
			existing.words += s.Words
			continue
		}
		byUser[userID] = &bound{row: subjectRow{UserID: userID, Source: models.CallSubjectSourceSpeakerMatch, SpeakerKey: &key, Signals: signals}, words: s.Words}
		order = append(order, userID)
	}
	if len(order) == 0 {
		return uploaderOnly(in.Uploader)
	}
	result := decision{Internal: boundSpeakers == len(in.Speakers)}
	primary := order[0]
	for _, id := range order {
		b := byUser[id]
		if b.words > byUser[primary].words {
			primary = id
		}
		if totalWords > 0 {
			share := float64(int(float64(b.words)*10000/float64(totalWords))) / 100
			b.row.TalkShare = &share
		}
		for _, signal := range b.row.Signals {
			b.row.GrantsAccess = b.row.GrantsAccess || strongSignals[signal]
		}
	}
	for _, id := range order {
		b := byUser[id]
		b.row.IsPrimary = id == primary
		result.Subjects = append(result.Subjects, b.row)
	}
	result.Shared = len(result.Subjects) > 1 && !result.Internal
	return result
}

// personal is the owner of a personal call, bound to their speaker when the
// owner marked it, named it at upload or introduced themselves. It is never
// shared or internal: there is no company.
func personal(in decideInput) decision {
	result := uploaderOnly(in.Uploader)
	if !in.Uploader.Valid || len(result.Subjects) == 0 {
		return result
	}
	owner := &result.Subjects[0]
	bestWords := -1
	totalWords := 0
	for _, s := range in.Speakers {
		totalWords += s.Words
	}
	for _, s := range in.Speakers {
		userID, signals, ok := bindSpeaker(s, in)
		if !ok || userID != in.Uploader.UUID {
			continue
		}
		owner.Signals = mergeSignals(owner.Signals, signals)
		// Diarization sometimes splits one person into two voices.
		if s.Words > bestWords {
			key := s.Key
			owner.SpeakerKey, bestWords = &key, s.Words
			owner.Source = models.CallSubjectSourceSpeakerMatch
		}
	}
	if owner.SpeakerKey != nil && totalWords > 0 {
		share := float64(int(float64(bestWords)*10000/float64(totalWords))) / 100
		owner.TalkShare = &share
	}
	return result
}

func uploaderOnly(uploader uuid.NullUUID) decision {
	if !uploader.Valid {
		return decision{}
	}
	return decision{Subjects: []subjectRow{{UserID: uploader.UUID, Source: models.CallSubjectSourceUploader, IsPrimary: true}}}
}

// bindSpeaker returns the employee a speaker is, with the signals that said so.
func bindSpeaker(s speaker, in decideInput) (uuid.UUID, []string, bool) {
	assigned, hasAssignment := in.Assignments[s.Key]
	name := s.Key
	if hasAssignment && strings.TrimSpace(assigned.DisplayName) != "" {
		name = assigned.DisplayName
	}
	normalizedName := normalizeName(name)

	var matchedHint *hint
	for i := range in.Hints {
		if normalizedName != "" && normalizeName(in.Hints[i].Name) == normalizedName {
			matchedHint = &in.Hints[i]
			break
		}
	}
	switch {
	case hasAssignment:
		if !serviceAssignmentRoles[assigned.Role] {
			return uuid.Nil, nil, false
		}
	case matchedHint != nil:
		if !serviceHintRoles[matchedHint.Role] {
			return uuid.Nil, nil, false
		}
	}

	if hasAssignment && assigned.Contact.Valid {
		if _, isMember := in.Members[assigned.Contact.UUID]; isMember {
			signals := []string{models.SubjectSignalManualAssignment}
			if in.Uploader.Valid && in.Uploader.UUID == assigned.Contact.UUID {
				signals = append(signals, models.SubjectSignalUploader)
			}
			return assigned.Contact.UUID, signals, true
		}
	}

	scores := map[uuid.UUID][]string{}
	add := func(id uuid.UUID, signal string) {
		if _, isMember := in.Members[id]; isMember {
			scores[id] = append(scores[id], signal)
		}
	}
	if matchedHint != nil {
		add(matchedHint.UserID, models.SubjectSignalHintName)
	}
	opening := words(s.Opening)
	for id, m := range in.Members {
		if normalizedName != "" && memberNamed(m, normalizedName) {
			add(id, models.SubjectSignalMemberName)
		}
		if introduces(opening, m) {
			add(id, models.SubjectSignalSelfIntroduction)
		}
	}
	// Uploading the call counts only for a candidate something else already
	// points to: on its own it would bind every speaker to the uploader.
	if in.Uploader.Valid {
		if signals, ok := scores[in.Uploader.UUID]; ok && len(signals) > 0 {
			scores[in.Uploader.UUID] = append(signals, models.SubjectSignalUploader)
		}
	}
	best, bestScore, tie := uuid.Nil, 0, false
	for id, signals := range scores {
		switch {
		case len(signals) > bestScore:
			best, bestScore, tie = id, len(signals), false
		case len(signals) == bestScore:
			tie = true
		}
	}
	if bestScore == 0 || tie {
		return uuid.Nil, nil, false
	}
	signals := scores[best]
	sort.Strings(signals)
	return best, signals, true
}

func memberNamed(m member, name string) bool {
	first, last := normalizeName(m.FirstName), normalizeName(m.LastName)
	for _, form := range []string{strings.TrimSpace(first + " " + last), strings.TrimSpace(last + " " + first), normalizeName(m.Username)} {
		if form != "" && form == name {
			return true
		}
	}
	// A first name alone names a person only when it is all the label says.
	return first != "" && name == first
}

// introduces looks for "меня зовут Иван", "это Иван" or "я Иван" at the start of
// what a speaker said.
func introduces(opening []string, m member) bool {
	first := normalizeName(m.FirstName)
	if len([]rune(first)) < 3 {
		return false
	}
	for i := range opening {
		if opening[i] != first {
			continue
		}
		if i >= 2 && opening[i-2] == "меня" && opening[i-1] == "зовут" {
			return true
		}
		if i >= 1 && (opening[i-1] == "это" || opening[i-1] == "я") {
			return true
		}
	}
	return false
}

func mergeSignals(a, b []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, signal := range append(append([]string{}, a...), b...) {
		if !seen[signal] {
			seen[signal] = true
			result = append(result, signal)
		}
	}
	sort.Strings(result)
	return result
}

func normalizeName(value string) string {
	return strings.Join(words(value), " ")
}

func words(value string) []string {
	value = strings.ReplaceAll(strings.ToLower(value), "ё", "е")
	return strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
