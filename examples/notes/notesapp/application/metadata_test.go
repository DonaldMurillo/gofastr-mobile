package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp/application"
)

func TestMetadataCollectionsAndScopedUndoPersistThroughApplication(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	directory := t.TempDir()
	shared, host := start(t, directory)
	ctx := handler.SetUser(context.Background(), shared.Data.User)
	details := notes.Details{Pinned: true, Tag: "work", ReminderDate: "2028-02-29", ReminderTime: "09:45"}
	created, err := shared.Service.SaveDetails(ctx, "", "Persistent metadata", "Original body", details)
	if err != nil || created.Details != details {
		t.Fatalf("metadata create: %+v, %v", created, err)
	}
	legacy, err := shared.Service.Save(ctx, created.ID, "Legacy text update", "Preserve metadata")
	if err != nil || legacy.Details != details {
		t.Fatalf("legacy Save reset metadata: %+v, %v", legacy, err)
	}
	for _, invalid := range []notes.Details{
		{Tag: "shared"}, {Tag: "unknown"}, {ReminderDate: "2027-02-29"},
		{ReminderDate: "2028-2-09"}, {ReminderTime: "9:45"}, {ReminderTime: "24:00"},
		{ReminderDate: "0000-01-01"},
	} {
		if _, err := shared.Service.SaveDetails(ctx, created.ID, "Must not save", "", invalid); err == nil {
			t.Fatalf("accepted invalid metadata: %+v", invalid)
		}
	}
	foreignContext := handler.SetUser(context.Background(), localapp.ForID("other-owner"))
	foreign, err := shared.Service.Save(foreignContext, "", "Foreign note", "Private")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shared.Service.SaveDetails(foreignContext, created.ID, "Stolen", "", details); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("foreign metadata save: %v", err)
	}
	if _, err := shared.Service.Change(ctx, []string{created.ID, foreign.ID}, notes.Change{Kind: "trash"}); !errors.Is(err, notes.ErrNotFound) {
		t.Fatalf("foreign selected ID accepted: %v", err)
	}
	unchanged, _ := shared.Service.Get(ctx, created.ID)
	if unchanged.Trashed {
		t.Fatal("owner preflight failure partially trashed the authorized note")
	}
	changed, err := shared.Service.Change(ctx, []string{created.ID}, notes.Change{Kind: "archive"})
	if err != nil || changed.Count != 1 || changed.Undo == "" || strings.Contains(changed.Undo, created.ID) {
		t.Fatalf("archive receipt: %+v, %v", changed, err)
	}
	if active, err := shared.Service.List(ctx, ""); err != nil || len(active) != 0 {
		t.Fatalf("archive leaked into legacy list: %+v, %v", active, err)
	}
	archived, err := shared.Service.Collection(ctx, notes.Query{View: "archived", Tag: "work"})
	if err != nil || archived.Total != 1 || archived.Notes[0].ID != created.ID {
		t.Fatalf("archived collection: %+v, %v", archived, err)
	}
	if _, err := shared.Service.Undo(foreignContext, changed.Undo); !errors.Is(err, notes.ErrUndoUnavailable) {
		t.Fatalf("foreign undo: %v", err)
	}
	if _, err := application.Service(shared.App).Undo(ctx, changed.Undo); !errors.Is(err, notes.ErrUndoUnavailable) {
		t.Fatalf("another service accepted this process-local receipt: %v", err)
	}
	if _, err := shared.Service.Save(ctx, created.ID, "Updated after archive", "Keep this text on undo"); err != nil {
		t.Fatal(err)
	}
	if undone, err := shared.Service.Undo(ctx, changed.Undo); err != nil || undone.Count != 1 {
		t.Fatalf("authorized undo: %+v, %v", undone, err)
	}
	if _, err := shared.Service.Undo(ctx, changed.Undo); !errors.Is(err, notes.ErrUndoUnavailable) {
		t.Fatalf("receipt replay: %v", err)
	}
	undoNote, _ := shared.Service.Get(ctx, created.ID)
	if undoNote.Body != "Keep this text on undo" || undoNote.Details != details {
		t.Fatalf("undo overwrote text or metadata: %+v", undoNote)
	}
	trashed, err := shared.Service.Change(ctx, []string{created.ID}, notes.Change{Kind: "trash"})
	if err != nil {
		t.Fatal(err)
	}
	stop(t, host)
	restarted, _ := start(t, directory)
	ctx = handler.SetUser(context.Background(), restarted.Data.User)
	again, err := restarted.Service.Get(ctx, created.ID)
	if err != nil || !again.Trashed || again.Tag != "work" || !again.Pinned || again.ReminderDate != details.ReminderDate || again.ReminderTime != details.ReminderTime {
		t.Fatalf("metadata did not persist: %+v, %v", again, err)
	}
	trash, err := restarted.Service.Collection(ctx, notes.Query{View: "trash"})
	if err != nil || trash.Total != 1 || trash.Notes[0].ID != created.ID {
		t.Fatalf("trash did not persist: %+v, %v", trash, err)
	}
	if _, err := restarted.Service.Undo(ctx, trashed.Undo); !errors.Is(err, notes.ErrUndoUnavailable) {
		t.Fatalf("restart revived undo receipt: %v", err)
	}
	if _, err := restarted.Service.Change(ctx, []string{created.ID}, notes.Change{Kind: "restore"}); err != nil {
		t.Fatal(err)
	}
	visible, _ := restarted.Service.List(ctx, "")
	if len(visible) != 1 || visible[0].Body != undoNote.Body {
		t.Fatalf("restored note: %+v", visible)
	}
}

func TestApplicationCollectionsAccumulateStableIDsAndRejectInvalidSelection(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	shared, _ := start(t, t.TempDir())
	ctx := handler.SetUser(context.Background(), shared.Data.User)
	var ids []string
	for index := range 23 {
		created, err := shared.Service.Save(ctx, "", fmt.Sprintf("Note %02d", index), "Body")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	first, err := shared.Service.Collection(ctx, notes.Query{Order: "title"})
	if err != nil || len(first.Notes) != 10 || first.Next != "2" || first.Total != 10 {
		t.Fatalf("first page: %+v, %v", first, err)
	}
	second, _ := shared.Service.Collection(ctx, notes.Query{Order: "title", Page: 2})
	last, _ := shared.Service.Collection(ctx, notes.Query{Order: "title", Page: 3})
	if len(second.Notes) != 20 || second.Next != "3" || len(last.Notes) != 23 || last.Next != "" {
		t.Fatalf("accumulation: second=%+v last=%+v", second, last)
	}
	for index, item := range first.Notes {
		if item.ID != second.Notes[index].ID || item.ID != ids[index] {
			t.Fatal("paging replaced stable IDs or preceding rows")
		}
	}
	for _, selection := range [][]string{nil, {ids[0], ids[0]}, {"missing"}, ids[:21]} {
		if _, err := shared.Service.Change(ctx, selection, notes.Change{Kind: "tag", Tag: "ideas"}); err == nil {
			t.Fatalf("invalid selection accepted: %+v", selection)
		}
	}
	if _, err := shared.Service.Change(ctx, ids[:2], notes.Change{Kind: "tag", Tag: "ideas"}); err != nil {
		t.Fatal(err)
	}
	filtered, err := shared.Service.Collection(ctx, notes.Query{Tag: "ideas", Search: "Note"})
	if err != nil || filtered.Total != 2 {
		t.Fatalf("filtered collection: %+v, %v", filtered, err)
	}
}
