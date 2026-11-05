package webview_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/render/webview"
	"github.com/DonaldMurillo/gofastr/core/router"
)

type fixture struct {
	renderer *webview.Renderer
	server   *httptest.Server
	last     nativeui.Request
	calls    int
}

func newFixture(t *testing.T, platform mobileui.Platform) *fixture {
	t.Helper()
	fixture := &fixture{}
	definitions, err := nativeui.New("/items",
		nativeui.Route{Path: "/items", Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Shared items", Prompt: "One definition", LargeTitle: true,
				Actions: []nativeui.Action{{ID: "new", Label: "New item", Icon: nativeui.IconCompose}, {ID: "about", Label: "About", Leading: true, Icon: nativeui.IconInfo}},
				List: &nativeui.List{Header: "Items", Footer: "Local data", SearchKey: "query", SearchPlaceholder: "Find an item", Refreshable: true,
					Rows: []nativeui.Row{{Title: "Item <one>", Subtitle: request.Values["query"], Destination: "/items/42"}}}}, nil
		}, Actions: map[string]nativeui.Handle{
			"new": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/items/new"}}, nil
			},
			"about": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "sheet", Route: "/about"}}, nil
			},
		}},
		nativeui.Route{Path: "/items/new", Screen: editor, Actions: fixture.actions()},
		nativeui.Route{Path: "/items/{id}", Screen: editor, Actions: fixture.actions()},
		nativeui.Route{Path: "/about", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "About shared screens", Text: "Text <not HTML>\nSecond paragraph", Actions: []nativeui.Action{{ID: "close", Label: "Close", Primary: true}}}, nil
		}, Actions: map[string]nativeui.Handle{"close": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "dismiss"}}, nil
		}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture.renderer, err = webview.New(definitions, platform)
	if err != nil {
		t.Fatal(err)
	}
	router := router.New()
	router.Post(webview.ActionPath, fixture.renderer.Actions())
	fixture.renderer.UIHost().Mount(router)
	fixture.server = httptest.NewServer(router)
	t.Cleanup(fixture.server.Close)
	return fixture
}

func editor(context.Context, nativeui.Request) (nativeui.Screen, error) {
	return nativeui.Screen{Title: "Edit item", Actions: []nativeui.Action{{ID: "save", Label: "Save", Primary: true}, {ID: "share", Label: "Share", Icon: nativeui.IconShare}},
		Form: &nativeui.Form{Status: "Saved fields", Fields: []nativeui.Field{{Key: "title", Label: "Title", Kind: "text", Style: "title", Value: `"<saved>`},
			{Key: "body", Label: "Body", Kind: "multiline", Value: "</textarea><script>unsafe()</script>"}}, Draft: &nativeui.Draft{Key: "item-draft", Discard: "Discard draft"}}}, nil
}

func (fixture *fixture) actions() map[string]nativeui.Handle {
	return map[string]nativeui.Handle{
		"save": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
			fixture.last, fixture.calls = request, fixture.calls+1
			if strings.TrimSpace(request.Values["title"]) == "" {
				return nativeui.Result{}, fmt.Errorf("service save: %w", &nativeui.ActionError{Code: "validation", Message: "Give the item a title.", Fields: map[string]string{"title": "Required"}, Err: errors.New("original service validation")})
			}
			return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, ClearDraft: true, Haptic: "success", Announcement: "Item saved"}, nil
		},
		"share": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Share: request.Values["title"] + "\n" + request.Values["body"]}, nil
		},
	}
}

func (fixture *fixture) request(t *testing.T, method, path, contentType, accept, body string) (int, http.Header, string) {
	t.Helper()
	request, err := http.NewRequest(method, fixture.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	request.Header.Set("Accept", accept)
	if accept == "partial" {
		request.Header.Set("X-Gofastr-Navigate", "1")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, string(encoded)
}

func TestSSROwnsGenericScreensAndPartialNavigation(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			fixture := newFixture(t, platform)
			status, _, markup := fixture.request(t, "GET", "/?query=%3Cb%3E", "", "text/html", "")
			if status != http.StatusOK {
				t.Fatalf("root did not render shared root: %d %s", status, markup)
			}
			for _, expected := range []string{"Shared items", `data-gfm-platform="` + string(platform) + `"`, `href="/items/42"`, `name="query"`, `value="&lt;b&gt;"`, "New item", "About", "Refresh", "Local data", "gfm-webview", "/__gofastr/runtime.js"} {
				if !strings.Contains(markup, expected) {
					t.Errorf("SSR missing %q", expected)
				}
			}
			status, _, markup = fixture.request(t, "GET", "/items/42", "", "partial", "")
			if status != http.StatusOK || strings.Contains(markup, "<!DOCTYPE") {
				t.Fatalf("not a normal screen partial: %d %s", status, markup)
			}
			for _, expected := range []string{`name="title"`, `name="body"`, `data-gfw-field="title"`, "Saved fields", "Discard draft", "&lt;/textarea&gt;&lt;script&gt;unsafe()&lt;/script&gt;", "data-hui-leave-guard", "Share"} {
				if !strings.Contains(markup, expected) {
					t.Errorf("form missing %q", expected)
				}
			}
			if strings.Contains(markup, "<script>unsafe()") {
				t.Fatal("unescaped form content")
			}
			status, _, markup = fixture.request(t, "GET", "/about", "", "text/html", "")
			if status != http.StatusOK || !strings.Contains(markup, "Text &lt;not HTML&gt;") || !strings.Contains(markup, "Close") {
				t.Fatalf("text screen not rendered: %d %s", status, markup)
			}
		})
	}
}

func TestPublicActionBoundaryRetainsFieldsAndWireV1(t *testing.T) {
	fixture := newFixture(t, mobileui.IOS)
	status, _, body := fixture.request(t, "POST", webview.ActionPath, "application/json", "application/json",
		`{"route":"/items/42?source=list","action":"save","values":{"title":"Saved title","body":"Text <not HTML>"}}`)
	var response nativeui.Response
	if err := json.Unmarshal([]byte(body), &response); err != nil || status != http.StatusOK || response.Version != 1 || !response.ClearDraft || response.Navigation.Kind != "pop" || response.Haptic != "success" || response.Announcement != "Item saved" {
		t.Fatalf("wire response: %d %s %v", status, body, err)
	}
	if fixture.last.Params["id"] != "42" || fixture.last.Values["source"] != "list" || fixture.last.Values["body"] != "Text <not HTML>" {
		t.Fatalf("typed request lost fields or route params: %+v", fixture.last)
	}
	for _, action := range []string{"new", "about"} {
		status, _, body = fixture.request(t, "POST", webview.ActionPath, "application/json", "application/json", fmt.Sprintf(`{"route":"/items","action":%q,"values":{}}`, action))
		if err := json.Unmarshal([]byte(body), &response); err != nil || status != 200 || response.Screen == nil || response.Navigation == nil {
			t.Fatalf("navigation action: %d %s %v", status, body, err)
		}
	}
	status, _, body = fixture.request(t, "POST", webview.ActionPath, "application/json", "application/json", `{"route":"/items/42","action":"save","values":{"title":"   ","body":"Still editing"}}`)
	if status != http.StatusUnprocessableEntity || !strings.Contains(body, `"code":"validation"`) || !strings.Contains(body, `"title":"Required"`) || strings.Contains(body, "original service validation") {
		t.Fatalf("structured validation failure: %d %s", status, body)
	}
	form := url.Values{"title": {"   "}, "body": {"</textarea><script>draft</script>"}}
	endpoint := webview.ActionPath + "?" + url.Values{"route": {"/items/42"}, "action": {"save"}}.Encode()
	status, _, body = fixture.request(t, "POST", endpoint, "application/x-www-form-urlencoded", "text/html", form.Encode())
	if status != 422 || !strings.Contains(body, `value="   "`) || !strings.Contains(body, "&lt;/textarea&gt;&lt;script&gt;draft&lt;/script&gt;") || !strings.Contains(body, "Give the item a title.") || !strings.Contains(body, `aria-invalid="true"`) {
		t.Fatalf("HTML fallback discarded editing buffer: %d %s", status, body)
	}
	form.Set("title", "Saved")
	status, headers, body := fixture.request(t, "POST", endpoint, "application/x-www-form-urlencoded", "text/html", form.Encode())
	if status != http.StatusSeeOther || headers.Get("Location") != "/items" {
		t.Fatalf("native form navigation: %d %s %s", status, headers.Get("Location"), body)
	}
	shareEndpoint := webview.ActionPath + "?" + url.Values{"route": {"/items/42"}, "action": {"share"}}.Encode()
	status, _, body = fixture.request(t, "POST", shareEndpoint, "application/x-www-form-urlencoded", "text/html", form.Encode())
	if status != http.StatusOK || !strings.Contains(body, "System sharing requires JavaScript") || !strings.Contains(body, `aria-label="Text to share"`) || !strings.Contains(body, "&lt;script&gt;draft&lt;/script&gt;") || !strings.Contains(body, `name="title"`) {
		t.Fatalf("share fallback lost the submitted text: %d %s", status, body)
	}
}

func TestActionWhitelistsAndBoundedRequests(t *testing.T) {
	fixture := newFixture(t, mobileui.IOS)
	for _, payload := range []string{
		`null`, `[]`, `{} {}`, `{"route":"/items","action":"new","values":{"title":42}}`,
		`{"route":"https://external.example/items","action":"save","values":{}}`,
		`{"route":"/items/42","action":"delete-all","values":{}}`,
		`{"route":"/items/42","action":"save","values":{},"privileged":true}`,
	} {
		status, _, body := fixture.request(t, "POST", webview.ActionPath, "application/json", "application/json", payload)
		if status != http.StatusBadRequest || fixture.calls != 0 {
			t.Errorf("unsafe request reached handler: %d %s", status, body)
		}
	}
	request := httptest.NewRequest("POST", webview.ActionPath, strings.NewReader(`{"route":"/items/42","action":"save","values":{"title":"valid"}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://external.example")
	writer := httptest.NewRecorder()
	fixture.renderer.Actions().ServeHTTP(writer, request)
	if writer.Code != http.StatusForbidden || fixture.calls != 0 {
		t.Fatal("cross-origin action reached Go")
	}
	writer = httptest.NewRecorder()
	fixture.renderer.Actions().ServeHTTP(writer, httptest.NewRequest("GET", webview.ActionPath, nil))
	if writer.Code != http.StatusMethodNotAllowed || writer.Header().Get("Allow") != "POST" {
		t.Fatal("action handler accepted GET")
	}
	status, _, _ := fixture.request(t, "POST", webview.ActionPath, "application/json", "application/json", strings.Repeat(" ", (8<<20)+1))
	if status != http.StatusRequestEntityTooLarge || fixture.calls != 0 {
		t.Fatal("oversized action reached Go")
	}
}

func TestRejectInvalidRendererConfiguration(t *testing.T) {
	if _, err := webview.New(nil, mobileui.IOS); err == nil {
		t.Fatal("accepted nil registry")
	}
	app, err := nativeui.New("/", nativeui.Route{Path: "/", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Home", Text: "Content"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := webview.New(app, "desktop"); err == nil {
		t.Fatal("accepted unsupported platform")
	}
}

func TestRendererAssetsTravelThroughTheUIHost(t *testing.T) {
	fixture := newFixture(t, mobileui.IOS)
	status, _, source := fixture.request(t, "GET", "/__gofastr/runtime/gfm-webview.js", "", "text/javascript", "")
	if status != 200 || !strings.Contains(source, "gofastr:webview:draft:") || !strings.Contains(source, "gofastr-history") {
		t.Fatalf("renderer behavior was not served by the host: %d %s", status, source)
	}
	if strings.Contains(source, "location.href =") || strings.Contains(source, "history.pushState") || strings.Contains(source, "/notes") {
		t.Fatal("renderer owns a competing router or application-specific route")
	}
	if !strings.Contains(source, "presentation/v1") || !strings.Contains(source, "respond") {
		t.Fatal("renderer does not negotiate and respond to shared presentations")
	}
	status, _, source = fixture.request(t, "GET", "/__gofastr/runtime/gfm-webview-presentation.js", "", "text/javascript", "")
	if status != 200 || !strings.Contains(source, ".showModal()") || !strings.Contains(source, "data-gfw-presentation") || !strings.Contains(source, "data-gfw-choice") || !strings.Contains(source, "data-gfw-option") {
		t.Fatalf("presentation adapter was not served by the host: %d %s", status, source)
	}
	for _, forbidden := range []string{"innerHTML", "history.", "location.", "fetch(", "/notes", "window.confirm"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("presentation adapter contains non-DOM behavior %q", forbidden)
		}
	}
	status, _, source = fixture.request(t, "GET", "/__gofastr/runtime/gfm-webview-files.js", "", "text/javascript", "")
	if status != 200 || !strings.Contains(source, "data-gfw-file-input") || !strings.Contains(source, "data-gfw-file-download") || !strings.Contains(source, "URL.revokeObjectURL") {
		t.Fatalf("file adapter was not served by the host: %d %s", status, source)
	}
	for _, forbidden := range []string{"innerHTML", "history.", "location.", "fetch(", "/notes", "showOpenFilePicker"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("file adapter contains non-DOM or automatic file behavior %q", forbidden)
		}
	}
	status, _, source = fixture.request(t, "GET", "/__gofastr/runtime/gfm-webview-photos.js", "", "text/javascript", "")
	if status != 200 || !strings.Contains(source, "createImageBitmap") || !strings.Contains(source, "'from-image'") || !strings.Contains(source, "canvas.toBlob") {
		t.Fatalf("photo normalizer was not served by the host: %d %s", status, source)
	}
	status, _, source = fixture.request(t, "GET", "/__gofastr/comp/gfm-webview.css", "", "text/css", "")
	if status != 200 || !strings.Contains(source, "gfw-field") || !strings.Contains(source, "var(--color-text)") {
		t.Fatalf("renderer stylesheet was not served by the host: %d %s", status, source)
	}
}
