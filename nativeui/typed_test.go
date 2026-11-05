package nativeui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestTypedAPIsPreserveWireAndServiceErrors(t *testing.T) {
	cause := errors.New("application validation")
	app, err := New("/items", Route{Path: "/items", Screen: func(_ context.Context, request Request) (Screen, error) {
		return Screen{Title: "Items", Text: "Content", Actions: []Action{{ID: "save", Label: "Save"}}}, nil
	}, Actions: map[string]Handle{"save": func(_ context.Context, request Request) (Result, error) {
		if request.Values["title"] == "invalid" {
			return Result{}, fmt.Errorf("save: %w", &ActionError{Code: "validation", Message: "A title is required", Fields: map[string]string{"title": "Required"}, Err: cause})
		}
		return Result{ClearDraft: true, Announcement: request.Values["title"]}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	values := map[string]string{"title": "Typed value"}
	screen, err := app.Render(ctx, "/items?search=query", values)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := app.RenderJSON(ctx, "/items?search=query", `{"title":"Typed value"}`)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil || !reflect.DeepEqual(decoded, Response{Version: 1, Screen: &screen}) {
		t.Fatalf("typed render changed wire v1: %s %v", encoded, err)
	}
	response, err := app.Dispatch(ctx, "/items", "save", values)
	if err != nil || !response.ClearDraft || response.Announcement != values["title"] {
		t.Fatalf("typed dispatch: %+v %v", response, err)
	}
	_, err = app.Dispatch(ctx, "/items", "save", map[string]string{"title": "invalid"})
	var failure *ActionError
	if !errors.As(err, &failure) || !errors.Is(err, cause) || failure.Fields["title"] != "Required" {
		t.Fatalf("service error chain lost: %v", err)
	}
	metadata := app.Routes()
	metadata[0].Path, metadata[0].Actions[0] = "/external", "privileged"
	resolved, err := app.Resolve("/items?search=test")
	if err != nil || resolved.Path != "/items" || !reflect.DeepEqual(resolved.Actions, []string{"save"}) {
		t.Fatalf("metadata changed registry: %+v %v", resolved, err)
	}
}

func TestTypedDestinationValidation(t *testing.T) {
	app, err := New("/items", Route{Path: "/items", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Items", List: &List{Rows: []Row{{Title: "Unsafe", Destination: "https://external.example/"}}}}, nil
	}}, Route{Path: "/items/{id}", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Item", Text: "Safe"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"/items/.", "/items/..", "/items/%2e%2e", "/items/%00", "/items/%5cpath", "/items?bad=%zz", "https://external.example/items", "//external.example/items"} {
		if _, err := app.Resolve(destination); err == nil {
			t.Errorf("accepted unsafe destination %q", destination)
		}
	}
	if _, err := app.Render(context.Background(), "/items", nil); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("unsafe row escaped validation: %v", err)
	}
	values := make(map[string]string)
	for index := 0; index < 129; index++ {
		values[fmt.Sprint(index)] = "value"
	}
	if _, err := app.Render(context.Background(), "/items/42", values); err == nil {
		t.Fatal("typed inputs bypassed field limits")
	}
}
