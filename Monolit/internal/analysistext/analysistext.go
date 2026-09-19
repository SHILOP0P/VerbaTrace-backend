// Package analysistext turns the free text of an analysis into text for a
// reader.
//
// The analysis model cites cards (u1.7), requirements (r3), recommendations
// (rec2) and transcript segments (s4.1) by the IDs the pipeline gave them, and
// now and then writes a field name of its schema (assigned_unit) into prose.
// Both are keys for the server and mean nothing to a person: a bracketed list
// of IDs goes, a single ID becomes the title it stands for, a segment ID goes
// with the words that pointed at it, and a field name becomes a word. The rules
// mirror the frontend helper src/shared/lib/analysis-refs.ts, so a text reads
// the same in the app, in exports and in the CRM.
package analysistext

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ws is the whitespace of JavaScript's \s: the frontend helper decides what
// counts as a gap, and Go's \s is ASCII only.
const ws = `[\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}]`

type replacement struct {
	pattern *regexp.Regexp
	with    string
}

var (
	speakerMarker = regexp.MustCompile(`(?i)\{\{speaker:([^{}]*)\}\}`)

	// A field name after a noun that owns it reads as «части пункта».
	schemaPhrase = regexp.MustCompile(`(?i)(части|частям|частях|частями|частей|элементы|элементам|элементах|элементов|содержания|содержание|содержанию|требования|требованиям|требованиях|требований|условия|условиям|условий|формулировки|формулировке|формулировкам|текст|текста|тексту|тексте)` + ws + `+assigned_units?`)
	// After a preposition the word takes the case the preposition asks for,
	// otherwise «в assigned_unit» reads «в пункт». Order matters: the plural
	// before the singular it starts with.
	schemaAfterPreposition = []replacement{
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(в|во|на|о|об)` + ws + `+assigned_units`), "${1}${2} пунктах"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(в|во|на|о|об)` + ws + `+assigned_unit`), "${1}${2} пункте"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(из|для|до|от|у|без|кроме)` + ws + `+assigned_units`), "${1}${2} пунктов"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(из|для|до|от|у|без|кроме)` + ws + `+assigned_unit`), "${1}${2} пункта"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(по|к|ко)` + ws + `+assigned_units`), "${1}${2} пунктам"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(по|к|ко)` + ws + `+assigned_unit`), "${1}${2} пункту"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(по|к|ко)` + ws + `+source_segments`), "${1}${2} репликам"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(из|для|до|от|у|без|кроме)` + ws + `+source_segments`), "${1}${2} реплик"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(в|во|на|о|об)` + ws + `+source_segments`), "${1}${2} репликах"},
		{regexp.MustCompile(`(?i)(^|` + ws + `|\()(в|во|на|о|об)` + ws + `+source_segment`), "${1}${2} реплике"},
	}
	// The model also transliterates the field: «вопрос юнита» is «вопрос пункта».
	transliteratedUnit = regexp.MustCompile(`(?i)юнит(ами|ам|ах|ов|ом|а|у|е|ы)?`)
	schemaTerms        = []replacement{
		{regexp.MustCompile(`(?i)assigned_units`), "пункты"},
		{regexp.MustCompile(`(?i)assigned_unit`), "пункт"},
		{regexp.MustCompile(`(?i)source_segments?`), "реплики"},
	}
	// Any other snake_case key is a field name, never a word. Case matters: the
	// frontend pattern has no i flag.
	snakeCaseKey = regexp.MustCompile(`[a-z]+(?:_[a-z0-9]+)+`)

	tidyUp = []replacement{
		{regexp.MustCompile(`[(\[]` + ws + `*[,;]?` + ws + `*[)\]]`), ""},
		{regexp.MustCompile(`([(\[])` + ws + `*[,;]` + ws + `*`), "${1}"},
		{regexp.MustCompile(ws + `*[,;]` + ws + `*([)\]])`), "${1}"},
		{regexp.MustCompile(`,` + ws + `*,`), ","},
		{regexp.MustCompile(`[ \t]+([,.;:!?)\]])`), "${1}"},
		{regexp.MustCompile(`[ \t]{2,}`), " "},
	}
)

// StripReferenceIDs makes model text readable. A bracketed list made only of
// IDs goes together with the whitespace before it. A single ID becomes «Title»
// when titles has it, keeping a label word before it, and goes with its label
// otherwise. Speaker markers are left intact for the reader's side to name,
// although the frontend helper does not guard them: the server stores text that
// still has to resolve, and a key like speaker_0 looks like a field name.
func StripReferenceIDs(text string, titles map[string]string) string {
	if text == "" {
		return text
	}
	// No rule matches a brace or treats one as part of a word, so cleaning the
	// text between markers piece by piece is the same as cleaning it whole.
	var b strings.Builder
	last := 0
	for _, marker := range speakerMarker.FindAllStringIndex(text, -1) {
		b.WriteString(stripPiece(text[last:marker[0]], titles))
		b.WriteString(text[marker[0]:marker[1]])
		last = marker[1]
	}
	b.WriteString(stripPiece(text[last:], titles))
	cleaned := b.String()
	for _, rule := range tidyUp {
		cleaned = rule.pattern.ReplaceAllString(cleaned, rule.with)
	}
	return strings.TrimFunc(cleaned, isSpace)
}

// ResultTitles maps the IDs of the cards and recommendations of an analysis
// result to readable titles, for StripReferenceIDs to name what a text cites.
// A result without them, or one that does not parse, names nothing.
func ResultTitles(result []byte) map[string]string {
	type titled struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		CriterionKey string `json:"criterion_key"`
	}
	var parsed struct {
		Items           []titled `json:"items"`
		Recommendations []titled `json:"recommendations"`
	}
	// A field of an unexpected type skips that field only; the rest still names.
	_ = json.Unmarshal(result, &parsed)
	titles := make(map[string]string, len(parsed.Items)+len(parsed.Recommendations))
	for _, entry := range append(parsed.Items, parsed.Recommendations...) {
		title := strings.TrimSpace(entry.Title)
		// A scorecard criterion is titled by the scorecard's author and may
		// legitimately read like an ID («Тариф S1»).
		if strings.TrimSpace(entry.CriterionKey) == "" {
			title = StripReferenceIDs(title, nil)
		}
		if id := strings.TrimSpace(entry.ID); id != "" && title != "" {
			titles[id] = title
		}
	}
	return titles
}

// ResolveSpeakerMarkers replaces {{speaker:KEY}} with the speaker's name, or
// «Спикер KEY» when the name is not known.
func ResolveSpeakerMarkers(text string, names map[string]string) string {
	return ResolveSpeakerMarkersFunc(text, func(key string) string {
		if name := strings.TrimSpace(names[key]); name != "" {
			return name
		}
		return strings.TrimSpace("Спикер " + key)
	})
}

// ResolveSpeakerMarkersFunc replaces every {{speaker:KEY}} with name(KEY), for
// callers that name an unknown speaker their own way.
func ResolveSpeakerMarkersFunc(text string, name func(key string) string) string {
	return speakerMarker.ReplaceAllStringFunc(text, func(marker string) string {
		return name(strings.TrimSpace(speakerMarker.FindStringSubmatch(marker)[1]))
	})
}

func stripPiece(piece string, titles map[string]string) string {
	if piece == "" {
		return piece
	}
	piece = replaceMatches(piece, schemaPhrase, func(text string, match []int) (string, bool) {
		return text[match[2]:match[3]] + " пункта", !isWordByte(byteAt(text, match[1]))
	})
	for _, term := range schemaAfterPreposition {
		piece = replaceMatches(piece, term.pattern, func(text string, match []int) (string, bool) {
			after := byteAt(text, match[1])
			if isWordByte(after) || strings.ContainsRune("@/-", rune(after)) {
				return "", false
			}
			return term.pattern.ReplaceAllString(text[match[0]:match[1]], term.with), true
		})
	}
	piece = replaceMatches(piece, transliteratedUnit, func(text string, match []int) (string, bool) {
		before, _ := utf8.DecodeLastRuneInString(text[:match[0]])
		after, _ := utf8.DecodeRuneInString(text[match[1]:])
		suffix := ""
		if match[2] >= 0 {
			suffix = text[match[2]:match[3]]
		}
		return "пункт" + suffix, !unicode.IsLetter(before) && !unicode.IsLetter(after)
	})
	for _, term := range schemaTerms {
		piece = replaceMatches(piece, term.pattern, func(text string, match []int) (string, bool) {
			return term.with, standaloneKey(text, match)
		})
	}
	piece = replaceMatches(piece, snakeCaseKey, func(text string, match []int) (string, bool) {
		next := byteAt(text, match[1])
		fileExtension := next == '.' && isLowerASCII(byteAt(text, match[1]+1))
		return "", standaloneKey(text, match) && !fileExtension
	})
	r := removeBracketedLists([]rune(piece))
	r = removeSegmentReferences(r)
	return string(replaceLabelledRefs(r, titles))
}

// replaceMatches replaces the matches that decide accepts. RE2 has no
// lookaround, so the edges a lookaround checked are checked here; for these
// patterns a rejected match leaves no other match starting inside it.
func replaceMatches(text string, pattern *regexp.Regexp, decide func(text string, match []int) (string, bool)) string {
	matches := pattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, match := range matches {
		with, ok := decide(text, match)
		if !ok {
			continue
		}
		b.WriteString(text[last:match[0]])
		b.WriteString(with)
		last = match[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

// standaloneKey is a key not glued to a word, an e-mail or a path: nothing of
// [\w.@/-] before it and nothing of [\w@/-] after it.
func standaloneKey(text string, match []int) bool {
	before, after := byteBefore(text, match[0]), byteAt(text, match[1])
	return !isWordByte(before) && !strings.ContainsRune(".@/-", rune(before)) &&
		!isWordByte(after) && !strings.ContainsRune("@/-", rune(after))
}

// removeBracketedLists drops «(u1.2, r3)», «[см. s4.1]» and the like, parsed by
// hand with the frontend's edge rules.
func removeBracketedLists(r []rune) []rune {
	out := make([]rune, 0, len(r))
	for i := 0; i < len(r); {
		if end := bracketedList(r, i); end > 0 {
			i = end
			continue
		}
		out = append(out, r[i])
		i++
	}
	return out
}

// bracketedList returns the end of a list of IDs that starts at i, whitespace
// before the bracket included, or -1.
func bracketedList(r []rune, i int) int {
	open := skipSpaces(r, i)
	if open >= len(r) || (r[open] != '(' && r[open] != '[') {
		return -1
	}
	end := listItem(r, skipSpaces(r, open+1))
	if end < 0 {
		return -1
	}
	for {
		separator := skipSpaces(r, end)
		if separator >= len(r) || !isListSeparator(r[separator]) {
			break
		}
		next := listItem(r, skipSpaces(r, separator+1))
		if next < 0 {
			break
		}
		end = next
	}
	closing := skipSpaces(r, end)
	if closing < len(r) && (r[closing] == ')' || r[closing] == ']') {
		return closing + 1
	}
	return -1
}

// listItem is one entry of a list: an ID, maybe after a label word.
func listItem(r []rune, i int) int {
	for _, label := range labelEnds(r, i) {
		if end := refAt(r, skipSpaces(r, label)); end >= 0 {
			return end
		}
	}
	return refAt(r, i)
}

func isListSeparator(c rune) bool {
	return c == ',' || c == ';' || c == '/' || sameLetter(c, 'и')
}

// removeSegmentReferences drops a segment ID with the words that only pointed
// at it, since it has no title to stand in for it: «присутствуют в сегменте
// s3.1» becomes «присутствуют».
func removeSegmentReferences(r []rune) []rune {
	out := make([]rune, 0, len(r))
	for i := 0; i < len(r); {
		if end := segmentReference(r, i); end > 0 {
			i = end
			continue
		}
		out = append(out, r[i])
		i++
	}
	return out
}

var (
	pointerPrepositions = []string{"в", "во", "на", "из", "по", "к", "о", "об"}
	pointerNouns        = []string{"сегмент", "реплик", "фрагмент"}
)

// segmentReference matches (\s+PREPOSITION)?(\s+NOUN\p{L}*)?\s+sN(.N)? at i.
func segmentReference(r []rune, i int) int {
	if i >= len(r) || !isSpace(r[i]) {
		return -1
	}
	word := skipSpaces(r, i)
	for _, preposition := range pointerPrepositions {
		if end := foldPrefixEnd(r, word, preposition); end >= 0 {
			if refEnd := segmentNounAndID(r, end); refEnd > 0 {
				return refEnd
			}
		}
	}
	return segmentNounAndID(r, i)
}

func segmentNounAndID(r []rune, i int) int {
	if i >= len(r) || !isSpace(r[i]) {
		return -1
	}
	word := skipSpaces(r, i)
	for _, noun := range pointerNouns {
		if end := foldPrefixEnd(r, word, noun); end >= 0 {
			for end < len(r) && unicode.IsLetter(r[end]) {
				end++
			}
			if refEnd := spacedSegmentID(r, end); refEnd > 0 {
				return refEnd
			}
		}
	}
	return spacedSegmentID(r, i)
}

// spacedSegmentID matches \s+sN(.N)? at i.
func spacedSegmentID(r []rune, i int) int {
	if i >= len(r) || !isSpace(r[i]) {
		return -1
	}
	start := skipSpaces(r, i)
	if start >= len(r) || !sameLetter(r[start], 's') || !edgeBefore(r, start) {
		return -1
	}
	for _, end := range refEnds(r, start) {
		if edgeAfter(r, end) {
			return end
		}
	}
	return -1
}

// replaceLabelledRefs names or drops every ID left outside lists.
func replaceLabelledRefs(r []rune, titles map[string]string) []rune {
	out := make([]rune, 0, len(r))
	for i := 0; i < len(r); {
		if label, ref, end := labelledRef(r, i); end > 0 {
			out = append(out, []rune(refReplacement(label, ref, titles))...)
			i = end
			continue
		}
		out = append(out, r[i])
		i++
	}
	return out
}

// labelledRef matches «рекомендация rec2» or a bare «rec2» at i.
func labelledRef(r []rune, i int) (label, ref string, end int) {
	for _, labelEnd := range labelEnds(r, i) {
		if labelEnd >= len(r) || !isSpace(r[labelEnd]) {
			continue
		}
		start := skipSpaces(r, labelEnd)
		if refEnd := refAt(r, start); refEnd >= 0 {
			return string(r[i:labelEnd]), string(r[start:refEnd]), refEnd
		}
	}
	if refEnd := refAt(r, i); refEnd >= 0 {
		return "", string(r[i:refEnd]), refEnd
	}
	return "", "", -1
}

func refReplacement(label, ref string, titles map[string]string) string {
	title := strings.TrimSpace(titles[ref])
	if title == "" {
		title = strings.TrimSpace(titles[strings.ToLower(ref)])
	}
	switch {
	case title == "":
		return ""
	case label == "":
		return "«" + title + "»"
	default:
		return label + " «" + title + "»"
	}
}

// refAt returns the end of an ID at i: rec\d+, [ur]\d+(\.\d+)? or s\d+(\.\d+)?,
// not glued to a letter, digit or dot before it and not followed by a letter or
// digit. Such letters inside «Pro24», «u2f» or «2.u1» are no reference.
func refAt(r []rune, i int) int {
	if i >= len(r) || !edgeBefore(r, i) {
		return -1
	}
	for _, end := range refEnds(r, i) {
		if edgeAfter(r, end) {
			return end
		}
	}
	return -1
}

// refEnds lists where an ID at i may end, longest first, the way a
// backtracking regular expression tries them.
func refEnds(r []rune, i int) []int {
	switch {
	case sameLetter(r[i], 'r') && foldPrefixEnd(r, i+1, "ec") >= 0 && isDigit(at(r, i+3)):
		return []int{digitsEnd(r, i+3)}
	case (sameLetter(r[i], 'u') || sameLetter(r[i], 'r') || sameLetter(r[i], 's')) && isDigit(at(r, i+1)):
		whole := digitsEnd(r, i+1)
		if at(r, whole) == '.' && isDigit(at(r, whole+1)) {
			return []int{digitsEnd(r, whole+1), whole}
		}
		return []int{whole}
	default:
		return nil
	}
}

// labelEnds lists where a label word at i may end, in the frontend's order:
// см. / см, рекомендаци-, карточк-, критери-, требовани-, пункт(-).
func labelEnds(r []rune, i int) []int {
	var ends []int
	if end := foldPrefixEnd(r, i, "см"); end >= 0 {
		if at(r, end) == '.' {
			ends = append(ends, end+1)
		}
		ends = append(ends, end)
	}
	for _, word := range []struct{ stem, endings string }{
		{"рекомендаци", "яиюей"}, {"карточк", "аиуеой"}, {"критери", "йияюем"}, {"требовани", "еяюйм"},
	} {
		if end := foldPrefixEnd(r, i, word.stem); end >= 0 && end < len(r) && foldContains(word.endings, r[end]) {
			ends = append(ends, end+1)
		}
	}
	if end := foldPrefixEnd(r, i, "пункт"); end >= 0 {
		if end < len(r) && foldContains("аыеом", r[end]) {
			ends = append(ends, end+1)
		}
		ends = append(ends, end)
	}
	return ends
}

func edgeBefore(r []rune, i int) bool {
	if i == 0 {
		return true
	}
	previous := r[i-1]
	return previous != '.' && !unicode.IsLetter(previous) && !unicode.IsNumber(previous)
}

func edgeAfter(r []rune, end int) bool {
	return end >= len(r) || (!unicode.IsLetter(r[end]) && !unicode.IsNumber(r[end]))
}

func skipSpaces(r []rune, i int) int {
	for i < len(r) && isSpace(r[i]) {
		i++
	}
	return i
}

func digitsEnd(r []rune, i int) int {
	for i < len(r) && isDigit(r[i]) {
		i++
	}
	return i
}

// at is r[i], or zero past the end.
func at(r []rune, i int) rune {
	if i < 0 || i >= len(r) {
		return 0
	}
	return r[i]
}

// byteBefore and byteAt read the neighbours of a match; zero past either end.
// The edge sets are ASCII, and no byte of a multibyte rune falls in them.
func byteBefore(text string, i int) byte {
	if i <= 0 {
		return 0
	}
	return text[i-1]
}

func byteAt(text string, i int) byte {
	if i >= len(text) {
		return 0
	}
	return text[i]
}

// isWordByte is JavaScript's \w.
func isWordByte(c byte) bool {
	return c == '_' || isLowerASCII(c) || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func isLowerASCII(c byte) bool { return c >= 'a' && c <= 'z' }

// isDigit is JavaScript's \d: ASCII digits only, even in unicode mode.
func isDigit(c rune) bool { return c >= '0' && c <= '9' }

// isSpace is JavaScript's \s.
func isSpace(c rune) bool {
	switch c {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return c >= 0x2000 && c <= 0x200A
}

// sameLetter compares under simple case folding, as a case-insensitive
// JavaScript regular expression in unicode mode does.
func sameLetter(c, want rune) bool {
	if c == want {
		return true
	}
	for folded := unicode.SimpleFold(want); folded != want; folded = unicode.SimpleFold(folded) {
		if folded == c {
			return true
		}
	}
	return false
}

func foldContains(set string, c rune) bool {
	for _, want := range set {
		if sameLetter(c, want) {
			return true
		}
	}
	return false
}

// foldPrefixEnd returns the end of prefix at i, compared case-insensitively,
// or -1.
func foldPrefixEnd(r []rune, i int, prefix string) int {
	for _, want := range prefix {
		if i >= len(r) || !sameLetter(r[i], want) {
			return -1
		}
		i++
	}
	return i
}
