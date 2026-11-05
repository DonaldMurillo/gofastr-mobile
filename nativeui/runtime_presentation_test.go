package nativeui

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
)

func TestPresentationCommandFlow(t *testing.T) {
	runtime, connection, calls := presentationFixture(t)
	command := Command{Kind: "open", Connection: connection, Route: "/showcase"}
	opened := presentationCommand(t, runtime, command)
	command.Instance, command.Kind, command.Action, command.RequestID = opened.Instance, "action", "choose", "open-selection"
	if reply := presentationCommand(t, runtime, command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	command.Kind = "complete"
	shown := presentationCommand(t, runtime, command)
	if shown.Error != nil || shown.PresentationID == "" || shown.Response.Presentation == nil {
		t.Fatalf("not presented: %+v", shown)
	}
	if replay := presentationCommand(t, runtime, command); replay.PresentationID != shown.PresentationID {
		t.Fatal("completion duplicated presentation")
	}
	command.Kind, command.PresentationID, command.Choice, command.RequestID = "respond", shown.PresentationID, "apply", "apply-selection"
	for _, selected := range [][]string{nil, {"disabled"}, {"forged"}, {"first", "first"}} {
		command.Selected = selected
		if reply := presentationCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "validation" {
			t.Fatalf("accepted invalid selection: %+v", reply)
		}
	}
	command.Selected = []string{"second", "first"}
	applied := presentationCommand(t, runtime, command)
	if applied.Error != nil || applied.Response.Announcement != "first,second" || calls.Load() != 1 {
		t.Fatalf("apply: %+v, %d calls", applied, calls.Load())
	}
	if replay := presentationCommand(t, runtime, command); replay.Error != nil || calls.Load() != 1 {
		t.Fatal("response executed twice")
	}
	command.Kind = "complete"
	if reply := presentationCommand(t, runtime, command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	command.Kind, command.RequestID = "respond", "stale-response"
	if reply := presentationCommand(t, runtime, command); reply.Error == nil {
		t.Fatal("accepted stale response")
	}
	command.Kind, command.RequestID = "complete", "open-selection"
	if reply := presentationCommand(t, runtime, command); reply.Response.Presentation != nil || reply.PresentationID != "" {
		t.Fatal("replayed closed presentation")
	}
}

func TestPresentationCancellationAndWindowCoordination(t *testing.T) {
	runtime, connection, calls := presentationFixture(t)
	open := Command{Kind: "open", Connection: connection, Route: "/showcase"}
	first, second := presentationCommand(t, runtime, open), presentationCommand(t, runtime, open)
	show := Command{Connection: connection, Instance: first.Instance, Kind: "action", Action: "choose", RequestID: "first"}
	presentationCommand(t, runtime, show)
	show.Kind = "complete"
	shown := presentationCommand(t, runtime, show)
	show.Instance, show.Kind, show.RequestID = second.Instance, "action", "second"
	presentationCommand(t, runtime, show)
	show.Kind = "complete"
	if reply := presentationCommand(t, runtime, show); reply.Error == nil || reply.Error.Code != "pending" {
		t.Fatal("two foreground presentations admitted")
	}
	cancel := Command{Kind: "respond", Connection: connection, Instance: first.Instance, RequestID: "cancel", PresentationID: shown.PresentationID, Choice: "cancel", Selected: []string{"untrusted"}}
	if reply := presentationCommand(t, runtime, cancel); reply.Error != nil || calls.Load() != 0 {
		t.Fatal("cancel invoked continuation")
	}
	cancel.Kind = "complete"
	presentationCommand(t, runtime, cancel)
	if reply := presentationCommand(t, runtime, show); reply.Error != nil || reply.PresentationID == "" {
		t.Fatal("cancel did not release presenter")
	}
	closed := Command{Kind: "close", Connection: connection, Instance: second.Instance}
	presentationCommand(t, runtime, closed)
	show.Instance, show.RequestID, show.Kind = first.Instance, "third", "action"
	presentationCommand(t, runtime, show)
	show.Kind = "complete"
	if reply := presentationCommand(t, runtime, show); reply.Error != nil || reply.PresentationID == "" {
		t.Fatal("close leaked presenter")
	}
}

func TestPresentationUnknownWriteIsNotRetried(t *testing.T) {
	runtime, connection, calls := presentationFixture(t)
	original := runtime.app.routes[0].Actions["choose"]
	runtime.app.routes[0].Actions["choose"] = func(ctx context.Context, request Request) (Result, error) {
		result, err := original(ctx, request)
		result.Presentation.Commit = true
		return result, err
	}
	runtime.app.routes[0].Presentations["choose"] = func(context.Context, Request, PresentationResult) (Result, error) {
		calls.Add(1)
		return Result{}, errors.New("write may have completed before response loss")
	}
	opened := presentationCommand(t, runtime, Command{Kind: "open", Connection: connection, Route: "/showcase"})
	command := Command{Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "show", Action: "choose"}
	presentationCommand(t, runtime, command)
	command.Kind = "complete"
	shown := presentationCommand(t, runtime, command)
	command.Kind, command.PresentationID, command.Choice, command.RequestID, command.Selected = "respond", shown.PresentationID, "apply", "uncertain-write", []string{"first"}
	for attempt := 0; attempt < 2; attempt++ {
		if reply := presentationCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "unknown" {
			t.Fatalf("uncertain result: %+v", reply)
		}
	}
	command.Kind = "complete"
	if reply := presentationCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "unknown" {
		t.Fatal("settlement hid unknown outcome")
	}
	command.Kind, command.RequestID, command.Action = "action", "retry", "choose"
	if reply := presentationCommand(t, runtime, command); reply.Error == nil {
		t.Fatal("uncertain screen allowed blind retry")
	}
	if calls.Load() != 1 {
		t.Fatalf("write ran %d times", calls.Load())
	}
}

func TestPresentationRequiresAdvertisedSupport(t *testing.T) {
	runtime, connection, _ := presentationFixture(t)
	runtime.connections[connection].contract.Capabilities[CapabilityPresentation] = SupportUnavailable
	opened := presentationCommand(t, runtime, Command{Kind: "open", Connection: connection, Route: "/showcase"})
	command := Command{Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "unsupported", Action: "choose"}
	presentationCommand(t, runtime, command)
	command.Kind = "complete"
	if reply := presentationCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "unsupported" || reply.Response.Presentation != nil {
		t.Fatalf("unsupported presentation was not explicit: %+v", reply)
	}
}

func presentationFixture(t *testing.T) (*Runtime, string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	app, err := New("/showcase", Route{Path: "/showcase", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Showcase", Text: "Go behaviors", Actions: []Action{{ID: "choose", Label: "Choose"}}}, nil
	}, Actions: map[string]Handle{"choose": func(context.Context, Request) (Result, error) {
		return Result{Presentation: &Presentation{ID: "choose", Kind: PresentationSheet, Title: "Choose", Dismissible: true,
			Choices:   []PresentationChoice{{ID: "apply", Label: "Apply"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}},
			Selection: &Selection{Multiple: true, Min: 1, Max: 2, Options: []SelectionOption{{ID: "first", Label: "First"}, {ID: "second", Label: "Second"}, {ID: "disabled", Label: "Unavailable", Disabled: true}}}}}, nil
	}}, Presentations: map[string]Respond{"choose": func(_ context.Context, _ Request, result PresentationResult) (Result, error) {
		calls.Add(1)
		if !slices.Equal(result.Selected, []string{"first", "second"}) {
			return Result{Announcement: "other"}, nil
		}
		return Result{Announcement: "first,second"}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(app, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hello := presentationCommand(t, runtime, Command{Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[Capability]Support{CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, CapabilityPresentation: SupportAvailable}}})
	if hello.Error != nil {
		t.Fatal(hello.Error)
	}
	return runtime, hello.Connection, &calls
}

func presentationCommand(t *testing.T, runtime *Runtime, command Command) Reply {
	t.Helper()
	command.Version = ProtocolVersion
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.CommandJSON(context.Background(), string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	var reply Reply
	if err := json.Unmarshal([]byte(result), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}
