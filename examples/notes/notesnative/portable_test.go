package notesnative

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesbackup"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestBackupReviewAndRestoreThroughSharedProtocol(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := devicesHello(t)
	settings := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/settings"})
	found := false
	for _, row := range settings.Screen.List.Rows {
		found = found || row.Destination == "/settings/backup"
	}
	if !found {
		t.Fatal("backup settings missing")
	}
	document, err := notesbackup.Encode(notesbackup.Bundle{Format: "gofastr-notes", Version: 1, Notes: []notesbackup.Record{{Note: notes.Note{ID: "foreign", Title: "Reviewed backup", Body: "Restored text", Details: notes.Details{Tag: "work"}}, Files: []files.Document{{Name: "brief.txt", MediaType: "text/plain", Data: []byte("exact")}}}}})
	if err != nil {
		t.Fatal(err)
	}
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/settings/backup"})
	picked := filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "choose", Action: "restore"})
	result := filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: opened.Instance, RequestID: "picked", FileID: picked.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &document}})
	items, _ := List("")
	if items.Count() != 0 {
		t.Fatal("file picker restored without review")
	}
	review := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: result.Response.Navigation.Route})
	if review.Error != nil || !strings.Contains(review.Screen.List.Header, "1 to restore") {
		t.Fatalf("review: %+v", review)
	}
	filesAction(t, nativeui.Command{Connection: connection, Instance: review.Instance, RequestID: "apply", Action: "apply"})
	items, _ = List("")
	if items.Count() != 1 || items.Item(0).Body != "Restored text" {
		t.Fatal("reviewed restore failed")
	}
	exported := filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "export", Action: "export"})
	if exported.Response.Files == nil || exported.Response.Files.Export == nil {
		t.Fatal("backup not exported through existing files adapter")
	}
	bundle, err := notesbackup.Decode(exported.Response.Files.Export.Data)
	if err != nil || len(bundle.Notes) != 1 || len(bundle.Notes[0].Files) != 1 {
		t.Fatalf("backup export: %v", err)
	}
}

func TestConflictingEditorKeepsDraftAndCanSaveTextCopy(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Original", "Original text")
	if err != nil {
		t.Fatal(err)
	}
	connection := devicesHello(t)
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID})
	if _, err := Save(note.ID, "Other editor", "Newer saved text"); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"title": "My draft", "body": "Keep unsaved text"}
	failed := showcaseCommand(t, nativeui.Command{Kind: "action", Connection: connection, Instance: editor.Instance, RequestID: "stale-save", Action: "save", Values: values, Revision: 1})
	failed = showcaseCommand(t, nativeui.Command{Kind: "complete", Connection: connection, Instance: editor.Instance, RequestID: "stale-save", Values: values, Revision: 1})
	if failed.Error == nil || failed.Error.Code != "conflict" || failed.Editor.Values["body"] != values["body"] || !failed.Editor.Dirty {
		t.Fatalf("stale save: %+v", failed)
	}
	current, _ := Get(note.ID)
	if current.Body != "Newer saved text" {
		t.Fatal("stale save overwrote a newer note")
	}
	copy := filesAction(t, nativeui.Command{Connection: connection, Instance: editor.Instance, RequestID: "copy", Action: "save-copy", Values: values, Revision: 1})
	if copy.Response.Saved.Route == "/notes/"+note.ID {
		t.Fatal("copy replaced the original")
	}
	current, _ = Get(note.ID)
	if current.Body != "Newer saved text" {
		t.Fatal("copy changed original")
	}
}

func TestRestoredDraftRetainsOriginalConflictBaseline(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Original", "Original text")
	if err != nil {
		t.Fatal(err)
	}
	connection := devicesHello(t)
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID})
	values := map[string]string{"title": "Older draft", "body": "Unsaved text"}
	checkpoint := showcaseCommand(t, nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: editor.Instance, Values: values, Revision: 1})
	if checkpoint.Error != nil {
		t.Fatal(checkpoint.Error)
	}
	if _, err := Save(note.ID, "Newer save", "Do not replace"); err != nil {
		t.Fatal(err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = devicesHello(t)
	editor = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID})
	if !editor.Editor.Restored {
		t.Fatal("draft was not restored")
	}
	// Another checkpoint and restart must not silently adopt the newer database
	// note as the old draft's save baseline.
	checkpoint = showcaseCommand(t, nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: editor.Instance, Values: values, Revision: 1})
	if checkpoint.Error != nil {
		t.Fatal(checkpoint.Error)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = devicesHello(t)
	editor = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID})
	result := showcaseCommand(t, nativeui.Command{Kind: "action", Connection: connection, Instance: editor.Instance, RequestID: "restore-save", Action: "save", Values: values, Revision: 1})
	if result.Error == nil || result.Error.Code != "conflict" {
		t.Fatalf("restored stale draft overwrote current data: %+v", result)
	}
}
