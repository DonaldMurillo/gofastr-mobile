package nativeui_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func textComponentDefinition() extension.Definition {
	manifest := extension.Manifest{Name: "example.text/v1", Kind: extension.ComponentKind, Capabilities: []string{"example.text/v1"}, IOS: extension.IOS{Factory: "ExampleText", Sources: []string{"Text.swift"}}, WebView: extension.WebView{Script: "text.js"}}
	content, _ := json.Marshal(manifest)
	return extension.Definition{Manifest: manifest, Assets: fstest.MapFS{extension.ManifestName: {Data: content}, "Text.swift": {Data: []byte("import UIKit")}, "text.js": {Data: []byte("void 0;")}}, ValidateComponent: func(component extension.Component) error {
		var payload struct {
			Text string `json:"text"`
		}
		return extension.DecodeComponent(component.Payload, &payload)
	}}
}

func renderingComponentApp(t *testing.T, components []extension.Component) *nativeui.App {
	t.Helper()
	app, err := nativeui.NewWithExtensions("/", []extension.Definition{textComponentDefinition()}, nativeui.Route{Path: "/", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Custom Go screen", Components: components}, nil
	}}, nativeui.Route{Path: "/detail", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Detail", Text: "Authorized"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestRenderingComponentProtocolRequiresActualAdapter(t *testing.T) {
	component := extension.Component{ID: "body", Name: "example.text/v1", Payload: `{"text":"Go-authored content"}`, Destinations: map[string]string{"detail": "/detail"}}
	for _, support := range []nativeui.Support{"", nativeui.SupportUnavailable, nativeui.SupportFallback, nativeui.SupportAvailable} {
		t.Run(string(support), func(t *testing.T) {
			runtime, err := nativeui.NewRuntime(renderingComponentApp(t, []extension.Component{component}), nativeui.RuntimeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			capabilities := map[nativeui.Capability]nativeui.Support{nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable}
			if support != "" {
				capabilities["example.text/v1"] = support
			}
			hello := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: capabilities}})
			if hello.Error != nil {
				t.Fatal(hello.Error)
			}
			reply := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "open", Connection: hello.Connection, Route: "/"})
			if support != nativeui.SupportAvailable {
				if reply.Error == nil || reply.Error.Code != "unsupported" {
					t.Fatalf("missing adapter admitted: %+v", reply)
				}
			} else if reply.Error != nil || reply.Screen == nil || len(reply.Screen.Components) != 1 {
				t.Fatalf("compiled component failed: %+v", reply)
			}
		})
	}
}

func TestRenderingComponentDestinationsAndIdentityAreValidated(t *testing.T) {
	valid := extension.Component{ID: "body", Name: "example.text/v1", Payload: `{"text":"Body"}`}
	for _, components := range [][]extension.Component{
		{valid, valid},
		{{ID: "body", Name: valid.Name, Payload: valid.Payload, Destinations: map[string]string{"detail": "https://external.example/"}}},
		{{ID: "body", Name: valid.Name, Payload: valid.Payload, Destinations: map[string]string{"detail": "/unknown"}}},
		{{ID: "body", Name: "missing/v1", Payload: valid.Payload}},
		{{ID: "body", Name: valid.Name, Payload: `{"text":"` + strings.Repeat("x", extension.MaxComponentPayload) + `"}`}},
	} {
		if _, err := renderingComponentApp(t, components).Render(context.Background(), "/", nil); err == nil {
			t.Fatal("invalid component screen rendered")
		}
	}
}

func TestRenderingComponentCountAndCombinedPayloadAreBounded(t *testing.T) {
	var components []extension.Component
	for index := range 17 {
		components = append(components, extension.Component{ID: fmt.Sprintf("item-%d", index), Name: "example.text/v1", Payload: `{"text":"Small"}`})
	}
	if _, err := renderingComponentApp(t, components[:16]).Render(context.Background(), "/", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := renderingComponentApp(t, components).Render(context.Background(), "/", nil); err == nil {
		t.Fatal("component count was not bounded")
	}
	for index := range 2 {
		components[index].Payload = `{"text":"` + strings.Repeat("x", extension.MaxComponentPayload/2) + `"}`
	}
	if _, err := renderingComponentApp(t, components[:2]).Render(context.Background(), "/", nil); err == nil {
		t.Fatal("combined presentation payload was not bounded")
	}
}

func TestRenderingRequirementsDoNotMutateApplicationDefinitions(t *testing.T) {
	requirements := []nativeui.Requirement{{Capability: "example.text/v1", AllowFallback: true}}
	app, err := nativeui.NewWithExtensions("/", []extension.Definition{textComponentDefinition()}, nativeui.Route{Path: "/", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Text", Components: []extension.Component{{ID: "body", Name: "example.text/v1", Payload: `{"text":"Read only"}`}}, Requires: requirements}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	screen, err := app.Render(context.Background(), "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !requirements[0].AllowFallback || screen.Requires[0].AllowFallback {
		t.Fatal("requirements either mutated application memory or admitted a silent fallback")
	}
}
