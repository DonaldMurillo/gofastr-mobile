package webview_test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/webassets"
	"github.com/DonaldMurillo/gofastr-native/extensions/media"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/render/webview"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func newRenderingFixture(t *testing.T) (*fixture, extension.Component) {
	t.Helper()
	block, err := media.Block("private-media", media.Content{Attachments: []nativeui.Attachment{{
		ID: "brief", Name: "brief <private>.txt", MediaType: "text/plain", Size: 12, Destination: "/items/brief",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := nativeui.NewWithExtensions("/items", []extension.Definition{media.Definition()},
		nativeui.Route{Path: "/items", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Private media", Components: []extension.Component{block},
				Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "title", Label: "Title", Kind: "text", Value: "Saved"}}}}, nil
		}},
		nativeui.Route{Path: "/items/{id}", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Detail", Text: "Private detail"}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webview.New(app, mobileui.IOS)
	if err != nil {
		t.Fatal(err)
	}
	routes := router.New()
	routes.Post(webview.ActionPath, renderer.Actions())
	renderer.UIHost().Mount(routes)
	server := httptest.NewServer(routes)
	t.Cleanup(server.Close)
	return &fixture{renderer: renderer, server: server}, block
}

func TestRenderingComponentsPropagateInFullAndPartialMarkupWithoutLegacyFallback(t *testing.T) {
	fixture, block := newRenderingFixture(t)
	module, err := webassets.Module(media.Definition())
	if err != nil {
		t.Fatal(err)
	}
	for _, accept := range []string{"text/html", "partial"} {
		t.Run(accept, func(t *testing.T) {
			status, _, markup := fixture.request(t, "GET", "/items", "", accept, "")
			if status != http.StatusOK {
				t.Fatalf("render: %d %s", status, markup)
			}
			for _, expected := range []string{`data-gfw-components`, `data-gfw-component="private-media"`, `data-gfw-component-name="gofastr.media/v1"`, `Loading component…`, `data-gfw-field="title"`} {
				if !strings.Contains(markup, expected) {
					t.Fatalf("missing generic markup %q", expected)
				}
			}
			for _, forbidden := range []string{`data-gfw-attachments`, `data-gfw-images`, `data-gfw-file-preview`, `data-gfw-attachment=`} {
				if strings.Contains(markup, forbidden) {
					t.Fatalf("component SSR used a legacy media slot: %q", forbidden)
				}
			}
			match := regexp.MustCompile(`data-gfw-config="([^"]+)"`).FindStringSubmatch(markup)
			if len(match) != 2 {
				t.Fatal("missing client config")
			}
			var config struct {
				Extensions          []string              `json:"extensions"`
				ComponentExtensions []string              `json:"componentExtensions"`
				Components          []extension.Component `json:"components"`
			}
			if err := json.Unmarshal([]byte(html.UnescapeString(match[1])), &config); err != nil {
				t.Fatal(err)
			}
			if len(config.Extensions) != 1 || config.Extensions[0] != module || len(config.ComponentExtensions) != 1 || config.ComponentExtensions[0] != media.Name ||
				len(config.Components) != 1 || config.Components[0].Payload != block.Payload || config.Components[0].Destinations["file.0"] != "/items/brief" {
				t.Fatalf("component metadata was lost: %+v", config)
			}
		})
	}
}

func TestRenderingSupportIsRequiredAtTheWebViewHTTPBoundary(t *testing.T) {
	fixture, block := newRenderingFixture(t)
	for _, support := range []nativeui.Support{"", nativeui.SupportUnavailable, nativeui.SupportFallback, nativeui.SupportAvailable} {
		t.Run(string(support), func(t *testing.T) {
			capabilities := map[nativeui.Capability]nativeui.Support{nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable}
			if support != "" {
				capabilities[nativeui.Capability(media.Name)] = support
			}
			hello := rendererCommand(t, fixture.renderer, context.Background(), nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{
				Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: capabilities,
			}})
			if hello.Error != nil {
				t.Fatal(hello.Error)
			}
			opened := rendererCommand(t, fixture.renderer, context.Background(), nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/items"})
			if support != nativeui.SupportAvailable {
				if opened.Error == nil || opened.Screen != nil || !strings.Contains(opened.Error.Message, media.Name) {
					t.Fatalf("required media was silently omitted: %+v", opened)
				}
				return
			}
			if opened.Error != nil || opened.Screen == nil || len(opened.Screen.Components) != 1 || opened.Screen.Components[0].Payload != block.Payload {
				t.Fatalf("component snapshot not returned: %+v", opened)
			}
		})
	}
}

func TestComponentHostIsGenericAndBundledByTheExistingRuntime(t *testing.T) {
	fixture := newFixture(t, mobileui.IOS)
	status, _, source := fixture.request(t, "GET", "/__gofastr/runtime/gfm-webview-rendering.js", "", "text/javascript", "")
	if status != http.StatusOK {
		t.Fatalf("rendering host missing: %d", status)
	}
	for _, expected := range []string{"__gofastr_component_extension_definitions", "adapter.mount", "entry.instance.update(snapshot)", "entry.instance?.dispose()"} {
		if !strings.Contains(source, expected) {
			t.Fatalf("missing host behavior %q", expected)
		}
	}
	for _, forbidden := range []string{"innerHTML", "fetch(", "history.", "location.", "gofastr.media/v1", "/notes"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("generic component host owns library/application behavior: %q", forbidden)
		}
	}
}
