package notes

import (
	"strings"
	"testing"
)

func TestSearchSnippetFindsLateUnicodeMatch(t *testing.T) {
	body := strings.Repeat("unrelated words ", 40) + "😀 CAFÉ and the matching passage " + strings.Repeat("trailing ", 40)
	snippet := SearchSnippet(body, "café")
	if !strings.Contains(snippet, "CAFÉ") || !strings.HasPrefix(snippet, "…") || len([]rune(snippet)) > 162 {
		t.Fatalf("snippet: %q", snippet)
	}
	matches := Matches("😀 CAFÉ café", "café")
	if len(matches) != 2 || matches[0].Start != 2 || matches[0].End != 6 {
		t.Fatalf("Unicode ranges: %+v", matches)
	}
	if len(Matches("anything", "")) != 0 {
		t.Fatal("empty query matched text")
	}
	late := SearchSnippet(strings.Repeat("before ", 45)+"needle passage", "needle")
	if !strings.HasPrefix(late, "…before") {
		t.Fatalf("snippet starts mid-word: %q", late)
	}
	if matches := Matches(late, "needle"); len(matches) != 1 || matches[0].Start > 20 {
		t.Fatalf("late match pushed out of the visible preview: %q", late)
	}
	if SearchRank(Note{Title: "Calendar"}, "calendar") >= SearchRank(Note{Title: "Other", Body: "calendar"}, "calendar") {
		t.Fatal("title did not rank before body")
	}
}
