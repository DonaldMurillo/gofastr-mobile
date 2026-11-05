package notesnative

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestFilesThroughNativeBinding(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := filesHello(t)
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/files"})
	command := nativeui.Command{Connection: connection, Instance: opened.Instance, Action: "import", RequestID: "import-cancel"}
	shown := filesAction(t, command)
	if shown.FileID == "" || shown.Response.Files.Pick.MaxBytes != files.MaxBytes {
		t.Fatalf("no bounded file task: %+v", shown)
	}
	forged := nativeui.Command{Kind: "file-result", Connection: connection, Instance: opened.Instance, RequestID: "forged", FileID: "forged", FileResult: &nativeui.FileResult{Status: nativeui.EffectCancelled}}
	if reply := showcaseCommand(t, forged); reply.Error == nil {
		t.Fatal("forged picker accepted")
	}
	forged.FileID, forged.RequestID = shown.FileID, "cancel"
	filesActionResult(t, forged)
	list := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	if len(list.Screen.List.Rows) != 0 {
		t.Fatal("cancellation imported a note")
	}
	command.RequestID = "import-valid"
	shown = filesAction(t, command)
	response := nativeui.Command{Kind: "file-result", Connection: connection, Instance: opened.Instance, RequestID: "import-response", FileID: shown.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &files.Document{Name: "idea.md", MediaType: "text/markdown", Data: []byte("# Imported idea\n\nThe whole body.")}}}
	imported := filesActionResult(t, response)
	if imported.Response.Navigation == nil || !strings.HasPrefix(imported.Response.Navigation.Route, "/notes/") {
		t.Fatalf("no imported editor: %+v", imported)
	}
	noteRoute := imported.Response.Navigation.Route
	filesActionResult(t, response)
	list = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	if len(list.Screen.List.Rows) != 1 || list.Screen.List.Rows[0].Title != "Imported idea" {
		t.Fatalf("duplicate or malformed import: %+v", list.Screen)
	}
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: noteRoute})
	export := filesAction(t, nativeui.Command{Connection: connection, Instance: editor.Instance, RequestID: "export", Action: "export", Values: map[string]string{"title": "Changed locally", "body": "Not saved"}, Revision: 1})
	if string(export.Response.Files.Export.Data) != "Changed locally\n\nNot saved" || export.Editor == nil || !export.Editor.Dirty {
		t.Fatalf("export lost local edits: %+v", export)
	}
	filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: editor.Instance, RequestID: "exported", FileID: export.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted}, Values: map[string]string{"title": "Changed locally", "body": "Not saved"}, Revision: 1})
	navigation := filesAction(t, nativeui.Command{Connection: connection, Instance: editor.Instance, RequestID: "attachments-dirty", Action: "attachments", Values: map[string]string{"title": "Changed locally", "body": "Not saved"}, Revision: 1})
	if navigation.Response.Navigation == nil || navigation.Response.Navigation.Route != noteRoute+"/files" || !navigation.Editor.Dirty || !navigation.Editor.DraftStored {
		t.Fatalf("attachments must open without saving or losing the current draft: %+v", navigation)
	}
	attachments := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: navigation.Response.Navigation.Route})
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: attachments.Instance, RequestID: "attach", Action: "attach"})
	attached := filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: attachments.Instance, RequestID: "attached", FileID: shown.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &files.Document{Name: "brief.txt", MediaType: "text/plain", Data: []byte("Private attachment")}}})
	attachmentRoute := attached.Response.Navigation.Route
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = filesHello(t)
	attachment := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: attachmentRoute})
	if attachment.Error != nil || attachment.Screen.FilePreview == nil || attachment.Screen.FilePreview.File.Name != "brief.txt" || attachment.Screen.FilePreview.Text != "Private attachment" {
		t.Fatalf("attachment lost on restart: %+v", attachment)
	}
	restored := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: noteRoute})
	if restored.Error != nil || len(restored.Screen.Attachments) != 1 || restored.Screen.Attachments[0].Name != "brief.txt" || restored.Screen.Attachments[0].Destination != attachmentRoute || restored.Editor.Values["body"] != "Not saved" {
		t.Fatalf("file card or unsaved note missing after restart: screen=%+v editor=%+v", restored.Screen, restored.Editor)
	}
	export = filesAction(t, nativeui.Command{Connection: connection, Instance: attachment.Instance, RequestID: "attachment-export", Action: "export"})
	if string(export.Response.Files.Export.Data) != "Private attachment" {
		t.Fatal("attachment bytes changed")
	}
	filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: attachment.Instance, RequestID: "attachment-exported", FileID: export.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCancelled}})
	remove := filesAction(t, nativeui.Command{Connection: connection, Instance: attachment.Instance, RequestID: "remove", Action: "remove"})
	if remove.PresentationID == "" {
		t.Fatal("removal did not require confirmation")
	}
	removed := filesActionResult(t, nativeui.Command{Kind: "respond", Connection: connection, Instance: attachment.Instance, RequestID: "removed", PresentationID: remove.PresentationID, Choice: "remove"})
	if removed.Response.Navigation == nil {
		t.Fatal("removal did not finish")
	}
	missing := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: attachmentRoute})
	if missing.Error == nil || missing.Error.Code != "not_found" {
		t.Fatalf("removed attachment still exists: %+v", missing)
	}
	refreshed := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: noteRoute})
	if refreshed.Error != nil || len(refreshed.Screen.Attachments) != 0 {
		t.Fatalf("removed attachment still has a file card: %+v", refreshed)
	}
}

func TestFileValidationAndForegroundOwnership(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := filesHello(t)
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/files"})
	shown := filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "pick", Action: "import"})
	leave := showcaseCommand(t, nativeui.Command{Kind: "leave", Connection: connection, Instance: opened.Instance})
	if leave.Error == nil || leave.Error.Code != "pending" {
		t.Fatal("left pending file task")
	}
	other := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/behaviors/alert"})
	blocked := nativeui.Command{Connection: connection, Instance: other.Instance, RequestID: "competing", Action: "show"}
	blocked.Kind = "action"
	showcaseCommand(t, blocked)
	blocked.Kind = "complete"
	if reply := showcaseCommand(t, blocked); reply.Error == nil || reply.Error.Code != "pending" {
		t.Fatal("file task allowed competing alert")
	}
	for index, document := range []files.Document{
		{Name: "no.exe", MediaType: "text/plain"}, {Name: "../bad.txt", MediaType: "text/plain"}, {Name: "huge.txt", MediaType: "text/plain", Data: make([]byte, files.MaxBytes+1)},
	} {
		reply := showcaseCommand(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: opened.Instance, RequestID: fmt.Sprint(index), FileID: shown.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &document}})
		if reply.Error == nil || reply.Error.Code != "validation" {
			t.Fatalf("invalid input accepted: %+v", reply)
		}
	}
	command := nativeui.Command{Kind: "file-result", Connection: connection, Instance: opened.Instance, RequestID: "binary", FileID: shown.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &files.Document{Name: "binary.txt", MediaType: "text/plain", Data: []byte{0xff, 0x00}}}}
	if reply := showcaseCommand(t, command); reply.Error == nil || reply.Error.Code != "validation" {
		t.Fatalf("invalid UTF-8 imported: %+v", reply)
	}
	command.Kind = "complete"
	showcaseCommand(t, command)
	blocked.RequestID = "after-file"
	if reply := filesAction(t, blocked); reply.PresentationID == "" {
		t.Fatal("file validation did not release foreground slot")
	}
}

func filesHello(t *testing.T) string {
	t.Helper()
	reply := showcaseCommand(t, nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityPresentation: nativeui.SupportAvailable, nativeui.CapabilityFiles: nativeui.SupportAvailable,
	}}})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	return reply.Connection
}

func filesAction(t *testing.T, command nativeui.Command) nativeui.Reply {
	t.Helper()
	command.Kind = "action"
	return filesActionResult(t, command)
}

func filesActionResult(t *testing.T, command nativeui.Command) nativeui.Reply {
	t.Helper()
	if reply := showcaseCommand(t, command); reply.Error != nil {
		t.Fatalf("%s: %+v", command.Kind, reply.Error)
	}
	command.Kind = "complete"
	reply := showcaseCommand(t, command)
	if reply.Error != nil {
		t.Fatalf("settle: %+v", reply.Error)
	}
	return reply
}
