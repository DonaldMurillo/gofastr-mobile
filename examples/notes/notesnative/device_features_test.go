package notesnative

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/device/sharein"
	"github.com/DonaldMurillo/gofastr-native/device/widgets"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func devicesHello(t *testing.T) string {
	t.Helper()
	reply := showcaseCommand(t, nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "android", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityDrafts: nativeui.SupportAvailable,
		nativeui.CapabilityPresentation: nativeui.SupportAvailable, nativeui.CapabilityFiles: nativeui.SupportAvailable, nativeui.CapabilityLive: nativeui.SupportAvailable,
		nativeui.CapabilityIncomingShares: nativeui.SupportAvailable, nativeui.CapabilitySecureStorage: nativeui.SupportAvailable, nativeui.CapabilityWidgets: nativeui.SupportAvailable, nativeui.CapabilityPrivacy: nativeui.SupportAvailable,
		"gofastr.media/v1": nativeui.SupportAvailable,
	}}})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	return reply.Connection
}

func TestNotesSharesSaveOnceAcrossAckFailureAndRestart(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := devicesHello(t)
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/inbox"})
	shown := filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "read", Action: "read"})
	item := &sharein.Item{Identifier: strings.Repeat("a", 32), Title: "Shared title", Text: "Shared text", Files: []files.Document{{Name: "brief.txt", MediaType: "text/plain", Data: []byte("Exact shared bytes")}}}
	review := serviceResult(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "received"}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, IncomingShare: item})
	path := review.Response.Navigation.Route
	opened = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: path})
	values := map[string]string{"title": "Reviewed title", "body": "Reviewed text"}
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "save", Action: "save", Values: values, Revision: 1})
	if shown.Response.Service.IncomingShare.Operation != sharein.Acknowledge || shown.Editor.Dirty {
		t.Fatal("save did not commit before acknowledgement")
	}
	serviceResult(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "ack-failed", Revision: 1, Values: values}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectFailed})
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = devicesHello(t)
	opened = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: path})
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "save-again", Action: "save", Values: values, Revision: 1})
	serviceResult(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "acked", Revision: 1, Values: values}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted})
	items, err := List("")
	if err != nil || items.Count() != 1 || items.Item(0).Title != "Reviewed title" {
		t.Fatalf("import duplicated or lost review changes: %v", err)
	}
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + items.Item(0).ID})
	media := screenMedia(t, editor.Screen)
	if len(media.Attachments) != 1 {
		t.Fatal("attachment was lost or duplicated")
	}
	missing := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: path})
	if missing.Error == nil {
		t.Fatal("acknowledged share remains reviewable")
	}
	// A retained OS launch can present the source again. Its database receipt
	// prevents another note and preserves subsequent user edits.
	if _, err := Save(items.Item(0).ID, "Edited after import", "Keep this edit"); err != nil {
		t.Fatal(err)
	}
	inbox := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/inbox"})
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: inbox.Instance, RequestID: "read-retained", Action: "read"})
	serviceResult(t, nativeui.Command{Connection: connection, Instance: inbox.Instance, RequestID: "retained"}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, IncomingShare: item})
	opened = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: path})
	filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "retained-save", Action: "save", Values: values, Revision: 1})
	items, err = List("")
	if err != nil || items.Count() != 1 || items.Item(0).Body != "Keep this edit" {
		t.Fatal("source receipt overwrote an existing note")
	}
}

func TestProtectedNotesBindSaveToAuthenticatedBytesAndLockWithoutDrafts(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := devicesHello(t)
	collection := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/protected"})
	created := filesAction(t, nativeui.Command{Connection: connection, Instance: collection.Instance, RequestID: "new", Action: "new"})
	route := created.Response.Navigation.Route
	id := strings.Split(route, "/")[2]
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	if opened.Error != nil || opened.Screen.Protection == nil || opened.Screen.Form.Draft != nil {
		t.Fatal("protected editor permits durable drafts or lacks privacy")
	}
	values := map[string]string{"title": "Private marker title 7d827", "body": "Private marker body f20c8"}
	command := nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "save-cancel", Action: "save", Values: values, Revision: 1}
	shown := filesAction(t, command)
	if shown.Response.Service.SecureStorage.Protection != securestore.Biometric {
		t.Fatal("protected note allowed unprotected storage")
	}
	cancelled := serviceResult(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "cancelled", Values: values, Revision: 1}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCancelled})
	if cancelled.Response.Saved != nil || !cancelled.Editor.Dirty {
		t.Fatal("cancelled secure write marked a note saved")
	}
	command.RequestID = "save-success"
	shown = filesAction(t, command)
	stored := bytes.Clone(shown.Response.Service.SecureStorage.Value)
	newer := map[string]string{"title": values["title"], "body": values["body"] + " newer unsaved text"}
	saved := serviceResult(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "stored", Values: newer, Revision: 2}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, SecureStorage: &securestore.Result{}})
	if saved.Editor == nil || !saved.Editor.Dirty || saved.Editor.Baseline["body"] != values["body"] || saved.Editor.Values["body"] != newer["body"] || saved.Response.Navigation != nil {
		t.Fatal("secure save adopted bytes that were never authenticated, or lost newer edits")
	}
	lock := showcaseCommand(t, nativeui.Command{Kind: "lock", Connection: connection})
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	old := showcaseCommand(t, nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: opened.Instance, Values: newer, Revision: 3})
	if old.Error == nil {
		t.Fatal("locked instance accepts plaintext checkpoints")
	}
	old = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: route})
	if old.Error == nil {
		t.Fatal("old foreground grant survived locking")
	}
	locked := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/protected/" + id})
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: locked.Instance, RequestID: "unlock", Action: "unlock"})
	denied := serviceResult(t, nativeui.Command{Connection: connection, Instance: locked.Instance, RequestID: "denied"}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectDenied})
	if denied.Response.Navigation != nil {
		t.Fatal("failed biometrics opened a protected note")
	}
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: locked.Instance, RequestID: "unlock-again", Action: "unlock"})
	granted := serviceResult(t, nativeui.Command{Connection: connection, Instance: locked.Instance, RequestID: "granted"}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, SecureStorage: &securestore.Result{Found: true, Value: stored}})
	unlocked := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: granted.Response.Navigation.Route})
	if unlocked.Editor.Values["body"] != values["body"] {
		t.Fatal("protected read changed encrypted content")
	}
	ordinary, err := List("")
	if err != nil || ordinary.Count() != 0 {
		t.Fatal("protected text entered the ordinary notes database")
	}
	_ = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{values["title"], values["body"], newer["body"]} {
			if bytes.Contains(data, []byte(marker)) {
				t.Fatalf("protected plaintext persisted in %s", filepath.Base(path))
			}
		}
		return nil
	})
	encoded, _ := json.Marshal(lock)
	if bytes.Contains(encoded, []byte(values["body"])) {
		t.Fatal("lock response disclosed protected content")
	}
}

func TestWidgetRequiresExplicitSelectionAndTracksSavedNote(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Widget title", "Saved widget text")
	if err != nil {
		t.Fatal(err)
	}
	connection := devicesHello(t)
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID})
	command := nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "choose", Action: "widget"}
	confirm := filesAction(t, command)
	if confirm.PresentationID == "" || confirm.ServiceID != "" {
		t.Fatal("widget published without explicit confirmation")
	}
	shown := filesActionResult(t, nativeui.Command{Kind: "respond", Connection: connection, Instance: opened.Instance, RequestID: "publish", PresentationID: confirm.PresentationID, Choice: "publish"})
	if shown.Response.Service.Widget.Snapshot.Text != "Saved widget text" {
		t.Fatal("widget did not use the saved note")
	}
	serviceResult(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "published"}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, Widget: &widgets.State{Published: true}})
	values := map[string]string{"title": "Updated widget title", "body": "Updated saved widget text"}
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "save", Action: "save", Values: values, Revision: 1})
	if shown.Response.Service == nil || shown.Response.Service.Widget.Snapshot.Text != values["body"] || shown.Response.Navigation != nil {
		t.Fatal("chosen widget was not refreshed after save")
	}
	serviceResult(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "saved-widget", Values: values, Revision: 1}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, Widget: &widgets.State{Published: true}})
	list := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: list.Instance, RequestID: "archive", Action: "archive", Values: map[string]string{"row": note.ID}})
	if shown.Response.Service == nil || shown.Response.Service.Widget.Operation != widgets.Clear {
		t.Fatal("archiving the published note retained its widget")
	}
	serviceResult(t, nativeui.Command{Connection: connection, Instance: list.Instance, RequestID: "clear-failed", Values: map[string]string{"row": note.ID}}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectFailed})
	stillActive, err := Get(note.ID)
	if err != nil || stillActive.Archived {
		t.Fatal("note was archived before its widget was cleared")
	}
	settings := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/settings/widget"})
	if !strings.Contains(settings.Screen.Text, "not confirmed") {
		t.Fatal("failed widget cleanup was hidden")
	}
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: settings.Instance, RequestID: "clear", Action: "clear"})
	serviceResult(t, nativeui.Command{Connection: connection, Instance: settings.Instance, RequestID: "cleared"}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted, Widget: &widgets.State{Published: false}})
	list = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	filesAction(t, nativeui.Command{Connection: connection, Instance: list.Instance, RequestID: "archive-after-clear", Action: "archive", Values: map[string]string{"row": note.ID}})
	archived, err := Get(note.ID)
	if err != nil || !archived.Archived {
		t.Fatal("archive did not finish after widget cleanup")
	}
}
