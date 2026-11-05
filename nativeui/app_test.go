package nativeui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLocalScreenAndActionBoundary(t *testing.T) {
	app, err := New("/items", Route{Path: "/items", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Items", List: &List{Rows: []Row{}}, Actions: []Action{{ID: "open", Label: "Open"}}}, nil
	}, Actions: map[string]Handle{"open": func(context.Context, Request) (Result, error) {
		return Result{Navigation: &Navigation{Kind: "push", Route: "/items/42?source=list"}}, nil
	}}}, Route{Path: "/items/{id}", Screen: func(_ context.Context, request Request) (Screen, error) {
		return Screen{Title: request.Params["id"], Text: request.Values["source"]}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	encoded, err := app.DispatchJSON(ctx, app.Root(), "open", "{}")
	if err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal([]byte(encoded), &response); err != nil {
		t.Fatal(err)
	}
	if response.Version != 1 || response.Screen.Title != "42" || response.Screen.Text != "list" || response.Navigation.Kind != "push" {
		t.Fatalf("wrong native response: %+v", response)
	}
	for _, destination := range []string{"https://example.com/items", "//example.com/items", "/missing", "/items#fragment", "/items/../42", "/items/%2f42", "not-a-route"} {
		if _, err := app.RenderJSON(ctx, destination, "{}"); err == nil {
			t.Errorf("accepted route %q", destination)
		}
	}
	for _, payload := range []string{"null", "[]", `{"field":42}`, "{broken", `{} {}`} {
		if _, err := app.RenderJSON(ctx, app.Root(), payload); err == nil {
			t.Errorf("accepted values %q", payload)
		}
	}
	if _, err := app.DispatchJSON(ctx, app.Root(), "unknown", "{}"); err == nil {
		t.Fatal("accepted unregistered action")
	}
}

func TestEmptyListIsAnArrayOnTheNativeWire(t *testing.T) {
	app, err := New("/", Route{Path: "/", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Empty", List: &List{}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := app.RenderJSON(context.Background(), "/", "{}")
	if err != nil || !strings.Contains(encoded, `"rows":[]`) {
		t.Fatalf("native list cannot decode: %s %v", encoded, err)
	}
}

func TestRejectUnsupportedScreenDefinitions(t *testing.T) {
	for _, screen := range []Screen{
		{Title: "Empty"},
		{Title: "Ambiguous", List: &List{}, Text: "Text"},
		{Title: "Empty form", Form: &Form{}},
		{Title: "Unknown action", Text: "Text", Actions: []Action{{ID: "missing", Label: "Missing"}}},
		{Title: "Missing label", Form: &Form{Fields: []Field{{Key: "field", Kind: "text"}}}},
		{Title: "Duplicate field", Form: &Form{Fields: []Field{{Key: "same", Label: "First", Kind: "text"}, {Key: "same", Label: "Second", Kind: "text"}}}},
		{Title: "Unknown kind", Form: &Form{Fields: []Field{{Key: "field", Label: "Field", Kind: "custom"}}}},
	} {
		t.Run(screen.Title, func(t *testing.T) {
			app, err := New("/", Route{Path: "/", Screen: func(context.Context, Request) (Screen, error) { return screen, nil }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.RenderJSON(context.Background(), "/", "{}"); err == nil {
				t.Fatal("unsupported definition reached native renderer")
			}
		})
	}
}

func TestRejectUnknownPlatformIcon(t *testing.T) {
	app, err := New("/", Route{Path: "/", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Action", Text: "Content", Actions: []Action{{ID: "action", Label: "Action", Icon: "platform-specific-symbol"}}}, nil
	}, Actions: map[string]Handle{"action": func(context.Context, Request) (Result, error) { return Result{}, nil }}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.RenderJSON(context.Background(), "/", "{}"); err == nil {
		t.Fatal("platform-specific icon escaped the mobile definition contract")
	}
}
