package behavior

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPresentationValidatesBeforeConsuming(t *testing.T) {
	spec := PresentationSpec{Choices: []Choice{{ID: "apply"}, {ID: "cancel", Cancel: true}, {ID: "disabled", Disabled: true}}, Selection: &Selection{
		Options: []Option{{ID: "first"}, {ID: "second"}, {ID: "disabled", Disabled: true}}, Multiple: true, Min: 1, Max: 2}}
	presentation, err := NewPresentation("instance", spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		instance, choice string
		selected         []string
	}{
		{"foreign", "apply", []string{"first"}}, {"instance", "forged", nil}, {"instance", "disabled", nil},
		{"instance", "", nil}, {"instance", "apply", nil}, {"instance", "apply", []string{"forged"}},
		{"instance", "apply", []string{"first", "first"}}, {"instance", "apply", []string{"disabled"}},
	} {
		if _, err := presentation.Resolve(input.instance, input.choice, input.selected); err == nil {
			t.Fatalf("accepted invalid result: %+v", input)
		}
	}
	outcome, err := presentation.Resolve("instance", "apply", []string{"second", "first"})
	if err != nil || outcome.Cancelled || !slices.Equal(outcome.Selected, []string{"first", "second"}) {
		t.Fatalf("result: %+v, %v", outcome, err)
	}
	if _, err := presentation.Resolve("instance", "apply", []string{"first"}); err == nil {
		t.Fatal("accepted a second result")
	}
}

func TestPresentationCancellationAndIsolation(t *testing.T) {
	for _, choice := range []string{"", "cancel"} {
		spec := PresentationSpec{Dismissible: true, Choices: []Choice{{ID: "apply"}, {ID: "cancel", Cancel: true}}, Selection: &Selection{Options: []Option{{ID: "one"}}, Min: 1}}
		presentation, err := NewPresentation("instance", spec)
		if err != nil {
			t.Fatal(err)
		}
		spec.Choices[1].Cancel = false
		spec.Selection.Min = 0
		outcome, err := presentation.Resolve("instance", choice, []string{"untrusted"})
		if err != nil || !outcome.Cancelled || len(outcome.Selected) != 0 {
			t.Fatalf("cancel: %+v, %v", outcome, err)
		}
	}
	presentation, err := NewPresentation("instance", PresentationSpec{Choices: []Choice{{ID: "ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	var admitted atomic.Int32
	var workers sync.WaitGroup
	for index := 0; index < 16; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := presentation.Resolve("instance", "ok", nil); err == nil {
				admitted.Add(1)
			}
		}()
	}
	workers.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("admitted %d responses", admitted.Load())
	}
}
