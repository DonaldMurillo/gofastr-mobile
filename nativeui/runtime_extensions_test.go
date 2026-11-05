package nativeui

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension"
)

func testFileExtension() extension.Definition {
	manifest := extension.Manifest{Name: "example.document/v1", Capabilities: []string{"example.document/v1"}, IOS: extension.IOS{Factory: "ExampleDocument", Sources: []string{"Adapter.swift"}}, WebView: extension.WebView{Script: "adapter.js"}}
	content, _ := json.Marshal(manifest)
	return extension.Definition{Manifest: manifest, Assets: fstest.MapFS{extension.ManifestName: &fstest.MapFile{Data: content}, "Adapter.swift": &fstest.MapFile{Data: []byte("import UIKit")}, "adapter.js": &fstest.MapFile{Data: []byte("(() => {})();")}},
		Validate: func(operation extension.FileOperation) (string, error) {
			var request struct {
				Title string `json:"title"`
			}
			if err := extension.Decode(operation.Payload, &request); err != nil {
				return "", err
			}
			if request.Title == "" {
				return "", errors.New("title required")
			}
			return "example.document/v1", nil
		}, Accept: func(_ extension.FileOperation, document files.Document) error {
			if document.MediaType != "text/plain" {
				return errors.New("text required")
			}
			return nil
		},
	}
}

func TestIndependentExtensionThroughCommandBoundary(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "unknown"}[unknown], func(t *testing.T) {
			var writes atomic.Int32
			operation := &extension.FileOperation{Name: "example.document/v1", Payload: `{"title":"Import document"}`}
			route := Route{Path: "/documents", Screen: func(context.Context, Request) (Screen, error) {
				return Screen{Title: "Documents", Text: "Import", Actions: []Action{{ID: "import", Label: "Import"}}, Requires: []Requirement{{Capability: "example.document/v1"}}}, nil
			},
				Actions: map[string]Handle{"import": func(context.Context, Request) (Result, error) {
					return Result{Files: &FileRequest{ID: "attach", Commit: true, Extension: operation}}, nil
				}},
				Files: map[string]FileRespond{"attach": func(context.Context, Request, FileResult) (Result, error) {
					writes.Add(1)
					if unknown {
						return Result{}, errors.New("storage outcome unknown")
					}
					return Result{Announcement: "Attached"}, nil
				}},
			}
			app, err := NewWithExtensions("/documents", []extension.Definition{testFileExtension()}, route)
			if err != nil {
				t.Fatal(err)
			}
			runtime, _ := NewRuntime(app, RuntimeOptions{})
			hello := Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[Capability]Support{CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, "example.document/v1": SupportAvailable}}
			if _, err := Negotiate(hello); err == nil {
				t.Fatal("standalone negotiation accepted unregistered capability")
			}
			connected := presentationCommand(t, runtime, Command{Kind: "hello", Hello: &hello})
			if connected.Error != nil {
				t.Fatal(connected.Error)
			}
			opened := presentationCommand(t, runtime, Command{Kind: "open", Connection: connected.Connection, Route: "/documents"})
			if opened.Error != nil {
				t.Fatal(opened.Error)
			}
			start := Command{Kind: "action", Connection: connected.Connection, Instance: opened.Instance, RequestID: "import-1", Action: "import"}
			presentationCommand(t, runtime, start)
			start.Kind = "complete"
			shown := presentationCommand(t, runtime, start)
			if shown.Error != nil || shown.FileID == "" {
				t.Fatalf("extension not issued: %+v", shown)
			}
			operation.Payload = `{"title":"Mutated after issuance"}`
			if shown.Response.Files.Extension.Payload != `{"title":"Import document"}` {
				t.Fatal("request was not frozen")
			}
			finish := Command{Kind: "file-result", Connection: connected.Connection, Instance: opened.Instance, RequestID: "result-1", FileID: shown.FileID, FileResult: &FileResult{Status: EffectCompleted, File: &files.Document{Name: "image.jpg", MediaType: "image/jpeg", Data: []byte("no")}}}
			if reply := presentationCommand(t, runtime, finish); reply.Error == nil || reply.Error.Code != "validation" {
				t.Fatal("invalid extension result admitted")
			}
			finish.FileResult.File = &files.Document{Name: "document.txt", MediaType: "text/plain", Data: []byte("content")}
			presentationCommand(t, runtime, finish)
			finish.Kind = "complete"
			settled := presentationCommand(t, runtime, finish)
			if unknown && (settled.Error == nil || settled.Error.Code != "unknown") {
				t.Fatalf("unknown outcome lost: %+v", settled)
			}
			if !unknown && (settled.Error != nil || settled.Response.Announcement != "Attached") {
				t.Fatalf("completion lost: %+v", settled)
			}
			finish.Kind = "file-result"
			presentationCommand(t, runtime, finish)
			if writes.Load() != 1 {
				t.Fatal("extension continuation replayed")
			}
			plain, _ := New("/documents", route)
			if _, err := plain.Dispatch(context.Background(), "/documents", "import", nil); err == nil {
				t.Fatal("unregistered extension issued")
			}
		})
	}
}
