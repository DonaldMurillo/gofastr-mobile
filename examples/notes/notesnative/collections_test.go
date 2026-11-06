package notesnative

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestNoteControlsCollectionsAndTabsThroughBinding(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := filesHello(t)
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/new"})
	assertTabs(t, editor.Screen, "notes")
	fields := make(map[string]nativeui.Field)
	for _, field := range editor.Screen.Form.Fields {
		fields[field.Key] = field
	}
	if !fields["title"].Required || fields["pinned"].Kind != "toggle" || fields["tag"].Kind != "select" || fields["reminder_date"].Kind != "date" || fields["reminder_time"].Kind != "time" {
		t.Fatalf("editor controls: %+v", fields)
	}
	for _, invalid := range []map[string]string{
		{"title": ""}, {"title": "Invalid toggle", "pinned": "1"}, {"title": "Disabled tag", "tag": "shared"},
		{"title": "Invalid date", "reminder_date": "2027-02-29"}, {"title": "Invalid time", "reminder_time": "25:00"},
	} {
		result := showcaseCommand(t, nativeui.Command{Kind: "action", Connection: connection, Instance: editor.Instance, RequestID: fmt.Sprintf("invalid-%d", len(invalid["title"])), Action: "save", Values: invalid})
		if result.Error == nil || result.Error.Code != "validation" {
			t.Fatalf("invalid control accepted: %+v, %+v", invalid, result)
		}
	}
	values := map[string]string{"title": "A metadata note", "body": "The original body", "pinned": "true", "tag": "work", "reminder_date": "2028-02-29", "reminder_time": "08:15"}
	saved := filesAction(t, nativeui.Command{Connection: connection, Instance: editor.Instance, RequestID: "save-metadata", Action: "save", Values: values})
	noteRoute := saved.Response.Saved.Route
	noteID := strings.TrimPrefix(noteRoute, "/notes/")
	for key, value := range values {
		if saved.Response.Saved.Values[key] != value {
			t.Fatalf("metadata baseline %s: %+v", key, saved.Response.Saved)
		}
	}
	for index := range 12 {
		if _, err := Save("", fmt.Sprintf("Page note %02d", index), "Another body"); err != nil {
			t.Fatal(err)
		}
	}
	list := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?order=title"})
	if list.Error != nil || len(list.Screen.List.Rows) != 10 || list.Screen.List.Paging.Next != "2" || list.Screen.List.Selection.Key != "selected" || list.Screen.List.Selection.Maximum != 20 {
		t.Fatalf("first list page: %+v", list)
	}
	for _, row := range list.Screen.List.Rows {
		if len(row.LeadingActions) != 1 || row.LeadingActions[0].ID != "pin" || !row.LeadingActions[0].FullSwipe || len(row.TrailingActions) != 2 || row.TrailingActions[1].ID != "trash" || row.TrailingActions[1].FullSwipe {
			t.Fatalf("unsafe or missing row actions: %+v", row)
		}
	}
	last := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?order=title&page=2"})
	if last.Error != nil || len(last.Screen.List.Rows) != 13 || last.Screen.List.Paging.Next != "" {
		t.Fatalf("accumulated list: %+v", last)
	}
	for index, row := range list.Screen.List.Rows {
		if last.Screen.List.Rows[index].ID != row.ID {
			t.Fatal("load more replaced the first page")
		}
	}
	foreign := showcaseCommand(t, nativeui.Command{Kind: "action", Connection: connection, Instance: list.Instance, RequestID: "forged-selection", Action: "tag", Values: map[string]string{"selected": `["foreign-id"]`}})
	if foreign.Error == nil || foreign.Error.Code != "validation" {
		t.Fatalf("forged selection accepted: %+v", foreign)
	}
	selection, _ := json.Marshal([]string{noteID})
	shown := filesAction(t, nativeui.Command{Connection: connection, Instance: list.Instance, RequestID: "tag-selected", Action: "tag", Values: map[string]string{"selected": string(selection)}})
	if shown.PresentationID == "" || shown.Response.Presentation.Selection == nil {
		t.Fatalf("tag task not presented: %+v", shown)
	}
	filesActionResult(t, nativeui.Command{Kind: "respond", Connection: connection, Instance: list.Instance, RequestID: "apply-tag", PresentationID: shown.PresentationID, Choice: "apply", Selected: []string{"ideas"}})
	filtered := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?tag=ideas"})
	if filtered.Error != nil || len(filtered.Screen.List.Rows) != 1 || filtered.Screen.List.Rows[0].ID != noteID {
		t.Fatalf("tag operation did not refresh data: %+v", filtered)
	}
	pin := filesAction(t, nativeui.Command{Connection: connection, Instance: filtered.Instance, RequestID: "row-pin", Action: "pin", Values: map[string]string{"row": noteID}})
	if pin.Response.Navigation != nil || pin.Response.Announcement == "" {
		t.Fatalf("row pin must return a refreshable no-navigation result: %+v", pin)
	}
	archive := filesAction(t, nativeui.Command{Connection: connection, Instance: filtered.Instance, RequestID: "row-archive", Action: "archive", Values: map[string]string{"row": noteID}})
	if archive.Response.Navigation != nil || archive.Response.Presentation != nil {
		t.Fatalf("safe row archive unexpectedly navigated: %+v", archive)
	}
	archived := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?view=archived"})
	if archived.Error != nil || len(archived.Screen.List.Rows) != 1 || archived.Screen.List.Rows[0].ID != noteID {
		t.Fatalf("archive did not change collection: %+v", archived)
	}
	undo := filesAction(t, nativeui.Command{Connection: connection, Instance: archived.Instance, RequestID: "undo-archive", Action: "undo"})
	if undo.Response.Navigation != nil || !strings.Contains(undo.Response.Announcement, "Undo applied") {
		t.Fatalf("undo effects: %+v", undo)
	}
	for route, active := range map[string]string{"/notes": "notes", "/files": "files", "/settings": "settings", "/settings/about": "settings", "/about": "settings", "/behaviors": "settings", noteRoute + "/files": "notes"} {
		opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
		if opened.Error != nil {
			t.Fatalf("tab route %s: %+v", route, opened.Error)
		}
		assertTabs(t, opened.Screen, active)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	again, err := Get(noteID)
	if err != nil || again.Pinned || again.Archived || again.Tag != "ideas" || again.ReminderDate != "2028-02-29" || again.ReminderTime != "08:15" {
		t.Fatalf("binding metadata did not persist: %+v, %v", again, err)
	}
}

func TestNoteCollectionQueryChangesResetPagingAndSelection(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	for index := range 13 {
		if _, err := Save("", fmt.Sprintf("Query note %02d", index), "Still a note"); err != nil {
			t.Fatal(err)
		}
	}
	connection := filesHello(t)
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?page=2"})
	if opened.Error != nil || len(opened.Screen.List.Rows) != 13 {
		t.Fatalf("open accumulated query: %+v", opened)
	}
	values := opened.Screen.Values
	selected, _ := json.Marshal([]string{opened.Screen.List.Rows[0].ID})
	values["selected"], values["view"] = string(selected), "trash"
	changed := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes", Values: values})
	if changed.Error != nil || changed.Screen.Values["page"] != "1" || changed.Screen.Values["selected"] != "[]" || len(changed.Screen.List.Selection.Selected) != 0 {
		t.Fatalf("query retained stale paging/selection: %+v", changed)
	}
	stale := showcaseCommand(t, nativeui.Command{Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "stale-query", Action: "trash", Values: values})
	if stale.Error == nil || stale.Error.Code != "validation" {
		t.Fatalf("query changed while dispatching selected IDs: %+v", stale)
	}
	if items, err := List(""); err != nil || items.Count() != 13 {
		t.Fatalf("stale selection mutated notes: %+v, %v", items, err)
	}
}

func TestSoftTrashConfirmationKeepsAttachmentsAndCanRestore(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Keep my file", "Soft deletion")
	if err != nil {
		t.Fatal(err)
	}
	connection := filesHello(t)
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID})
	shown := filesAction(t, nativeui.Command{Connection: connection, Instance: editor.Instance, RequestID: "attach", Action: "attach"})
	attached := filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: editor.Instance, RequestID: "attached", FileID: shown.FileID,
		FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &files.Document{Name: "keep.txt", MediaType: "text/plain", Data: []byte("Still here after trash")}}})
	attachmentRoute := attached.Response.Navigation.Route
	list := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	command := nativeui.Command{Connection: connection, Instance: list.Instance, RequestID: "trash-cancel", Action: "trash", Values: map[string]string{"row": note.ID}}
	shown = filesAction(t, command)
	if shown.Response.Presentation == nil || shown.Response.Presentation.Kind != nativeui.PresentationConfirmation || shown.Response.Presentation.Choices[0].Role != "destructive" {
		t.Fatalf("trash did not require Go confirmation: %+v", shown)
	}
	filesActionResult(t, nativeui.Command{Kind: "respond", Connection: connection, Instance: list.Instance, RequestID: "cancel-trash", PresentationID: shown.PresentationID, Choice: "cancel"})
	if note, err := Get(note.ID); err != nil || note.Trashed {
		t.Fatalf("cancelled trash changed note: %+v, %v", note, err)
	}
	command.RequestID = "trash-confirm"
	shown = filesAction(t, command)
	filesActionResult(t, nativeui.Command{Kind: "respond", Connection: connection, Instance: list.Instance, RequestID: "confirm-trash", PresentationID: shown.PresentationID, Choice: "apply"})
	active, err := List("")
	if err != nil || active.Count() != 0 {
		t.Fatalf("trash leaked into active notes: %+v, %v", active, err)
	}
	attachment := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: attachmentRoute})
	if attachment.Error != nil || screenMedia(t, attachment.Screen).Preview == nil || !strings.Contains(screenMedia(t, attachment.Screen).Preview.Text, "Still here after trash") {
		t.Fatalf("soft trash erased attachment: %+v", attachment)
	}
	trash := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?view=trash"})
	if trash.Error != nil || len(trash.Screen.List.Rows) != 1 {
		t.Fatalf("trash collection: %+v", trash)
	}
	filesAction(t, nativeui.Command{Connection: connection, Instance: trash.Instance, RequestID: "restore", Action: "restore", Values: map[string]string{"row": note.ID}})
	active, _ = List("")
	if active.Count() != 1 {
		t.Fatal("restore did not return note to active collection")
	}
}

func assertTabs(t *testing.T, screen *nativeui.Screen, active string) {
	t.Helper()
	if screen == nil || screen.ActiveTab != active || len(screen.Tabs) != 3 {
		t.Fatalf("tab shell: %+v", screen)
	}
	for index, expected := range []struct{ id, route string }{{"notes", "/notes"}, {"files", "/files"}, {"settings", "/settings"}} {
		if screen.Tabs[index].ID != expected.id || screen.Tabs[index].Route != expected.route {
			t.Fatalf("unstable tab metadata: %+v", screen.Tabs)
		}
	}
}
