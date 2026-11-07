package nativeui_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestDescribeAndRoutePatternRunNoApplicationCode(t *testing.T) {
	fixture := newTraceFixture(t)
	described := fixture.app.Describe()
	if len(described) != 3 || described[1].Path != "/items/{id}" || strings.Join(described[1].Actions, ",") != "block,check,fake-definition,import,save,secret" ||
		strings.Join(described[1].Files, ",") != "import" || strings.Join(described[1].Services, ",") != "secret" || described[0].Files != nil {
		t.Fatalf("describe: %+v", described)
	}
	described[1].Actions[0] = "mutated"
	if fixture.app.Describe()[1].Actions[0] != "block" {
		t.Fatal("Describe returned registry storage")
	}
	for destination, want := range map[string]string{"/items": "/items", "/items/" + canary + "?q=x": "/items/{id}", "/broken": "/broken"} {
		if pattern, ok := fixture.app.RoutePattern(destination); !ok || pattern != want {
			t.Fatalf("RoutePattern(%q) = %q %t", destination, pattern, ok)
		}
	}
	for _, destination := range []string{"/missing", "https://example.com/items", "items", ""} {
		if pattern, ok := fixture.app.RoutePattern(destination); ok {
			t.Fatalf("RoutePattern(%q) matched %q", destination, pattern)
		}
	}
	if fixture.calls.Load() != 0 {
		t.Fatalf("metadata ran %d handlers", fixture.calls.Load())
	}
}

func TestOutlineDropsUnregisteredTokensAndValues(t *testing.T) {
	fixture := newTraceFixture(t)
	screen, err := fixture.app.Render(context.Background(), "/items?view=all", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Smuggle values through every identifier slot after Render validated it.
	screen.Actions = append(screen.Actions, nativeui.Action{ID: canary, Label: canary, Icon: nativeui.Icon(canary)})
	screen.List.Rows = append(screen.List.Rows, nativeui.Row{ID: canary, Title: canary, Destination: "/elsewhere/" + canary, TrailingActions: []nativeui.RowAction{{ID: canary}}})
	screen.Requires = append(screen.Requires, nativeui.Requirement{Capability: nativeui.Capability(canary)}, nativeui.Requirement{Capability: nativeui.CapabilityFiles})
	screen.Tabs = []nativeui.Tab{{ID: "t", Label: canary, Route: "/items/" + canary}}
	outline := nativeui.Outline(fixture.app, screen)
	encoded, _ := json.Marshal(outline)
	if strings.Contains(string(encoded), canary) {
		t.Fatalf("outline leaked: %s", encoded)
	}
	if outline.Route != "/items" || outline.Content != "list" || outline.List.Rows != 2 || strings.Join(outline.List.RowActions, ",") != "archive" ||
		strings.Join(outline.Destinations, ",") != "/items/{id}" || len(outline.Requires) != 1 || len(outline.Tabs) != 0 || len(outline.List.Filters) != 1 || outline.List.Filters[0].Options != 3 {
		t.Fatalf("outline: %s", encoded)
	}
	if nativeui.Outline(nil, nativeui.Screen{Text: "x"}).Content != "text" {
		t.Fatal("nil app outline")
	}
}

func TestRendererContractOnlyInsideRuntime(t *testing.T) {
	var seen []bool
	var renderers []string
	app, err := nativeui.New("/", nativeui.Route{Path: "/", Screen: func(ctx context.Context, _ nativeui.Request) (nativeui.Screen, error) {
		contract, ok := nativeui.RendererContract(ctx)
		seen, renderers = append(seen, ok), append(renderers, contract.Renderer)
		if ok {
			contract.Capabilities[nativeui.CapabilityFiles] = "mutated"
		}
		return nativeui.Screen{Title: "Home", Text: "x"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Render(context.Background(), "/", nil); err != nil {
		t.Fatal(err)
	}
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	connection := wireHello(t, runtime, "webview", allCapabilities())
	wire(t, runtime, nativeui.Command{Kind: "open", Connection: connection, Route: "/"})
	wire(t, runtime, nativeui.Command{Kind: "open", Connection: connection, Route: "/"})
	if len(seen) != 3 || seen[0] || !seen[1] || renderers[1] != "webview" {
		t.Fatalf("contract visibility: %v %v", seen, renderers)
	}
	snapshot := runtime.Inspect()
	if snapshot.Connections[0].Capabilities[nativeui.CapabilityFiles] == "mutated" {
		t.Fatal("RendererContract exposed the negotiated map")
	}
}

func TestDefinitionErrorsKeepSentinelsAndUsePatterns(t *testing.T) {
	cases := map[string]struct {
		screen nativeui.Screen
		part   string
		is     error
	}{
		"row":    {nativeui.Screen{Title: "x", List: &nativeui.List{Rows: []nativeui.Row{{ID: "a", Title: "a", Destination: "/missing"}}}}, "row-destination", nativeui.ErrScreenNotFound},
		"tab":    {nativeui.Screen{Title: "x", Text: "x", Tabs: []nativeui.Tab{{ID: "a", Label: "a", Route: "https://example.com"}}, ActiveTab: "a"}, "tabs", nativeui.ErrInvalidRoute},
		"form":   {nativeui.Screen{Title: "x", Form: &nativeui.Form{}}, "form", nil},
		"action": {nativeui.Screen{Title: "x", Text: "x", Actions: []nativeui.Action{{ID: "nope", Label: "x"}}}, "actions", nil},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			app, err := nativeui.New("/things/{id}", nativeui.Route{Path: "/things/{id}", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) { return test.screen, nil }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Render(context.Background(), "/things/concrete-42", nil)
			var definition *nativeui.DefinitionError
			if !errors.As(err, &definition) || definition.Route != "/things/{id}" || definition.Part != test.part {
				t.Fatalf("error %T %v", err, err)
			}
			if test.is != nil && !errors.Is(err, test.is) {
				t.Fatalf("%v does not wrap %v", err, test.is)
			}
			if strings.Contains(definition.Route, "concrete-42") {
				t.Fatal("concrete route in definition")
			}
		})
	}
	app, _ := nativeui.New("/", nativeui.Route{Path: "/", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) { return nativeui.Screen{}, nil }})
	if _, err := app.Render(context.Background(), "/missing", nil); !errors.Is(err, nativeui.ErrScreenNotFound) {
		t.Fatalf("missing route: %v", err)
	} else if _, ok := err.(*nativeui.DefinitionError); ok {
		t.Fatal("request errors are not definition errors")
	}
}
