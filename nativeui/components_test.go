package nativeui_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func componentForm() nativeui.Screen {
	return nativeui.Screen{Title: "Mixed controls", Actions: []nativeui.Action{{ID: "save", Label: "Save", Commit: true}}, Form: &nativeui.Form{Fields: []nativeui.Field{
		{Key: "body", Label: "Body", Kind: "multiline", Value: "Body"},
		{Key: "title", Label: "Title", Kind: "text", Required: true, Value: "Saved"},
		{Key: "pinned", Label: "Pinned", Kind: "toggle", Required: true, Value: "true"},
		{Key: "tag", Label: "Tag", Kind: "select", Value: "home", Options: []nativeui.Option{{Value: "home", Label: "Home"}, {Value: "work", Label: "Work"}, {Value: "old", Label: "Old", Disabled: true}}},
		{Key: "date", Label: "Date", Kind: "date", Value: "2028-02-29"},
		{Key: "time", Label: "Time", Kind: "time", Value: "09:05"},
		{Key: "locked", Label: "Locked", Kind: "text", Disabled: true, Value: "fixed"},
		{Key: "footer", Label: "Footer", Kind: "multiline"},
	}, Draft: &nativeui.Draft{Key: "component-draft"}}}
}

func componentList() nativeui.Screen {
	return nativeui.Screen{Title: "Collection", Actions: []nativeui.Action{{ID: "bulk", Label: "Apply", Commit: true}}, List: &nativeui.List{
		SearchKey: "search", Filters: []nativeui.Field{{Key: "tag", Label: "Tag", Kind: "select", Value: "home", Options: []nativeui.Option{{Value: "home", Label: "Home"}, {Value: "work", Label: "Work"}}}},
		Selection: &nativeui.Selection{Key: "selected", Selected: []string{"a"}}, Paging: &nativeui.Paging{Key: "page", Next: "2", Label: "Load more"},
		Rows: []nativeui.Row{
			{ID: "a", Title: "First", Destination: "/items/a", LeadingActions: []nativeui.RowAction{{ID: "pin", Label: "Pin", FullSwipe: true}}, TrailingActions: []nativeui.RowAction{{ID: "trash", Label: "Trash", Role: "destructive"}}},
			{ID: "b", Title: "Disabled", Destination: "/items/b", Disabled: true, LeadingActions: []nativeui.RowAction{{ID: "pin", Label: "Pin"}}},
			{ID: "c", Title: "Third", Destination: "/items/c", TrailingActions: []nativeui.RowAction{{ID: "trash", Label: "Trash", Role: "destructive"}}},
		},
	}}
}

func componentApp(t *testing.T, render nativeui.Render, handle nativeui.Handle, respond nativeui.Respond) *nativeui.App {
	t.Helper()
	presentations := make(map[string]nativeui.Respond)
	if respond != nil {
		presentations["confirm"] = respond
	}
	app, err := nativeui.New("/items", nativeui.Route{Path: "/items", Screen: render,
		Actions:       map[string]nativeui.Handle{"save": handle, "bulk": handle, "pin": handle, "trash": handle, "hidden": handle},
		Presentations: presentations,
	}, nativeui.Route{Path: "/items/{id}", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Item", Text: "Item"}, nil
	}}, nativeui.Route{Path: "/settings", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Settings", Text: "Settings"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func componentRuntime(t *testing.T, app *nativeui.App, options nativeui.RuntimeOptions) (*nativeui.Runtime, string, nativeui.Reply) {
	t.Helper()
	runtime, err := nativeui.NewRuntime(app, options)
	if err != nil {
		t.Fatal(err)
	}
	hello := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[nativeui.Capability]nativeui.Support{nativeui.CapabilityPresentation: nativeui.SupportAvailable, nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityFiles: nativeui.SupportFallback}}})
	if hello.Error != nil {
		t.Fatal(hello.Error)
	}
	opened := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "open", Connection: hello.Connection, Route: "/items"})
	if opened.Error != nil {
		t.Fatal(opened.Error)
	}
	return runtime, hello.Connection, opened
}

func assertComponentFailure(t *testing.T, err error, key string) {
	t.Helper()
	var failure *nativeui.ActionError
	if !errors.As(err, &failure) || failure.Code != "validation" || failure.Fields[key] == "" || !errors.Is(err, nativeui.ErrInvalidValues) {
		t.Fatalf("expected structured %s validation, got %v", key, err)
	}
}

func TestComponentWireAndRenderedCollectionValues(t *testing.T) {
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		screen := componentList()
		screen.Tabs = []nativeui.Tab{{ID: "notes", Label: "Notes", Route: "/items", Icon: "notes"}, {ID: "settings", Label: "Settings", Route: "/settings", Icon: "settings"}}
		screen.ActiveTab = "notes"
		return screen, nil
	}, func(context.Context, nativeui.Request) (nativeui.Result, error) { return nativeui.Result{}, nil }, nil)
	encoded, err := app.RenderJSON(context.Background(), "/items?search=Go", "{}")
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{`"filters":`, `"selection":`, `"maximum":200`, `"paging":`, `"leadingActions":`, `"trailingActions":`, `"fullSwipe":true`, `"disabled":true`, `"activeTab":"notes"`, `"tabs":`} {
		if !strings.Contains(encoded, member) {
			t.Errorf("missing %s in %s", member, encoded)
		}
	}
	var response nativeui.Response
	if err := json.Unmarshal([]byte(encoded), &response); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(response.Screen.Values, map[string]string{"search": "Go", "tag": "home", "page": "", "selected": `["a"]`}) {
		t.Fatalf("incomplete render values: %v", response.Screen.Values)
	}
}

func TestDispatchValidatesMixedFormControls(t *testing.T) {
	var calls int
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) { return componentForm(), nil }, func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		calls++
		if request.Values["locked"] != "fixed" || request.Values["extra"] != "preserved" {
			t.Fatalf("partial submission lost fields: %v", request.Values)
		}
		return nativeui.Result{Saved: &nativeui.SavedState{Route: "/items", Values: map[string]string{"title": "Canonical"}}}, nil
	}, nil)
	for _, test := range []struct{ key, value string }{
		{"title", ""}, {"title", "  "}, {"pinned", "TRUE"}, {"pinned", "1"}, {"pinned", ""},
		{"date", "2027-02-29"}, {"date", "2028-2-29"}, {"date", "0000-01-01"}, {"date", "2028-02-29T00:00:00Z"},
		{"time", "9:05"}, {"time", "24:00"}, {"time", "09:05:00"}, {"time", "09:05Z"},
		{"tag", "old"}, {"tag", "foreign"}, {"locked", "changed"}, {"row", "a"},
	} {
		t.Run(test.key+"="+test.value, func(t *testing.T) {
			_, err := app.Dispatch(context.Background(), "/items", "save", map[string]string{test.key: test.value})
			assertComponentFailure(t, err, test.key)
		})
	}
	if calls != 0 {
		t.Fatal("invalid controls reached a handler")
	}
	response, err := app.Dispatch(context.Background(), "/items", "save", map[string]string{"title": "Title", "pinned": "false", "date": "", "time": "", "tag": "", "extra": "preserved"})
	if err != nil || calls != 1 || response.Saved.Values["pinned"] != "false" || response.Saved.Values["body"] != "Body" || response.Saved.Values["title"] != "Canonical" || response.Saved.Values["locked"] != "fixed" {
		t.Fatalf("mixed/optional controls or canonical baseline failed: %+v %v", response, err)
	}
	if _, err := app.Dispatch(context.Background(), "/items", "hidden", nil); !errors.Is(err, nativeui.ErrUnknownAction) {
		t.Fatalf("undeclared screen action dispatched: %v", err)
	}
}

func TestDispatchCollectionSelectionQueryAndRowTargets(t *testing.T) {
	calls := 0
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) { return componentList(), nil }, func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		calls++
		return nativeui.Result{Announcement: request.Values["row"] + request.Values["selected"]}, nil
	}, nil)
	for _, test := range []struct{ action, key, value string }{
		{"bulk", "selected", `null`}, {"bulk", "selected", `["foreign"]`}, {"bulk", "selected", `["b"]`}, {"bulk", "selected", `["a","a"]`},
		{"bulk", "selected", `[42]`}, {"bulk", "tag", "work"}, {"bulk", "tag", "foreign"}, {"bulk", "search", "changed"}, {"bulk", "page", "2"},
		{"bulk", "row", "a"}, {"pin", "row", "foreign"}, {"pin", "row", "b"}, {"pin", "row", "c"}, {"pin", "row", ""},
	} {
		t.Run(test.action+test.key+test.value, func(t *testing.T) {
			_, err := app.Dispatch(context.Background(), "/items", test.action, map[string]string{test.key: test.value})
			assertComponentFailure(t, err, test.key)
		})
	}
	_, err := app.Dispatch(context.Background(), "/items", "pin", nil)
	assertComponentFailure(t, err, "row")
	if calls != 0 {
		t.Fatal("invalid collection input mutated application data")
	}
	response, err := app.Dispatch(context.Background(), "/items", "pin", map[string]string{"row": "a", "selected": ` ["a", "c"] `})
	if err != nil || calls != 1 || response.Announcement != `a["a","c"]` {
		t.Fatalf("registered row action: %+v %v", response, err)
	}
}

func TestRuntimeValidatesCurrentAuthorizationAndPreservesPartialBuffers(t *testing.T) {
	var disabled atomic.Bool
	calls := 0
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		screen := componentForm()
		screen.Form.Fields[3].Options[1].Disabled = disabled.Load()
		return screen, nil
	}, func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		calls++
		return nativeui.Result{Saved: &nativeui.SavedState{Route: "/items", Values: map[string]string{"title": strings.TrimSpace(request.Values["title"])}}}, nil
	}, nil)
	store, err := behavior.NewFileDraftStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, connection, opened := componentRuntime(t, app, nativeui.RuntimeOptions{Drafts: store})
	command := nativeui.Command{Version: 1, Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "invalid", Action: "save", Revision: 1, Values: map[string]string{"date": "not a date"}}
	reply := runtime.Handle(context.Background(), command)
	if reply.Error == nil || reply.Error.Fields["date"] == "" || calls != 0 || reply.Editor.Values["date"] != "not a date" || !reply.Editor.DraftStored {
		t.Fatalf("runtime skipped form validation: %+v", reply)
	}
	disabled.Store(true)
	command.RequestID, command.Values = "reauthorize", map[string]string{"tag": "work", "date": "2028-02-29"}
	reply = runtime.Handle(context.Background(), command)
	if reply.Error == nil || reply.Error.Fields["tag"] == "" || calls != 0 {
		t.Fatalf("stale option authorization used: %+v", reply)
	}
	command.Kind = "complete"
	runtime.Handle(context.Background(), command)
	disabled.Store(false)
	command.Kind, command.RequestID, command.Revision, command.Values = "action", "save", 2, map[string]string{"title": " Canonical ", "tag": "home"}
	reply = runtime.Handle(context.Background(), command)
	if reply.Error != nil || calls != 1 || reply.Response.Saved.Values["date"] != "2028-02-29" || reply.Response.Saved.Values["pinned"] != "true" {
		t.Fatalf("partial save lost typed controls: %+v", reply)
	}
	checkpoint := command
	checkpoint.Kind, checkpoint.Revision, checkpoint.Values = "checkpoint", 3, map[string]string{"tag": "work"}
	if reply := runtime.Handle(context.Background(), checkpoint); reply.Error != nil || reply.Editor.Values["body"] != "Body" || reply.Editor.Values["title"] != " Canonical " {
		t.Fatalf("partial checkpoint erased a buffer: %+v", reply)
	}
	command.Kind, command.Revision, command.Values = "complete", 3, nil
	reply = runtime.Handle(context.Background(), command)
	if reply.Error != nil || !reply.Editor.Dirty || reply.Editor.Values["tag"] != "work" || reply.Editor.Baseline["tag"] != "home" || reply.Editor.Baseline["pinned"] != "true" || !reply.Editor.DraftStored {
		t.Fatalf("save settlement erased extras/newer input: %+v", reply)
	}
	checkpoint.Revision, checkpoint.Values = 4, map[string]string{"locked": "changed"}
	if reply := runtime.Handle(context.Background(), checkpoint); reply.Error == nil || reply.Error.Fields["locked"] == "" || reply.Editor.Values["locked"] != "fixed" {
		t.Fatalf("checkpoint changed disabled input: %+v", reply)
	}
}

func TestRuntimeRowActionsReauthorizeAndBindConfirmationTargets(t *testing.T) {
	var revoked atomic.Bool
	var mutations atomic.Int32
	var actions atomic.Int32
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		screen := componentList()
		screen.List.Selection.Selected = nil
		screen.List.Rows[0].Disabled = revoked.Load()
		return screen, nil
	}, func(context.Context, nativeui.Request) (nativeui.Result, error) {
		actions.Add(1)
		return nativeui.Result{Presentation: &nativeui.Presentation{ID: "confirm", Kind: nativeui.PresentationConfirmation, Title: "Trash?", Commit: true, Choices: []nativeui.PresentationChoice{{ID: "yes", Label: "Trash", Role: "destructive"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}}}, nil
	}, func(_ context.Context, request nativeui.Request, result nativeui.PresentationResult) (nativeui.Result, error) {
		if request.Values["row"] != "a" || result.Choice != "yes" {
			t.Fatalf("confirmation lost original row: %+v %+v", request, result)
		}
		mutations.Add(1)
		return nativeui.Result{}, nil
	})
	runtime, connection, opened := componentRuntime(t, app, nativeui.RuntimeOptions{})
	command := nativeui.Command{Version: 1, Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "trash", Action: "trash", Values: map[string]string{"row": "a"}}
	if reply := runtime.Handle(context.Background(), command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	command.Kind = "complete"
	presented := runtime.Handle(context.Background(), command)
	if presented.Error != nil || presented.PresentationID == "" {
		t.Fatalf("row action did not present: %+v", presented)
	}
	response := nativeui.Command{Version: 1, Kind: "respond", Connection: connection, Instance: opened.Instance, RequestID: "confirm", PresentationID: presented.PresentationID, Choice: "yes", Values: map[string]string{"row": "c"}}
	if reply := runtime.Handle(context.Background(), response); reply.Error == nil || reply.Error.Fields["row"] == "" || mutations.Load() != 0 {
		t.Fatalf("confirmation target swapped: %+v", reply)
	}
	response.Values = nil
	if reply := runtime.Handle(context.Background(), response); reply.Error != nil || mutations.Load() != 1 {
		t.Fatalf("confirmation did not retain row: %+v", reply)
	}
	response.Kind = "complete"
	runtime.Handle(context.Background(), response)
	revoked.Store(true)
	command.Kind, command.RequestID = "action", "revoked"
	if reply := runtime.Handle(context.Background(), command); reply.Error == nil || reply.Error.Fields["row"] == "" || mutations.Load() != 1 || actions.Load() != 1 {
		t.Fatalf("runtime trusted an obsolete row: %+v", reply)
	}
}

func TestRuntimeRestoresOldPartialDraftWithoutLosingAddedControls(t *testing.T) {
	store, err := behavior.NewFileDraftStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	editor, err := behavior.NewEditor("old-instance", "/items", behavior.Values{"title": "Old"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "component-draft", editor.Checkpoint(behavior.Values{"title": "Recovered", "locked": "tampered"})); err != nil {
		t.Fatal(err)
	}
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) { return componentForm(), nil }, func(context.Context, nativeui.Request) (nativeui.Result, error) { return nativeui.Result{}, nil }, nil)
	_, _, opened := componentRuntime(t, app, nativeui.RuntimeOptions{Drafts: store})
	if !opened.Editor.Restored || opened.Editor.Values["title"] != "Recovered" || opened.Editor.Values["pinned"] != "true" || opened.Editor.Values["date"] != "2028-02-29" || opened.Editor.Values["locked"] != "fixed" {
		t.Fatalf("old draft lost added/disabled controls: %+v", opened)
	}
}

func TestRuntimeConfirmationReauthorizesRowsAfterPresentation(t *testing.T) {
	var revoked atomic.Bool
	var mutations atomic.Int32
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		screen := componentList()
		screen.List.Selection.Selected = nil
		screen.List.Rows[0].Disabled = revoked.Load()
		return screen, nil
	}, func(context.Context, nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Presentation: &nativeui.Presentation{ID: "confirm", Kind: nativeui.PresentationConfirmation, Title: "Confirm", Commit: true, Choices: []nativeui.PresentationChoice{{ID: "yes", Label: "Yes"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}}}, nil
	}, func(context.Context, nativeui.Request, nativeui.PresentationResult) (nativeui.Result, error) {
		mutations.Add(1)
		return nativeui.Result{}, nil
	})
	runtime, connection, opened := componentRuntime(t, app, nativeui.RuntimeOptions{})
	command := nativeui.Command{Version: 1, Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "trash", Action: "trash", Values: map[string]string{"row": "a"}}
	if reply := runtime.Handle(context.Background(), command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	command.Kind = "complete"
	presented := runtime.Handle(context.Background(), command)
	revoked.Store(true)
	response := nativeui.Command{Version: 1, Kind: "respond", Connection: connection, Instance: opened.Instance, RequestID: "confirm", PresentationID: presented.PresentationID, Choice: "yes"}
	if reply := runtime.Handle(context.Background(), response); reply.Error == nil || reply.Error.Fields["row"] == "" || mutations.Load() != 0 {
		t.Fatalf("confirmation trusted stale authorization: %+v", reply)
	}
}

func TestRuntimeFileContinuationValidatesControlsWithoutBreakingCancellation(t *testing.T) {
	var calls atomic.Int32
	app, err := nativeui.New("/items", nativeui.Route{Path: "/items", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		screen := componentForm()
		screen.Actions[0].Commit = false
		return screen, nil
	}, Actions: map[string]nativeui.Handle{"save": func(context.Context, nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Files: &nativeui.FileRequest{ID: "import", Commit: true, Pick: &files.Pick{Title: "Import", MaxBytes: files.MaxBytes}}}, nil
	}}, Files: map[string]nativeui.FileRespond{"import": func(_ context.Context, request nativeui.Request, result nativeui.FileResult) (nativeui.Result, error) {
		calls.Add(1)
		if request.Values["pinned"] != "true" || request.Values["date"] != "2028-02-29" {
			t.Fatalf("file result lost mixed controls: %+v", request)
		}
		return nativeui.Result{}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, cancelled := range []bool{false, true} {
		runtime, connection, opened := componentRuntime(t, app, nativeui.RuntimeOptions{})
		command := nativeui.Command{Version: 1, Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "pick", Action: "save"}
		if reply := runtime.Handle(context.Background(), command); reply.Error != nil {
			t.Fatal(reply.Error)
		}
		command.Kind = "complete"
		shown := runtime.Handle(context.Background(), command)
		response := nativeui.Command{Version: 1, Kind: "file-result", Connection: connection, Instance: opened.Instance, RequestID: "import", FileID: shown.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &files.Document{Name: "note.txt", MediaType: "text/plain"}}, Values: map[string]string{"date": "invalid"}}
		if cancelled {
			response.FileResult = &nativeui.FileResult{Status: nativeui.EffectCancelled}
			if reply := runtime.Handle(context.Background(), response); reply.Error != nil || calls.Load() != 1 {
				t.Fatalf("cancellation validated a nonmutation: %+v", reply)
			}
			continue
		}
		if reply := runtime.Handle(context.Background(), response); reply.Error == nil || reply.Error.Fields["date"] == "" || calls.Load() != 0 {
			t.Fatalf("file continuation accepted invalid controls: %+v", reply)
		}
		response.Values["date"] = "2028-02-29"
		if reply := runtime.Handle(context.Background(), response); reply.Error != nil || calls.Load() != 1 {
			t.Fatalf("corrected file continuation failed: %+v", reply)
		}
	}
}

func TestCollectionDefinitionsRejectUnsafeIdentityBoundsAndDestinations(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*nativeui.Screen)
	}{
		{"duplicate rows", func(screen *nativeui.Screen) { screen.List.Rows[2].ID = "a" }},
		{"missing stable ID", func(screen *nativeui.Screen) { screen.List.Rows[0].ID = "" }},
		{"unsafe row ID", func(screen *nativeui.Screen) { screen.List.Rows[0].ID = "a/b" }},
		{"foreign row destination", func(screen *nativeui.Screen) { screen.List.Rows[0].Destination = "https://example.com/" }},
		{"missing row destination", func(screen *nativeui.Screen) {
			screen.List.Selection = nil
			screen.List.Rows[0].LeadingActions = nil
			screen.List.Rows[0].TrailingActions = nil
			screen.List.Rows[0].Destination = ""
		}},
		{"undeclared row action", func(screen *nativeui.Screen) { screen.List.Rows[0].LeadingActions[0].ID = "unregistered" }},
		{"duplicate row actions", func(screen *nativeui.Screen) {
			screen.List.Rows[0].TrailingActions = screen.List.Rows[0].LeadingActions
		}},
		{"destructive full swipe", func(screen *nativeui.Screen) { screen.List.Rows[0].TrailingActions[0].FullSwipe = true }},
		{"unsupported row role", func(screen *nativeui.Screen) { screen.List.Rows[0].TrailingActions[0].Role = "cancel" }},
		{"unsafe action ID", func(screen *nativeui.Screen) { screen.List.Rows[0].LeadingActions[0].ID = "pin\n" }},
		{"negative selection limit", func(screen *nativeui.Screen) { screen.List.Selection.Maximum = -1 }},
		{"excessive selection limit", func(screen *nativeui.Screen) { screen.List.Selection.Maximum = 201 }},
		{"disabled selected row", func(screen *nativeui.Screen) { screen.List.Selection.Selected = []string{"b"} }},
		{"foreign selected row", func(screen *nativeui.Screen) { screen.List.Selection.Selected = []string{"foreign"} }},
		{"duplicate selected row", func(screen *nativeui.Screen) { screen.List.Selection.Selected = []string{"a", "a"} }},
		{"cross-component key collision", func(screen *nativeui.Screen) { screen.List.Selection.Key = screen.List.Filters[0].Key }},
		{"reserved selection key", func(screen *nativeui.Screen) { screen.List.Selection.Key = "row" }},
		{"unsafe search key", func(screen *nativeui.Screen) { screen.List.SearchKey = "search[]" }},
		{"duplicate filter key", func(screen *nativeui.Screen) { screen.List.Filters[0].Key = screen.List.SearchKey }},
		{"paging key collision", func(screen *nativeui.Screen) { screen.List.Paging.Key = screen.List.Selection.Key }},
		{"unsafe page token", func(screen *nativeui.Screen) { screen.List.Paging.Next = "../next" }},
		{"unlabelled paging", func(screen *nativeui.Screen) { screen.List.Paging.Label = "" }},
		{"presentation selection in collection", func(screen *nativeui.Screen) { screen.List.Selection.Multiple = true }},
		{"foreign active tab", func(screen *nativeui.Screen) { screen.ActiveTab = "foreign" }},
		{"duplicate tab ID", func(screen *nativeui.Screen) {
			screen.Tabs = []nativeui.Tab{{ID: "notes", Label: "Notes", Route: "/items"}, {ID: "notes", Label: "Settings", Route: "/settings"}}
			screen.ActiveTab = "notes"
		}},
		{"external tab", func(screen *nativeui.Screen) {
			screen.Tabs = []nativeui.Tab{{ID: "notes", Label: "Notes", Route: "https://example.com/"}}
			screen.ActiveTab = "notes"
		}},
		{"parameterized tab root", func(screen *nativeui.Screen) {
			screen.Tabs = []nativeui.Tab{{ID: "notes", Label: "Notes", Route: "/items/a"}}
			screen.ActiveTab = "notes"
		}},
		{"unsafe tab icon", func(screen *nativeui.Screen) {
			screen.Tabs = []nativeui.Tab{{ID: "notes", Label: "Notes", Route: "/items", Icon: "<script>"}}
			screen.ActiveTab = "notes"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
				screen := componentList()
				test.change(&screen)
				return screen, nil
			}, func(context.Context, nativeui.Request) (nativeui.Result, error) { return nativeui.Result{}, nil }, nil)
			if _, err := app.Render(context.Background(), "/items", nil); err == nil {
				t.Fatal("unsafe definition reached a renderer")
			}
		})
	}
}

func TestFieldDefinitionsValidateOptionsAndCanonicalValues(t *testing.T) {
	for _, field := range []nativeui.Field{
		{Key: "row", Label: "Reserved", Kind: "text"},
		{Key: "field", Label: "Field", Kind: "toggle", Value: "yes"},
		{Key: "field", Label: "Field", Kind: "date", Value: "2026-11-31"},
		{Key: "field", Label: "Field", Kind: "time", Value: "9:00"},
		{Key: "field", Label: "Field", Kind: "select"},
		{Key: "field", Label: "Field", Kind: "text", Options: []nativeui.Option{{Value: "a", Label: "A"}}},
		{Key: "field", Label: "Field", Kind: "select", Options: []nativeui.Option{{Value: "a", Label: "A"}, {Value: "a", Label: "Again"}}},
		{Key: "field", Label: "Field", Kind: "select", Options: []nativeui.Option{{Value: "unsafe value", Label: "A"}}},
		{Key: "field", Label: "Field", Kind: "select", Value: "a", Options: []nativeui.Option{{Value: "a", Label: "A", Disabled: true}}},
	} {
		app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Field", Form: &nativeui.Form{Fields: []nativeui.Field{field}}}, nil
		}, func(context.Context, nativeui.Request) (nativeui.Result, error) { return nativeui.Result{}, nil }, nil)
		if _, err := app.Render(context.Background(), "/items", nil); err == nil {
			t.Errorf("accepted invalid field: %+v", field)
		}
	}
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "New required field", Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "title", Label: "Title", Kind: "text", Required: true}}}}, nil
	}, func(context.Context, nativeui.Request) (nativeui.Result, error) { return nativeui.Result{}, nil }, nil)
	if _, err := app.Render(context.Background(), "/items", nil); err != nil {
		t.Fatalf("empty required controls must remain renderable for new records: %v", err)
	}
}

func TestDispatchBoundsIncludeDefaultedFields(t *testing.T) {
	var calls atomic.Int32
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) { return componentForm(), nil }, func(context.Context, nativeui.Request) (nativeui.Result, error) {
		calls.Add(1)
		return nativeui.Result{}, nil
	}, nil)
	values := make(map[string]string)
	for index := 0; index < 128; index++ {
		values[fmt.Sprintf("extra%d", index)] = "value"
	}
	if _, err := app.Dispatch(context.Background(), "/items", "save", values); !errors.Is(err, nativeui.ErrInvalidValues) || calls.Load() != 0 {
		t.Fatalf("defaulted values bypassed submission bounds: %v", err)
	}
}

func TestRuntimeCurrentScreenActionRevocationIsNotAnUnknownWrite(t *testing.T) {
	var revoked atomic.Bool
	var calls atomic.Int32
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		screen := componentForm()
		if revoked.Load() {
			screen.Actions = nil
		}
		return screen, nil
	}, func(context.Context, nativeui.Request) (nativeui.Result, error) {
		calls.Add(1)
		return nativeui.Result{}, nil
	}, nil)
	runtime, connection, opened := componentRuntime(t, app, nativeui.RuntimeOptions{})
	revoked.Store(true)
	reply := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "save", Action: "save", Values: map[string]string{"title": "Valid"}})
	if reply.Error == nil || reply.Error.Code != "forbidden" || reply.Error.Fields["action"] == "" || calls.Load() != 0 {
		t.Fatalf("revoked action was dispatched or marked uncertain: %+v", reply)
	}
}

func TestRuntimeRejectsCollectionTamperingAndSelectionOverflow(t *testing.T) {
	var calls atomic.Int32
	app := componentApp(t, func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		screen := componentList()
		screen.List.Selection.Maximum = 1
		return screen, nil
	}, func(context.Context, nativeui.Request) (nativeui.Result, error) {
		calls.Add(1)
		return nativeui.Result{}, nil
	}, nil)
	runtime, connection, opened := componentRuntime(t, app, nativeui.RuntimeOptions{})
	for index, test := range []struct{ key, value string }{{"selected", `["a","c"]`}, {"selected", `["b"]`}, {"selected", `["foreign"]`}, {"search", "changed"}, {"page", "2"}, {"tag", "work"}} {
		reply := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: fmt.Sprint(index), Action: "bulk", Values: map[string]string{test.key: test.value}})
		if reply.Error == nil || reply.Error.Code != "validation" || reply.Error.Fields[test.key] == "" || calls.Load() != 0 {
			t.Fatalf("runtime admitted collection tampering %s=%s: %+v", test.key, test.value, reply)
		}
	}
	if _, err := app.Render(context.Background(), "/items?page=2", nil); err == nil {
		t.Fatal("a repeated Next token allowed paging to loop")
	}
}
