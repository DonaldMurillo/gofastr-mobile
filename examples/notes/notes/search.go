package notes

import (
	"strings"
	"unicode"
)

// Match indexes Unicode code points, never bytes or UTF-16 code units.
type Match struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func Matches(text, term string) []Match {
	haystack := []rune(strings.Map(unicode.ToLower, text))
	needle := []rune(strings.Map(unicode.ToLower, strings.TrimSpace(term)))
	if len(needle) == 0 {
		return nil
	}
	var result []Match
	for i := 0; i+len(needle) <= len(haystack) && len(result) < 32; {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			result = append(result, Match{i, i + len(needle)})
			i += len(needle)
		} else {
			i++
		}
	}
	return result
}

func SearchRank(note Note, term string) int {
	title, term := strings.ToLower(strings.TrimSpace(note.Title)), strings.ToLower(strings.TrimSpace(term))
	if title == term {
		return 0
	}
	if strings.HasPrefix(title, term) {
		return 1
	}
	if strings.Contains(title, term) {
		return 2
	}
	return 3
}

func SearchSnippet(body, term string) string {
	text := []rune(strings.Join(strings.Fields(body), " "))
	start := 0
	if matches := Matches(string(text), term); len(matches) > 0 {
		start = max(0, matches[0].Start-16)
		for start > 0 && start < matches[0].Start && !unicode.IsSpace(text[start-1]) {
			start++
		}
	}
	limit := max(160, min(296, len([]rune(term))+96))
	end := min(len(text), start+limit)
	result := string(text[start:end])
	if start > 0 {
		result = "…" + result
	}
	if end < len(text) {
		result += "…"
	}
	return result
}
