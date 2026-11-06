package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

func TestRelevancePagingKeepsEveryMatchingID(t *testing.T) {
	shared, _ := start(t, t.TempDir())
	ctx := handler.SetUser(context.Background(), shared.Data.User)
	for index := range 125 {
		_, err := shared.Service.Save(ctx, "", fmt.Sprintf("Other %03d", index), "The literal needle is in this body.")
		if err != nil {
			t.Fatal(err)
		}
	}
	exact, err := shared.Service.Save(ctx, "", "Needle", strings.Repeat("before ", 40)+"needle passage")
	if err != nil {
		t.Fatal(err)
	}
	query := notes.Query{Search: "needle", Order: "relevance", Page: 1}
	seen := map[string]bool{}
	windows := 0
	lastBoundary := ""
	for {
		result, err := shared.Service.Collection(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		if len(seen) == 0 && result.Notes[0].ID != exact.ID {
			t.Fatal("exact title did not rank first")
		}
		for _, note := range result.Notes {
			seen[note.ID] = true
		}
		if result.Next == "" {
			break
		}
		if err := query.SetPage(result.Next); err != nil {
			t.Fatal(err)
		}
		if query.After != nil && query.After.ID != lastBoundary {
			windows++
			lastBoundary = query.After.ID
		}
		if windows > 2 {
			t.Fatal("relevance pagination looped")
		}
	}
	if len(seen) != 126 || windows != 1 {
		t.Fatalf("lost matches across windows: %d, %d", len(seen), windows)
	}
}

func TestConcurrentEditorSavePreservesTheFirstCommit(t *testing.T) {
	shared, _ := start(t, t.TempDir())
	ctx := handler.SetUser(context.Background(), shared.Data.User)
	note, err := shared.Service.Save(ctx, "", "Original", "Original text")
	if err != nil {
		t.Fatal(err)
	}
	baseline := notes.EditorValues(note)
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, body := range []string{"Edit A", "Edit B"} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := shared.Service.SaveDetailsIfUnchanged(ctx, note.ID, note.Title, body, note.Details, baseline)
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, notes.ErrChanged) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("concurrent edits: saved=%d conflicts=%d", success, conflicts)
	}
}
