package notesnative

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestBindingRestartWithoutSaveCompletionRetainsUnknownAndNewestText(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Saved baseline", "Saved body")
	if err != nil {
		t.Fatal(err)
	}
	connection := servicesHello(t)
	route := fmt.Sprintf("/notes/%s", note.ID)
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	command := nativeui.Command{Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "interrupted-save", Action: "save", Revision: 1, Values: map[string]string{"title": "Committed title", "body": "Committed body"}}
	if reply := showcaseCommand(t, command); reply.Error != nil || reply.Editor == nil || !reply.Editor.Pending {
		t.Fatalf("save did not reach pending settlement: %+v", reply)
	}
	command.Kind, command.Revision, command.Values["body"] = "checkpoint", 2, "New typing after commit"
	if reply := showcaseCommand(t, command); reply.Error != nil || !reply.Editor.DraftStored {
		t.Fatalf("newer pending text was not durable: %+v", reply)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = servicesHello(t)
	recovered := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	if recovered.Error != nil || recovered.Response != nil || !recovered.Editor.Unknown || !recovered.Editor.Restored || recovered.Editor.CanLeave || recovered.Editor.Values["body"] != "New typing after commit" || recovered.Editor.Baseline["body"] != "Committed body" {
		t.Fatalf("binding restart lost uncertain save/newer edits: %+v", recovered)
	}
	command.Connection, command.Instance, command.Revision = connection, recovered.Instance, 1
	for _, choice := range []string{"keep", "discard"} {
		command.Kind, command.Choice = "leave", choice
		if reply := showcaseCommand(t, command); reply.Editor == nil || reply.Editor.CanLeave || !reply.Editor.Unknown || reply.Editor.Values["body"] != "New typing after commit" {
			t.Fatalf("restart allowed unsafe %s: %+v", choice, reply)
		}
	}
	command.Kind, command.RequestID = "action", "retry-must-not-write"
	if reply := showcaseCommand(t, command); reply.Error == nil || reply.Response != nil {
		t.Fatalf("unknown restart admitted another save: %+v", reply)
	}
	saved, err := Get(note.ID)
	if err != nil || saved.Body != "Committed body" {
		t.Fatalf("blocked retry changed repository: %+v %v", saved, err)
	}
	items, err := List("")
	if err != nil || items.Count() != 1 {
		t.Fatalf("restart replayed create: %+v %v", items, err)
	}
}

func TestBindingFilesystemDraftFaultBlocksWriteAndKeep(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Saved baseline", "Saved body")
	if err != nil {
		t.Fatal(err)
	}
	connection := servicesHello(t)
	route := fmt.Sprintf("/notes/%s", note.ID)
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	drafts, backup := filepath.Join(directory, "mobile-drafts"), filepath.Join(directory, "drafts-backup")
	if err := os.Rename(drafts, backup); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(drafts); _ = os.Rename(backup, drafts) })
	if err := os.WriteFile(drafts, []byte("not a draft directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := nativeui.Command{Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "faulted-save", Action: "save", Revision: 1, Values: map[string]string{"title": "Must remain local", "body": "Must not be written"}}
	if reply := showcaseCommand(t, command); reply.Error == nil || reply.Error.Code != "draft" || reply.Response != nil || reply.Editor == nil || reply.Editor.DraftStored || reply.Editor.Values["body"] != command.Values["body"] {
		t.Fatalf("filesystem fault did not block write: %+v", reply)
	}
	command.Kind, command.Choice = "leave", "keep"
	if reply := showcaseCommand(t, command); reply.Error == nil || reply.Editor.CanLeave || reply.Editor.Values["body"] != command.Values["body"] {
		t.Fatalf("filesystem fault allowed leave or lost buffer: %+v", reply)
	}
	saved, err := Get(note.ID)
	if err != nil || saved.Title != "Saved baseline" || saved.Body != "Saved body" {
		t.Fatalf("uncheckpointed save reached repository: %+v %v", saved, err)
	}
	if err := os.Remove(drafts); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, drafts); err != nil {
		t.Fatal(err)
	}
	command.Kind = "action"
	if reply := showcaseCommand(t, command); reply.Error != nil {
		t.Fatalf("unexecuted save could not recover after storage repair: %+v", reply)
	}
	command.Kind = "complete"
	if reply := showcaseCommand(t, command); reply.Error != nil || !reply.Editor.CanLeave {
		t.Fatalf("repaired save could not settle: %+v", reply)
	}
}
