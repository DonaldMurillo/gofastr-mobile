package screens

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func open(t *testing.T, directory string) *nativeui.App {
	t.Helper()
	store, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func savedID(t *testing.T, response nativeui.Response) string {
	t.Helper()
	if response.Saved == nil || len(response.Saved.Route) <= len("/items/") {
		t.Fatalf("save returned no canonical route: %+v", response.Result)
	}
	return response.Saved.Route[len("/items/"):]
}

func TestEveryRouteRendersAndEveryActionDispatches(t *testing.T) {
	ctx := context.Background()
	app := open(t, t.TempDir())
	for _, route := range []string{Root, "/items/new", "/about"} {
		if _, err := app.Render(ctx, route, nil); err != nil {
			t.Fatalf("render %s: %v", route, err)
		}
	}
	for action, route := range map[string]string{"new": "/items/new", "about": "/about"} {
		response, err := app.Dispatch(ctx, Root, action, nil)
		if err != nil || response.Navigation == nil || response.Navigation.Route != route {
			t.Fatalf("%s: %+v %v", action, response, err)
		}
	}
	saved, err := app.Dispatch(ctx, "/items/new", "save", map[string]string{"title": "First", "body": "Line one\nLine two"})
	if err != nil {
		t.Fatal(err)
	}
	id := savedID(t, saved)
	if _, err := app.Render(ctx, "/items/"+id, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Dispatch(ctx, "/items/"+id, "delete", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Render(ctx, "/items/"+id, nil); err == nil {
		t.Fatal("a deleted item still renders")
	}
}

func TestEmptyTitleIsRejectedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	app := open(t, t.TempDir())
	_, err := app.Dispatch(ctx, "/items/new", "save", map[string]string{"title": "   ", "body": "kept in the draft"})
	var action *nativeui.ActionError
	if !errors.As(err, &action) {
		t.Fatalf("want a field error, got %v", err)
	}
	list, err := app.Render(ctx, Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.List.Rows) != 0 {
		t.Fatalf("a rejected save wrote %d items", len(list.List.Rows))
	}
}

func TestSavedItemsSurviveReopeningTheStore(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	first := open(t, directory)
	saved, err := first.Dispatch(ctx, "/items/new", "save", map[string]string{"title": "Groceries", "body": "milk"})
	if err != nil {
		t.Fatal(err)
	}
	id := savedID(t, saved)
	edited, err := first.Dispatch(ctx, "/items/"+id, "save", map[string]string{"title": "Groceries", "body": "milk, eggs"})
	if err != nil {
		t.Fatal(err)
	}
	newID := savedID(t, edited)

	reopened := open(t, directory)
	list, err := reopened.Render(ctx, Root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.List.Rows) != 1 || list.List.Rows[0].Title != "Groceries" || list.List.Rows[0].Destination != "/items/"+newID {
		t.Fatalf("after reopening: %+v", list.List.Rows)
	}
	screen, err := reopened.Render(ctx, "/items/"+newID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if body := screen.Form.Fields[1].Value; body != "milk, eggs" {
		t.Fatalf("edited body = %q", body)
	}
}
