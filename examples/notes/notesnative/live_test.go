package notesnative

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
)

func TestNotesRepositoryPagesBeyondTwoHundredWithStableTiesAndScopedSelection(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	var expected []string
	for range 215 {
		note, err := Save("", "Equal title", "Bodies are not fetched into collection screens")
		if err != nil {
			t.Fatal(err)
		}
		expected = append(expected, note.ID)
	}
	operation, err := beginCommand()
	if err != nil {
		t.Fatal(err)
	}
	foreignContext := handler.SetUser(context.Background(), localapp.ForID("foreign-paging-owner"))
	foreign, err := operation.service.Save(foreignContext, "", "Equal title", "Must never become visible")
	operation.finish()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(expected)
	connection := servicesHello(t)
	first := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?order=title"})
	if first.Error != nil || first.Screen.Live == nil {
		t.Fatalf("first page: %+v", first)
	}
	selected, _ := json.Marshal([]string{first.Screen.List.Rows[0].ID})
	values := first.Screen.Values
	values["selected"], values["page"] = string(selected), first.Screen.List.Paging.Next
	second := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes", Values: values})
	if second.Error != nil || len(second.Screen.List.Selection.Selected) != 1 || second.Screen.List.Selection.Selected[0] != first.Screen.List.Rows[0].ID {
		t.Fatal("load more discarded still-visible selection")
	}
	seen := make(map[string]bool)
	opened := first
	lastWindow := ""
	for step := 0; ; step++ {
		if opened.Error != nil || len(opened.Screen.List.Rows) > 100 || step > 30 {
			t.Fatalf("paging unbounded or failed at step %d: %+v", step, opened)
		}
		windowFirst := opened.Screen.List.Rows[0].ID
		if lastWindow != windowFirst && len(seen) > 0 && seen[windowFirst] {
			t.Fatal("keyset cursor repeated an earlier window")
		}
		lastWindow = windowFirst
		for index, row := range opened.Screen.List.Rows {
			if row.ID == foreign.ID || row.Subtitle == "Must never become visible" {
				t.Fatal("repository paging escaped owner scope")
			}
			if index > 0 && opened.Screen.List.Rows[index-1].ID >= row.ID {
				t.Fatal("equal titles have no stable ID tie-break")
			}
			seen[row.ID] = true
		}
		next := opened.Screen.List.Paging.Next
		if next == "" {
			break
		}
		opened = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?order=title&page=" + url.QueryEscape(next)})
	}
	if len(seen) != len(expected) {
		t.Fatalf("Load more reached %d of %d notes", len(seen), len(expected))
	}
	for _, identifier := range expected {
		if !seen[identifier] {
			t.Fatalf("unreachable record beyond 200: %s", identifier)
		}
	}
	for _, identifier := range []string{expected[0], foreign.ID} {
		selection, _ := json.Marshal([]string{identifier})
		reply := showcaseCommand(t, nativeui.Command{Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "hidden-" + identifier, Action: "pin", Values: map[string]string{"selected": string(selection)}})
		if reply.Error == nil || reply.Error.Code != "validation" {
			t.Fatal("off-window/foreign selection authorized")
		}
	}
	last := opened.Screen.List.Rows[len(opened.Screen.List.Rows)-1]
	filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "deep-row-pin", Action: "pin", Values: map[string]string{"row": last.ID}})
	pinned := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?order=title"})
	if pinned.Screen.List.Rows[0].ID != last.ID {
		t.Fatal("deep visible selection did not persist pin ordering")
	}
	foreignRoute := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + foreign.ID})
	if foreignRoute.Error == nil || foreignRoute.Error.Code != "not_found" {
		t.Fatal("foreign note can admit device actions")
	}
	seen = make(map[string]bool)
	recentRoute := "/notes"
	opened = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: recentRoute})
	for step := 0; ; step++ {
		if opened.Error != nil || len(opened.Screen.List.Rows) > 100 || step > 30 {
			t.Fatalf("recent keyset page at %d: %+v", step, opened)
		}
		for _, row := range opened.Screen.List.Rows {
			seen[row.ID] = true
		}
		if opened.Screen.List.Paging.Next == "" {
			break
		}
		recentRoute = "/notes?page=" + url.QueryEscape(opened.Screen.List.Paging.Next)
		opened = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: recentRoute})
	}
	if len(seen) != 215 {
		t.Fatalf("recent ordering reached only %d notes", len(seen))
	}
}

func TestNotesLiveCommandsObserveOnlyConfirmedOwnerWritesAndCancelOnClose(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := servicesHello(t)
	list := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	version := list.Screen.Live.Version
	editor := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/new"})
	invalid := showcaseCommand(t, nativeui.Command{Kind: "action", Connection: connection, Instance: editor.Instance, RequestID: "invalid-save", Action: "save", Values: map[string]string{"title": ""}})
	if invalid.Error == nil || liveVersion(t, connection) != version {
		t.Fatal("failed note write bumped version")
	}
	operation, err := beginCommand()
	if err != nil {
		t.Fatal(err)
	}
	_, err = operation.service.Save(handler.SetUser(context.Background(), localapp.ForID("other-live-owner")), "", "Private", "No owner notification")
	operation.finish()
	if err != nil || liveVersion(t, connection) != version {
		t.Fatal("foreign write changed current owner's version")
	}
	list = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	finished := make(chan string, 1)
	startWatch := func(instance, observed string) {
		payload, _ := json.Marshal(nativeui.Command{Version: nativeui.ProtocolVersion, Kind: "watch", Connection: connection, Instance: instance, DataVersion: observed})
		go func() {
			result, failure := Command(string(payload))
			if failure != nil {
				finished <- failure.Error()
			} else {
				finished <- result
			}
		}()
	}
	await := func() nativeui.Reply {
		t.Helper()
		select {
		case encoded := <-finished:
			var reply nativeui.Reply
			if err := json.Unmarshal([]byte(encoded), &reply); err != nil {
				t.Fatal(err)
			}
			return reply
		case <-time.After(3 * time.Second):
			t.Fatal("live read held the session lock or ignored cancellation")
		}
		return nativeui.Reply{}
	}
	startWatch(list.Instance, version)
	saved := filesAction(t, nativeui.Command{Connection: connection, Instance: editor.Instance, RequestID: "create-live", Action: "save", Values: map[string]string{"title": "Observed note", "body": "Live body"}})
	observed := await()
	if observed.Error != nil || !observed.Changed || observed.DataVersion == version {
		t.Fatalf("write did not release live watch: %+v", observed)
	}
	current := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	noteID := strings.TrimPrefix(saved.Response.Saved.Route, "/notes/")
	filesAction(t, nativeui.Command{Connection: connection, Instance: current.Instance, RequestID: "metadata-live", Action: "pin", Values: map[string]string{"row": noteID}})
	metadata := showcaseCommand(t, nativeui.Command{Kind: "watch", Connection: connection, Instance: current.Instance, DataVersion: observed.DataVersion})
	if metadata.Error != nil || !metadata.Changed {
		t.Fatalf("metadata write did not bump live version: %+v", metadata)
	}
	cancellable := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	startWatch(cancellable.Instance, cancellable.Screen.Live.Version)
	closed := showcaseCommand(t, nativeui.Command{Kind: "close", Connection: connection, Instance: cancellable.Instance})
	if closed.Error != nil {
		t.Fatal(closed.Error)
	}
	cancelled := await()
	if cancelled.Error == nil || cancelled.Changed {
		t.Fatalf("obsolete live watch returned an update: %+v", cancelled)
	}
	current = showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes"})
	stale := showcaseCommand(t, nativeui.Command{Kind: "watch", Connection: connection, Instance: current.Instance, DataVersion: current.Screen.Live.Version, Values: map[string]string{"search": "changed without refreshing"}})
	if stale.Error == nil || stale.Error.Code != "stale" {
		t.Fatalf("live read accepted an unobserved query: %+v", stale)
	}
}

func TestNotesSharingTextAndPhotoUsesTypedDocuments(t *testing.T) {
	if err := Start(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	note, err := Save("", "Share attachments", "Do not mutate")
	if err != nil {
		t.Fatal(err)
	}
	connection := servicesHello(t)
	list := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + note.ID + "/files"})
	var photo bytes.Buffer
	if err := png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for index, document := range []files.Document{{Name: "plain.txt", MediaType: "text/plain", Data: []byte("private text")}, {Name: "photo.png", MediaType: "image/png", Data: photo.Bytes()}} {
		before := liveVersion(t, connection)
		pick := filesAction(t, nativeui.Command{Connection: connection, Instance: list.Instance, RequestID: fmt.Sprintf("attach-%d", index), Action: "attach"})
		attached := filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: list.Instance, RequestID: fmt.Sprintf("attached-%d", index), FileID: pick.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &document}})
		if liveVersion(t, connection) == before {
			t.Fatal("successful attachment write did not bump live data")
		}
		detail := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: attached.Response.Navigation.Route})
		shown := filesAction(t, nativeui.Command{Connection: connection, Instance: detail.Instance, RequestID: "share", Action: "share"})
		shared := shown.Response.Service.Share.Files
		if len(shared) != 1 || shared[0].MediaType != document.MediaType || string(shared[0].Data) != string(document.Data) {
			t.Fatal("file/photo share changed bytes or media type")
		}
		serviceResult(t, nativeui.Command{Connection: connection, Instance: detail.Instance, RequestID: "shared"}, shown.ServiceID, nativeui.ServiceResult{Status: nativeui.EffectCompleted})
	}
}
