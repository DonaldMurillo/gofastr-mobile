package nativeui

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr-mobile/device/files"
)

func TestFilesCapabilityAndUnknownMutationThroughCommands(t *testing.T) {
	var calls atomic.Int32
	app, err := New("/files", Route{Path: "/files", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Files", Text: "Choose one", Actions: []Action{{ID: "pick", Label: "Pick"}}}, nil
	}, Actions: map[string]Handle{"pick": func(context.Context, Request) (Result, error) {
		return Result{Files: &FileRequest{ID: "pick", Commit: true, Pick: &files.Pick{Title: "Choose", MaxBytes: files.MaxBytes}}}, nil
	}}, Files: map[string]FileRespond{"pick": func(context.Context, Request, FileResult) (Result, error) {
		calls.Add(1)
		return Result{}, errors.New("write response lost")
	}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(app, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hello := Command{Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[Capability]Support{CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable}}}
	unavailable := presentationCommand(t, runtime, hello)
	opened := presentationCommand(t, runtime, Command{Kind: "open", Connection: unavailable.Connection, Route: "/files"})
	command := Command{Kind: "action", Connection: unavailable.Connection, Instance: opened.Instance, RequestID: "unsupported", Action: "pick"}
	presentationCommand(t, runtime, command)
	command.Kind = "complete"
	if reply := presentationCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "unsupported" || reply.Response.Files != nil {
		t.Fatalf("unsupported file task: %+v", reply)
	}
	hello.Hello.Capabilities[CapabilityFiles] = SupportFallback
	available := presentationCommand(t, runtime, hello)
	opened = presentationCommand(t, runtime, Command{Kind: "open", Connection: available.Connection, Route: "/files"})
	command = Command{Kind: "action", Connection: available.Connection, Instance: opened.Instance, RequestID: "start", Action: "pick"}
	presentationCommand(t, runtime, command)
	command.Kind = "complete"
	shown := presentationCommand(t, runtime, command)
	if shown.FileID == "" || shown.Error != nil {
		t.Fatalf("fallback did not issue task: %+v", shown)
	}
	command = Command{Kind: "file-result", Connection: available.Connection, Instance: opened.Instance, RequestID: "result", FileID: shown.FileID, FileResult: &FileResult{Status: EffectCompleted, File: &files.Document{Name: "empty.txt", MediaType: "text/plain"}}}
	for attempt := 0; attempt < 2; attempt++ {
		if reply := presentationCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "unknown" {
			t.Fatalf("uncertain file mutation: %+v", reply)
		}
	}
	command.Kind = "complete"
	if reply := presentationCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "unknown" {
		t.Fatal("settlement hid unknown file outcome")
	}
	command.Kind, command.RequestID, command.Action = "action", "blind-retry", "pick"
	if reply := presentationCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "pending" {
		t.Fatal("unknown mutation allowed blind retry")
	}
	if calls.Load() != 1 {
		t.Fatalf("file mutation ran %d times", calls.Load())
	}
}
