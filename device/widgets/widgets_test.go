package widgets

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSnapshotBoundsAndRoutes(t *testing.T) {
	valid := Snapshot{Title: "Recent notes", Text: "Updated by the app", Route: "/notes?filter=recent", Items: []Item{{ID: "note:1", Title: "Shopping", Subtitle: "Milk\nBread"}}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Snapshot){
		"empty title":         func(s *Snapshot) { s.Title = "" },
		"large unicode title": func(s *Snapshot) { s.Title = strings.Repeat("é", 101) },
		"invalid UTF-8":       func(s *Snapshot) { s.Text = string([]byte{0xff}) },
		"control":             func(s *Snapshot) { s.Title = "Notes\n" },
		"too many":            func(s *Snapshot) { s.Items = make([]Item, MaxItems+1) },
		"duplicate id":        func(s *Snapshot) { s.Items = append(s.Items, s.Items[0]) },
		"invalid id":          func(s *Snapshot) { s.Items[0].ID = "note/1" },
		"absolute route":      func(s *Snapshot) { s.Route = "https://example.com/notes" },
		"authority":           func(s *Snapshot) { s.Route = "//example.com/notes" },
		"fragment":            func(s *Snapshot) { s.Route = "/notes#fragment" },
		"traversal":           func(s *Snapshot) { s.Route = "/notes/%2e%2e/private" },
		"encoded authority":   func(s *Snapshot) { s.Route = "/%2fexample.com" },
		"encoded control":     func(s *Snapshot) { s.Route = "/notes%0aother" },
		"encoded backslash":   func(s *Snapshot) { s.Route = "/notes%5cother" },
		"query control":       func(s *Snapshot) { s.Route = "/notes?q=%0a" },
		"encoded slash":       func(s *Snapshot) { s.Route = "/notes%2fother" },
		"encoded question":    func(s *Snapshot) { s.Route = "/notes%3fother" },
		"white space":         func(s *Snapshot) { s.Route = "/notes?name=two words" },
		"large route":         func(s *Snapshot) { s.Route = "/" + strings.Repeat("x", 2048) },
		"escaped JSON size": func(s *Snapshot) {
			s.Text = strings.Repeat("<", 2048)
			s.Items = []Item{{ID: "one", Title: strings.Repeat("<", 200), Subtitle: strings.Repeat("<", 512)}, {ID: "two", Title: strings.Repeat("<", 200), Subtitle: strings.Repeat("<", 512)}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot := valid
			snapshot.Items = append([]Item(nil), valid.Items...)
			mutate(&snapshot)
			if err := snapshot.Validate(); err == nil {
				t.Fatal("accepted invalid snapshot")
			}
		})
	}
}

func TestRequestsAndObservedStates(t *testing.T) {
	snapshot := &Snapshot{Title: "Notes", Route: "/notes"}
	for _, operation := range []string{Clear, Status} {
		if err := (Request{Operation: operation}).Validate(); err != nil {
			t.Fatal(err)
		}
		if err := (Request{Operation: operation, Snapshot: snapshot}).Validate(); err == nil {
			t.Fatal("accepted content on read/clear")
		}
	}
	if err := (Request{Operation: Publish}).Validate(); err == nil {
		t.Fatal("accepted missing snapshot")
	}
	if err := (Request{Operation: "refresh"}).Validate(); err == nil {
		t.Fatal("accepted unknown operation")
	}
	request := Request{Operation: Publish, Snapshot: snapshot}
	if err := request.Accept(State{Published: true}); err != nil {
		t.Fatal(err)
	}
	if err := request.Accept(State{}); err == nil {
		t.Fatal("publish reported no snapshot")
	}
	if err := (Request{Operation: Clear}).Accept(State{Published: true}); err == nil {
		t.Fatal("clear reported stored snapshot")
	}
	for _, published := range []bool{false, true} {
		if err := (Request{Operation: Status}).Accept(State{Published: published}); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := json.Marshal(State{})
	if err != nil || string(encoded) != `{"published":false}` {
		t.Fatalf("false state omitted: %s %v", encoded, err)
	}
}
