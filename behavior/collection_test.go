package behavior_test

import (
	"fmt"
	"maps"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/behavior"
)

func TestCollectionQueryResetAndAccumulatedPaging(t *testing.T) {
	state := behavior.CollectionState{Query: behavior.Values{"search": "Go", "tag": "work"}, Page: "2", Selected: []string{"a"}}
	unchanged := state.WithQuery(behavior.Values{"tag": "work", "search": "Go"})
	if !reflect.DeepEqual(state, unchanged) {
		t.Fatalf("unchanged query lost context: %+v", unchanged)
	}
	next, err := unchanged.LoadMore("3")
	if err != nil || next.Page != "3" || !maps.Equal(next.Query, state.Query) || !reflect.DeepEqual(next.Selected, state.Selected) {
		t.Fatalf("load more: %+v %v", next, err)
	}
	next.Query["search"], next.Selected[0] = "mutated", "foreign"
	if state.Query["search"] != "Go" || state.Selected[0] != "a" {
		t.Fatal("state aliases caller-owned values")
	}
	query := behavior.Values{"search": "Rust", "tag": "work"}
	reset := state.WithQuery(query)
	query["search"] = "outside"
	if reset.Page != "" || len(reset.Selected) != 0 || reset.Query["search"] != "Rust" || state.Page != "2" {
		t.Fatalf("filter change did not reset page and selection: %+v", reset)
	}
	for _, token := range []string{"", "2", "../3", "3\n", "3?limit=200", "3/4"} {
		if _, err := state.LoadMore(token); err == nil {
			t.Errorf("accepted page token %q", token)
		}
	}
}

func TestCollectionSelectionHasBoundedStableEnabledIdentity(t *testing.T) {
	available := map[string]bool{"a": true, "b": true, "disabled": false}
	for _, encoded := range []string{"null", "{}", `"a"`, `[42]`, `[null]`, `["a","a"]`, `["foreign"]`, `["disabled"]`, `["a"] []`, `["a","b"]`} {
		if _, err := behavior.DecodeSelection(encoded, available, 1); err == nil {
			t.Errorf("accepted selection %s", encoded)
		}
	}
	selected, err := behavior.DecodeSelection(` ["b", "a"] `, available, 0)
	if err != nil || !reflect.DeepEqual(selected, []string{"b", "a"}) {
		t.Fatalf("stable identities reordered: %v %v", selected, err)
	}
	if maximum, err := behavior.SelectionMaximum(0); err != nil || maximum != 200 {
		t.Fatalf("default limit: %d %v", maximum, err)
	}
	for _, maximum := range []int{-1, 201} {
		if err := behavior.ValidateSelection(nil, available, maximum); err == nil {
			t.Errorf("accepted maximum %d", maximum)
		}
	}
	for _, token := range []string{"", ".", "..", "white space", "<script>", "a/b", "a\x00", "é"} {
		if behavior.SafeToken(token) {
			t.Errorf("unsafe identity %q", token)
		}
	}
	if !behavior.SafeToken("note_42:uuid-1.2") {
		t.Fatal("rejected a safe stable identity")
	}
	selected = nil
	for index := 0; index < 201; index++ {
		identifier := fmt.Sprint(index)
		available[identifier] = true
		selected = append(selected, identifier)
	}
	if err := behavior.ValidateSelection(selected[:200], available, 0); err != nil {
		t.Fatal(err)
	}
	if err := behavior.ValidateSelection(selected, available, 0); err == nil {
		t.Fatal("default maximum admitted over 200 records")
	}
}
