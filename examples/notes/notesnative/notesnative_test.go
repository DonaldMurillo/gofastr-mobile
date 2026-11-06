package notesnative

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesterminal"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestNativeCommandsPersistAcrossRestarts(t *testing.T) {
	defer func() { _ = Stop(); owner.SetExtractor(nil) }()
	if _, err := List(""); err == nil {
		t.Fatal("command ran before startup")
	}
	directory := filepath.Join(t.TempDir(), "first-launch", "app")
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err == nil {
		t.Fatal("duplicate startup accepted")
	}
	created, err := Save("", "A native idea", "Written without a WebView.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(created.ID, "  ", "Keep my draft"); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("native validation: %v", err)
	}
	if _, err := Save(created.ID, "A native idea", "Edited with native controls."); err != nil {
		t.Fatal(err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	items, err := List("native")
	if err != nil || items.Count() != 1 || items.Item(0).Body != "Edited with native controls." {
		t.Fatalf("persisted native search: %+v %v", items, err)
	}
	if items.Item(-1) != nil || items.Item(1) != nil {
		t.Fatal("out-of-range native snapshot access")
	}
	if _, err := Get("missing"); err == nil {
		t.Fatal("missing note was returned")
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(created.ID); err == nil {
		t.Fatal("another installation can read the first one's data")
	}
}

func TestRendererCommandDraftRecoveryAndReceipts(t *testing.T) {
	defer func() { _ = Stop(); owner.SetExtractor(nil) }()
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	call := func(command nativeui.Command) nativeui.Reply {
		t.Helper()
		command.Version = nativeui.ProtocolVersion
		payload, err := json.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := Command(string(payload))
		if err != nil {
			t.Fatal(err)
		}
		var reply nativeui.Reply
		if err := json.Unmarshal([]byte(encoded), &reply); err != nil || reply.Error != nil {
			t.Fatalf("command %s: %+v, %v", command.Kind, reply.Error, err)
		}
		return reply
	}
	hello := nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable,
		nativeui.CapabilityDrafts: nativeui.SupportAvailable, nativeui.CapabilitySharing: nativeui.SupportAvailable,
	}}}
	connection := call(hello).Connection
	opened := call(nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/new"})
	command := nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: opened.Instance, Revision: 1, Values: map[string]string{"title": "Recovered Go draft", "body": "Survives process lifecycle"}}
	if reply := call(command); !reply.Editor.DraftStored {
		t.Fatal("checkpoint not durable")
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = call(hello).Connection
	opened = call(nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/new"})
	if !opened.Editor.Restored || opened.Editor.Values["title"] != "Recovered Go draft" {
		t.Fatalf("draft did not survive restart: %+v", opened.Editor)
	}
	command.Connection, command.Instance, command.Kind, command.Action, command.RequestID = connection, opened.Instance, "action", "share", "share-recovered"
	call(command)
	command.Kind = "complete"
	completed := call(command)
	if completed.Response.Service == nil || completed.Response.Service.Share.Text != "Recovered Go draft\n\nSurvives process lifecycle" || completed.ServiceID == "" {
		t.Fatal("share did not use the recovered editing buffer")
	}
	command.Kind, command.RequestID = "service-result", "share-cancelled"
	command.ServiceID, command.ServiceResult = completed.ServiceID, &nativeui.ServiceResult{Status: nativeui.EffectCancelled}
	if receipt := call(command); !strings.Contains(receipt.Response.Announcement, "cancelled") {
		t.Fatal("share cancellation was not observed")
	}
}

func TestGoDefinedNativeScreenFlow(t *testing.T) {
	defer func() { _ = Stop(); owner.SetExtractor(nil) }()
	if _, err := Render(Root(), "{}"); err == nil {
		t.Fatal("screen rendered before startup")
	}
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	decode := func(encoded string, err error) nativeui.Response {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		var response nativeui.Response
		if err := json.Unmarshal([]byte(encoded), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	list := decode(Render(Root(), "{}"))
	if list.Screen.Title != "Notes" || list.Screen.List == nil || len(list.Screen.List.Rows) != 0 {
		t.Fatalf("initial screen: %+v", list.Screen)
	}
	editor := decode(Dispatch(Root(), "new", "{}"))
	if editor.Navigation.Kind != "push" || editor.Screen.Form.Fields[0].Key != "title" || editor.Screen.Route != "/notes/new" {
		t.Fatalf("new screen: %+v", editor)
	}
	if _, err := Dispatch(editor.Screen.Route, "save", `{"title":" ","body":"Retain my text"}`); err == nil {
		t.Fatal("invalid title was saved")
	}
	saved := decode(Dispatch(editor.Screen.Route, "save", `{"title":"Defined in Go","body":"Rendered with real UIKit"}`))
	if saved.Navigation.Kind != "pop" || !saved.ClearDraft || saved.Announcement != "Note saved" {
		t.Fatalf("save effects: %+v", saved)
	}
	list = decode(Render(Root(), `{"search":"Defined"}`))
	if len(list.Screen.List.Rows) != 1 {
		t.Fatalf("search: %+v", list.Screen.List)
	}
	row := list.Screen.List.Rows[0]
	editor = decode(Render(row.Destination, "{}"))
	if editor.Screen.Form.Fields[1].Value != "Rendered with real UIKit" {
		t.Fatal("editor does not contain persisted content")
	}
	shared := decode(Dispatch(row.Destination, "share", `{"title":"Unsaved title","body":"Unsaved body"}`))
	if shared.Service == nil || shared.Service.Share.Text != "Unsaved title\n\nUnsaved body" || shared.Navigation != nil || shared.ClearDraft {
		t.Fatalf("share changed the draft: %+v", shared)
	}
	decode(Dispatch(row.Destination, "save", `{"title":"Updated in Go","body":"Still native"}`))
	note, err := Get(row.ID)
	if err != nil || note.Title != "Updated in Go" {
		t.Fatalf("Go-defined action did not reach shared service: %+v %v", note, err)
	}
	about := decode(Dispatch(Root(), "about", "{}"))
	if about.Navigation.Kind != "sheet" || about.Screen.Text == "" {
		t.Fatalf("about sheet: %+v", about)
	}
	if closed := decode(Dispatch(about.Screen.Route, "close", "{}")); closed.Navigation.Kind != "dismiss" {
		t.Fatal("sheet did not dismiss")
	}
	if _, err := Render("/notes/missing", "{}"); err == nil {
		t.Fatal("missing note screen rendered")
	}
	if _, err := Dispatch(Root(), "delete-everything", "{}"); err == nil {
		t.Fatal("unknown native action accepted")
	}
}

func TestNotesApplicationHasNoPlatformScreenSource(t *testing.T) {
	err := filepath.WalkDir("..", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		for _, extension := range []string{".swift", ".swift.tmpl", ".java", ".java.tmpl", ".m"} {
			if !entry.IsDir() && strings.HasSuffix(name, extension) {
				t.Errorf("application owns platform screen source: %s", name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNativeNotesOpenInWebAndTerminal(t *testing.T) {
	defer func() { _ = Stop(); owner.SetExtractor(nil) }()
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	created, err := Save("", "Native to web", "Shared behavior, independent screens.")
	if err != nil {
		t.Fatal(err)
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	app, host, err := notesapp.Build(directory, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Stop(context.Background()) })
	response, err := host.Do(context.Background(), &mobilehost.Request{Method: "GET", URL: mobilehost.DefaultOrigin + "/notes/" + created.ID})
	if err != nil || response.Status != 200 || !strings.Contains(string(response.Body), created.Body) {
		t.Fatalf("native note did not reach the web presentation: %+v %v", response, err)
	}
	user, err := localapp.LoadOrMint(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx := handler.SetUser(context.Background(), user)
	var output bytes.Buffer
	if err := notesterminal.Run(ctx, notesapp.Service(app), strings.NewReader("open "+created.ID+"\nquit\n"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), created.Body) {
		t.Fatalf("native note did not reach terminal: %s", output.String())
	}
}
