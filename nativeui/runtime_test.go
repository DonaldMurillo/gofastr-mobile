package nativeui

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-mobile/behavior"
)

func TestReplyAlwaysEncodesZeroRevisionAndEditorFlags(t *testing.T) {
	encoded, err := json.Marshal(Reply{Version: 1, Editor: &EditorState{}})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil || string(wire["revision"]) != "0" {
		t.Fatalf("zero revision is part of the renderer contract: %s, %v", encoded, err)
	}
	var editor map[string]json.RawMessage
	if err := json.Unmarshal(wire["editor"], &editor); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"dirty", "pending", "unknown", "restored", "draftStored", "canLeave", "canNavigate"} {
		if string(editor[flag]) != "false" {
			t.Fatalf("missing required editor flag %s: %s", flag, wire["editor"])
		}
	}
}

func TestRuntimeNewEditorOwnsSemanticDraft(t *testing.T) {
	runtime, connection, _ := runtimeFixture(t, func(context.Context, Request) (Result, error) { return Result{}, nil })
	store, err := behavior.NewFileDraftStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime.options.Drafts = store
	open := Command{Version: 1, Kind: "open", Connection: connection, Route: "/new"}
	first := runtime.Handle(context.Background(), open)
	checkpoint := Command{Version: 1, Kind: "checkpoint", Connection: connection, Instance: first.Instance, Revision: 1, Values: map[string]string{"title": "first draft"}}
	if reply := runtime.Handle(context.Background(), checkpoint); reply.Error != nil || !reply.Editor.DraftStored {
		t.Fatalf("first checkpoint: %+v", reply)
	}
	second := runtime.Handle(context.Background(), open)
	if second.Error != nil || !second.Editor.Restored || second.Editor.Values["title"] != "first draft" {
		t.Fatalf("new editor did not recover draft: %+v", second)
	}
	checkpoint.Values["title"], checkpoint.Revision = "obsolete delayed checkpoint", 2
	if reply := runtime.Handle(context.Background(), checkpoint); reply.Error == nil || reply.Error.Code != "superseded" {
		t.Fatalf("obsolete editor retained draft ownership: %+v", reply)
	}
	checkpoint.Instance, checkpoint.Values["title"] = second.Instance, "new owner"
	if reply := runtime.Handle(context.Background(), checkpoint); reply.Error != nil || !reply.Editor.DraftStored {
		t.Fatalf("new owner could not checkpoint: %+v", reply)
	}
	stored, found, err := store.Load(context.Background(), "test-draft")
	if err != nil || !found || stored.Current["title"] != "new owner" {
		t.Fatalf("obsolete work replaced newer draft: %+v, %v", stored, err)
	}
	runtime.options.MaxSessions = len(runtime.sessions)
	if reply := runtime.Handle(context.Background(), open); reply.Error == nil || reply.Error.Code != "busy" {
		t.Fatalf("screen capacity was not enforced: %+v", reply)
	}
	checkpoint.Revision, checkpoint.Values["title"] = 3, "owner after refused open"
	if reply := runtime.Handle(context.Background(), checkpoint); reply.Error != nil || !reply.Editor.DraftStored {
		t.Fatalf("refused open stole the current editor's draft: %+v", reply)
	}
}

func runtimeFixture(t *testing.T, save Handle) (*Runtime, string, string) {
	t.Helper()
	editor := func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Editor", Actions: []Action{{ID: "save", Label: "Save", Commit: true}},
			Form: &Form{Fields: []Field{{Key: "title", Label: "Title", Kind: "text", Value: "saved"}}, Draft: &Draft{Key: "test-draft"}}}, nil
	}
	app, err := New("/new", Route{Path: "/new", Screen: editor, Actions: map[string]Handle{"save": save}}, Route{Path: "/items/{id}", Screen: editor, Actions: map[string]Handle{"save": save}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(app, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hello := runtime.Handle(context.Background(), Command{Version: 1, Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[Capability]Support{
		CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, CapabilityShare: SupportFallback,
	}}})
	if hello.Error != nil || hello.Connection == "" {
		t.Fatalf("handshake: %+v", hello)
	}
	opened := runtime.Handle(context.Background(), Command{Version: 1, Kind: "open", Connection: hello.Connection, Route: "/new"})
	if opened.Error != nil || opened.Instance == "" || opened.Editor == nil {
		t.Fatalf("open: %+v", opened)
	}
	return runtime, hello.Connection, opened.Instance
}

func TestRuntimePreservesNewerInputAndAdoptsCreatedIdentity(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	var finalRoute string
	runtime, connection, instance := runtimeFixture(t, func(ctx context.Context, request Request) (Result, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		finalRoute = request.Route
		return Result{ClearDraft: true, Navigation: &Navigation{Kind: "pop"}, Saved: &SavedState{Route: "/items/42", Values: map[string]string{"title": request.Values["title"]}}, Haptic: "success"}, nil
	})
	command := Command{Version: 1, Kind: "action", Connection: connection, Instance: instance, RequestID: "save-1", Action: "save", Revision: 1, Values: map[string]string{"title": "submitted"}}
	completed := make(chan Reply, 1)
	go func() { completed <- runtime.Handle(context.Background(), command) }()
	<-started
	checkpoint := command
	checkpoint.Kind, checkpoint.Revision, checkpoint.Values = "checkpoint", 2, map[string]string{"title": "newer typing"}
	observed := runtime.Handle(context.Background(), checkpoint)
	if observed.Error != nil || !observed.Editor.Pending || observed.Editor.Values["title"] != "newer typing" {
		t.Fatalf("checkpoint blocked by business execution: %+v", observed)
	}
	duplicate := command
	duplicate.Revision = 2
	if result := runtime.Handle(context.Background(), duplicate); result.Error == nil {
		t.Fatal("duplicate pending action admitted")
	}
	close(release)
	if result := <-completed; result.Error != nil {
		t.Fatal(result.Error)
	}
	checkpoint.Kind = "complete"
	result := runtime.Handle(context.Background(), checkpoint)
	if result.Error != nil || result.Response.Navigation != nil || result.Editor.CanNavigate || result.Editor.Pending || result.Editor.Baseline["title"] != "submitted" || result.Editor.Values["title"] != "newer typing" {
		t.Fatalf("completion lost newer input: %+v %+v", result, result.Editor)
	}
	checkpoint.Kind, checkpoint.RequestID = "action", "save-2"
	if result := runtime.Handle(context.Background(), checkpoint); result.Error != nil {
		t.Fatal(result.Error)
	}
	checkpoint.Kind = "complete"
	result = runtime.Handle(context.Background(), checkpoint)
	if result.Error != nil || !result.Editor.CanNavigate || result.Response.Navigation == nil || finalRoute != "/items/42" || calls.Load() != 2 {
		t.Fatalf("a second save recreated the entity or failed: route=%q calls=%d reply=%+v", finalRoute, calls.Load(), result)
	}
	checkpoint.Kind = "action"
	if result := runtime.Handle(context.Background(), checkpoint); result.Error != nil || calls.Load() != 2 {
		t.Fatalf("completed request replayed the mutation: %+v calls=%d", result, calls.Load())
	}
}

func TestRuntimeForwardNavigationRequiresPreservedDraft(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		kind    string
		persist bool
		allowed bool
	}{
		{name: "push with stored draft", kind: "push", persist: true, allowed: true},
		{name: "sheet with stored draft", kind: "sheet", persist: true, allowed: true},
		{name: "push without draft storage", kind: "push"},
		{name: "pop does not abandon dirty editor", kind: "pop", persist: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			app, err := New("/editor", Route{Path: "/editor", Screen: func(context.Context, Request) (Screen, error) {
				return Screen{Title: "Editor", Actions: []Action{{ID: "next", Label: "Next"}}, Form: &Form{
					Fields: []Field{{Key: "title", Label: "Title", Kind: "text", Value: "Saved"}}, Draft: &Draft{Key: "draft"},
				}}, nil
			}, Actions: map[string]Handle{"next": func(context.Context, Request) (Result, error) {
				return Result{Navigation: &Navigation{Kind: scenario.kind, Route: "/child"}, ClearDraft: true}, nil
			}}}, Route{Path: "/child", Screen: func(context.Context, Request) (Screen, error) {
				return Screen{Title: "Child", Text: "Content"}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			options := RuntimeOptions{}
			if scenario.persist {
				options.Drafts, err = behavior.NewFileDraftStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
			}
			runtime, err := NewRuntime(app, options)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			hello := runtime.Handle(ctx, Command{Version: 1, Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[Capability]Support{CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable}}})
			opened := runtime.Handle(ctx, Command{Version: 1, Kind: "open", Connection: hello.Connection, Route: "/editor"})
			command := Command{Version: 1, Kind: "action", Connection: hello.Connection, Instance: opened.Instance, RequestID: "next", Action: "next", Revision: 1, Values: map[string]string{"title": "Unsaved"}}
			if reply := runtime.Handle(ctx, command); reply.Error != nil {
				t.Fatal(reply.Error)
			}
			command.Kind = "complete"
			reply := runtime.Handle(ctx, command)
			if reply.Editor == nil || !reply.Editor.Dirty || reply.Editor.Values["title"] != "Unsaved" || reply.Response == nil || reply.Response.ClearDraft {
				t.Fatalf("navigation lost or cleared current edits: %+v", reply)
			}
			if scenario.allowed {
				if reply.Error != nil || reply.Response.Navigation == nil || !reply.Editor.DraftStored {
					t.Fatalf("stored draft cannot open a child screen: %+v", reply)
				}
			} else if reply.Response.Navigation != nil || reply.Error == nil || reply.Error.Code != "unsaved_changes" {
				t.Fatalf("unsafe navigation must fail visibly: %+v", reply)
			}
		})
	}
}

func TestRuntimeUnknownSaveIsNotRetried(t *testing.T) {
	var writes atomic.Int32
	runtime, connection, instance := runtimeFixture(t, func(context.Context, Request) (Result, error) {
		writes.Add(1)
		return Result{}, errors.New("commit succeeded but response failed")
	})
	command := Command{Version: 1, Kind: "action", Connection: connection, Instance: instance, RequestID: "first", Action: "save", Revision: 1, Values: map[string]string{"title": "maybe saved"}}
	if result := runtime.Handle(context.Background(), command); result.Error == nil || result.Error.Code != "unknown" {
		t.Fatalf("uncertain mutation reported as definite failure: %+v", result)
	}
	command.Kind = "complete"
	if result := runtime.Handle(context.Background(), command); result.Editor == nil || !result.Editor.Unknown || !result.Editor.Dirty {
		t.Fatalf("uncertain outcome lost the buffer: %+v", result)
	}
	command.Kind, command.RequestID = "action", "retry"
	if result := runtime.Handle(context.Background(), command); result.Error == nil || writes.Load() != 1 {
		t.Fatalf("unsafe unknown-outcome retry: %+v writes=%d", result, writes.Load())
	}
}

func TestRuntimeRejectsStaleAndForeignInstances(t *testing.T) {
	runtime, connection, instance := runtimeFixture(t, func(context.Context, Request) (Result, error) { return Result{}, nil })
	command := Command{Version: 1, Kind: "checkpoint", Connection: connection, Instance: instance, Revision: 2, Values: map[string]string{"title": "new"}}
	if result := runtime.Handle(context.Background(), command); result.Error != nil {
		t.Fatal(result.Error)
	}
	command.Revision, command.Values = 1, map[string]string{"title": "old"}
	if result := runtime.Handle(context.Background(), command); result.Error == nil || result.Error.Code != "stale" {
		t.Fatalf("accepted stale checkpoint: %+v", result)
	}
	command.Connection = "another-renderer"
	if result := runtime.Handle(context.Background(), command); result.Error == nil {
		t.Fatal("accepted another renderer's instance")
	}
	for _, payload := range []string{`{"version":1,"kind":"hello","unexpected":true}`, `{} {}`, stringsOfSize(8<<20 + 1)} {
		if _, err := runtime.CommandJSON(context.Background(), payload); err == nil {
			t.Fatal("invalid command escaped the bounded wire decoder")
		}
	}
}

func TestRuntimeExpiresIdleConnections(t *testing.T) {
	runtime, connection, instance := runtimeFixture(t, func(context.Context, Request) (Result, error) { return Result{}, nil })
	now := time.Now()
	runtime.options.Now = func() time.Time { return now }
	now = now.Add(time.Hour)
	runtime.mu.Lock()
	runtime.expire()
	runtime.mu.Unlock()
	result := runtime.Handle(context.Background(), Command{Version: 1, Kind: "checkpoint", Connection: connection, Instance: instance})
	if result.Error == nil || result.Error.Code != "expired" {
		t.Fatalf("expired renderer remained active: %+v", result)
	}
}

func stringsOfSize(size int) string { return string(make([]byte, size)) }
