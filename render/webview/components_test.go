package webview_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
	"github.com/DonaldMurillo/gofastr-mobile/nativeui"
	"github.com/DonaldMurillo/gofastr-mobile/render/webview"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func newComponentsFixture(t *testing.T, platform mobileui.Platform) *fixture {
	t.Helper()
	fixture := &fixture{}
	tabs := []nativeui.Tab{{ID: "records", Label: "Records", Route: "/records", Icon: nativeui.IconCompose}, {ID: "files", Label: "Files", Route: "/files", Icon: nativeui.IconInfo}}
	handler := func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		fixture.last, fixture.calls = request, fixture.calls+1
		return nativeui.Result{Announcement: "Applied"}, nil
	}
	definitions, err := nativeui.New("/records",
		nativeui.Route{Path: "/records", Actions: map[string]nativeui.Handle{"new": handler, "about": handler, "files": handler, "behaviors": handler, "undo": handler, "bulk": handler, "pin": handler, "trash": handler}, Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Records", LargeTitle: true, Tabs: tabs, ActiveTab: "records", Values: request.Values,
				Actions: []nativeui.Action{{ID: "new", Label: "New record", Primary: true}, {ID: "about", Label: "About records", Leading: true},
					{ID: "files", Label: "Files"}, {ID: "behaviors", Label: "Behaviors"}, {ID: "bulk", Label: "Tag selected", Commit: true}, {ID: "undo", Label: "Undo last change", Commit: true}},
				List: &nativeui.List{Header: "Records", SearchKey: "q", Refreshable: true,
					Filters: []nativeui.Field{{Key: "tag", Label: "Filter tag", Kind: "select", Value: request.Values["tag"], Options: []nativeui.Option{{Value: "work", Label: "Work"}, {Value: "old", Label: "Old", Disabled: true}}},
						{Key: "pinned", Label: "Pinned only", Kind: "toggle", Value: request.Values["pinned"]}},
					Selection: &nativeui.Selection{Key: "selected", Maximum: 2}, Paging: &nativeui.Paging{Key: "page", Next: "3", Label: "Load more records"},
					Rows: []nativeui.Row{{ID: "one", Title: "Record <one>", Destination: "/records/one", LeadingActions: []nativeui.RowAction{{ID: "pin", Label: "Pin", FullSwipe: true}}, TrailingActions: []nativeui.RowAction{{ID: "trash", Label: "Trash", Role: "destructive"}}},
						{ID: "two", Title: "Record two", Destination: "/records/two"}, {ID: "locked", Title: "Locked record", Destination: "/records/locked", Disabled: true, TrailingActions: []nativeui.RowAction{{ID: "trash", Label: "Trash", Role: "destructive"}}}}}}, nil
		}},
		nativeui.Route{Path: "/records/{id}", Actions: map[string]nativeui.Handle{"save": handler}, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Edit record", Tabs: tabs, ActiveTab: "records", Actions: []nativeui.Action{{ID: "save", Label: "Save", Primary: true, Commit: true}},
				Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "title", Label: "Title", Kind: "text", Value: "Saved", Required: true},
					{Key: "body", Label: "Body", Kind: "multiline", Value: "Text"}, {Key: "pinned", Label: "Pinned", Kind: "toggle", Value: "true", Required: true},
					{Key: "tag", Label: "Tag", Kind: "select", Options: []nativeui.Option{{Value: "work", Label: "Work <tag>"}, {Value: "old", Label: "Old", Disabled: true}}},
					{Key: "day", Label: "Reminder day", Kind: "date"}, {Key: "time", Label: "Reminder time", Kind: "time"},
					{Key: "locked", Label: "Locked", Kind: "text", Value: "server", Disabled: true}}}}, nil
		}},
		nativeui.Route{Path: "/files", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Files", Text: "Files", Tabs: tabs, ActiveTab: "files"}, nil
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture.renderer, err = webview.New(definitions, platform)
	if err != nil {
		t.Fatal(err)
	}
	routes := router.New()
	routes.Post(webview.ActionPath, fixture.renderer.Actions())
	fixture.renderer.UIHost().Mount(routes)
	fixture.server = httptest.NewServer(routes)
	t.Cleanup(fixture.server.Close)
	return fixture
}

func TestComponentControlsUseSemanticLabelledInputs(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			fixture := newComponentsFixture(t, platform)
			status, _, markup := fixture.request(t, "GET", "/records/one", "", "partial", "")
			if status != http.StatusOK {
				t.Fatalf("render controls: %d %s", status, markup)
			}
			for _, expected := range []string{`data-gfw-field="title"`, `data-gfw-field="body"`, `data-gfw-field="pinned"`, `type="checkbox"`, `checked`, `aria-required="true"`,
				`<select`, `data-gfw-field="tag"`, `Work &lt;tag&gt;`, `value=""`, `>None</option>`, `type="date"`, `type="time"`, `step="60"`, `disabled`, `data-gfw-field-error="day"`,
				`data-gfw-tab="records"`, `data-gfw-tab="files"`, `aria-current="page"`, `aria-label="App tabs"`, `data-hui-leave-guard`} {
				if !strings.Contains(markup, expected) {
					t.Errorf("control missing %q", expected)
				}
			}
			if !strings.Contains(markup, `<label for="gfw-field-2">Pinned</label>`) || strings.Contains(markup, "<script>unsafe") {
				t.Fatal("controls lost their label or text escaping")
			}
		})
	}
}

func TestCollectionRendersFiltersSelectionPagingAndAccessibleRowMenus(t *testing.T) {
	fixture := newComponentsFixture(t, mobileui.IOS)
	status, _, markup := fixture.request(t, "GET", "/records?q=needle&tag=work&page=2", "", "partial", "")
	if status != http.StatusOK {
		t.Fatalf("render list: %d %s", status, markup)
	}
	for _, expected := range []string{`data-gfw-filter="tag"`, `data-gfw-filter="pinned"`, `data-gfw-select="one"`, `data-gfw-select="locked"`,
		`data-gfw-load-more`, `data-gfw-row-action="pin"`, `data-gfw-row-id="one"`, `data-gfw-full-swipe="true"`, `data-gfw-row-action="trash"`,
		`data-gfw-row-menu="one"`, `Actions for Record &lt;one&gt;`, `data-gfw-disabled="true"`, `aria-disabled="true"`, `aria-label="Selection actions"`, `data-gfw-refresh`, `data-gfw-search`} {
		if !strings.Contains(markup, expected) {
			t.Errorf("collection missing %q", expected)
		}
	}
	if strings.Contains(markup, `href="/records/locked"`) {
		t.Fatal("disabled row still has an active navigation link")
	}
	if !strings.Contains(markup, `page=3`) || !strings.Contains(markup, `q=needle`) || !strings.Contains(markup, `tag=work`) {
		t.Fatal("paging discarded the current query")
	}
	status, _, source := fixture.request(t, "GET", "/__gofastr/runtime/gfm-webview-components.js", "", "text/javascript", "")
	if status != http.StatusOK || !strings.Contains(source, "gestureEnd") || !strings.Contains(source, "restoreMetadata") {
		t.Fatalf("bundled components not served: %d %s", status, source)
	}
}

func TestCollectionActionsUseOneCompactHeaderAndOneToolbar(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) {
			fixture := newComponentsFixture(t, platform)
			status, _, markup := fixture.request(t, "GET", "/records", "", "partial", "")
			if status != http.StatusOK {
				t.Fatalf("render collection: %d %s", status, markup)
			}
			for _, action := range []string{"new", "about", "files", "behaviors", "bulk", "undo"} {
				if count := strings.Count(markup, `value="`+action+`"`); count != 1 {
					t.Errorf("action %s rendered %d times, want once", action, count)
				}
			}
			header := markup[strings.Index(markup, `<header`):strings.Index(markup, `</header>`)]
			if strings.Contains(header, `value="bulk"`) || strings.Contains(header, `value="files"`) || strings.Contains(header, `value="behaviors"`) {
				t.Fatal("collection or overflow actions still expand the sticky header")
			}
			start := strings.Index(markup, `class="gfw-selection-toolbar"`)
			if start < 0 {
				t.Fatal("selection toolbar missing")
			}
			toolbar := markup[start : start+strings.Index(markup[start:], `</div>`)]
			if !strings.Contains(toolbar, `value="bulk"`) || !strings.Contains(toolbar, `value="undo"`) || strings.Contains(toolbar, `value="new"`) || strings.Contains(toolbar, `value="files"`) {
				t.Fatal("collection action placement duplicates or loses action semantics")
			}
			if !strings.Contains(markup, `data-gfw-header-menu`) || !strings.Contains(markup, `aria-label="More actions"`) {
				t.Fatal("overflow actions have no labelled native disclosure")
			}
		})
	}
}

func TestControlValidationAndValuesCrossTheHTTPBoundary(t *testing.T) {
	fixture := newComponentsFixture(t, mobileui.IOS)
	values := map[string]string{"title": "Saved", "body": "Still editing", "pinned": "false", "tag": "work", "day": "2026-11-05", "time": "09:07", "locked": "server"}
	post := func(values map[string]string) (int, string) {
		encoded, _ := json.Marshal(map[string]any{"route": "/records/one", "action": "save", "values": values})
		status, _, body := fixture.request(t, "POST", webview.ActionPath, "application/json", "application/json", string(encoded))
		return status, body
	}
	if status, body := post(values); status != http.StatusOK || fixture.calls != 1 || fixture.last.Values["pinned"] != "false" || fixture.last.Values["day"] != "2026-11-05" || fixture.last.Values["time"] != "09:07" {
		t.Fatalf("valid controls lost string values: %d %s %+v", status, body, fixture.last)
	}
	for key, invalid := range map[string]string{"title": "", "pinned": "on", "tag": "old", "day": "2026-02-30", "time": "9:07", "locked": "tampered"} {
		original := values[key]
		values[key] = invalid
		if status, body := post(values); status != http.StatusUnprocessableEntity || !strings.Contains(body, `"`+key+`":`) || fixture.calls != 1 {
			t.Errorf("invalid %s reached the Go action: %d %s", key, status, body)
		}
		values[key] = original
	}
	values["tag"], values["day"], values["time"] = "", "", ""
	if status, body := post(values); status != http.StatusOK || fixture.calls != 2 {
		t.Fatalf("optional empty values rejected: %d %s", status, body)
	}
	form := url.Values{"title": {"Saved"}, "body": {"draft"}, "tag": {"work"}, "day": {""}, "time": {""}}
	endpoint := webview.ActionPath + "?" + url.Values{"route": {"/records/one"}, "action": {"save"}}.Encode()
	status, _, body := fixture.request(t, "POST", endpoint, "application/x-www-form-urlencoded", "text/html", form.Encode())
	if status != http.StatusOK || fixture.last.Values["pinned"] != "false" || fixture.last.Values["locked"] != "server" {
		t.Fatalf("native form checkbox or disabled baseline lost: %d %s %+v", status, body, fixture.last)
	}
}

func TestCollectionActionsRevalidateSelectedAndRowIDsAtHTTPBoundary(t *testing.T) {
	fixture := newComponentsFixture(t, mobileui.IOS)
	values := map[string]string{"q": "needle", "tag": "work", "pinned": "false", "page": "2", "selected": `["two","one"]`}
	post := func(action string, values map[string]string) (int, string) {
		encoded, _ := json.Marshal(map[string]any{"route": "/records?q=needle&tag=work&pinned=false&page=2", "action": action, "values": values})
		status, _, body := fixture.request(t, "POST", webview.ActionPath, "application/json", "application/json", string(encoded))
		return status, body
	}
	if status, body := post("bulk", values); status != http.StatusOK || fixture.calls != 1 || fixture.last.Values["selected"] != `["two","one"]` || fixture.last.Values["tag"] != "work" {
		t.Fatalf("bulk values: %d %s %+v", status, body, fixture.last)
	}
	for _, invalid := range []string{`["locked"]`, `["gone"]`, `[0]`, `not-json`, `["one","two","locked"]`} {
		values["selected"] = invalid
		if status, body := post("bulk", values); status != http.StatusUnprocessableEntity || fixture.calls != 1 {
			t.Errorf("untrusted selection admitted: %d %s", status, body)
		}
	}
	values["selected"], values["row"] = `[]`, "one"
	if status, body := post("pin", values); status != http.StatusOK || fixture.last.Values["row"] != "one" || fixture.calls != 2 {
		t.Fatalf("row ID not delivered: %d %s %+v", status, body, fixture.last)
	}
	for _, row := range []string{"locked", "gone", "two"} {
		values["row"] = row
		if status, body := post("pin", values); status != http.StatusUnprocessableEntity || fixture.calls != 2 {
			t.Errorf("unavailable row action admitted: %d %s", status, body)
		}
	}
	delete(values, "row")
	values["tag"] = ""
	if status, body := post("bulk", values); status != http.StatusUnprocessableEntity || fixture.calls != 2 {
		t.Fatalf("changed query admitted without reopening: %d %s", status, body)
	}
	endpoint := webview.ActionPath + "?" + url.Values{"route": {"/records?tag=work&pinned=false"}, "action": {"bulk"}}.Encode()
	form := url.Values{"tag": {"work"}, "pinned": {"false"}, "_gfw_selection": {"one", "two"}}
	status, _, body := fixture.request(t, "POST", endpoint, "application/x-www-form-urlencoded", "text/html", form.Encode())
	if status != http.StatusOK || fixture.last.Values["selected"] != `["one","two"]` {
		t.Fatalf("native selection fallback: %d %s %+v", status, body, fixture.last)
	}
	endpoint = webview.ActionPath + "?" + url.Values{"route": {"/records?tag=work&pinned=false"}, "action": {"pin"}, "row": {"one"}}.Encode()
	form.Del("_gfw_selection")
	status, _, body = fixture.request(t, "POST", endpoint, "application/x-www-form-urlencoded", "text/html", form.Encode())
	if status != http.StatusOK || fixture.last.Values["row"] != "one" {
		t.Fatalf("native row menu fallback: %d %s %+v", status, body, fixture.last)
	}
}

func TestControlValidationFallbackRetainsTypedValuesButNotDisabledEdits(t *testing.T) {
	fixture := newComponentsFixture(t, mobileui.IOS)
	form := url.Values{"title": {""}, "body": {"Live </textarea><script>buffer</script>"}, "tag": {"work"}, "day": {"2026-11-05"}, "time": {"09:07"}, "locked": {"tampered"}}
	endpoint := webview.ActionPath + "?" + url.Values{"route": {"/records/one"}, "action": {"save"}}.Encode()
	status, _, body := fixture.request(t, "POST", endpoint, "application/x-www-form-urlencoded", "text/html", form.Encode())
	if status != http.StatusUnprocessableEntity || fixture.calls != 0 {
		t.Fatalf("invalid controls admitted: %d %s", status, body)
	}
	for _, expected := range []string{"Live &lt;/textarea&gt;&lt;script&gt;buffer&lt;/script&gt;", `value="2026-11-05"`, `value="09:07"`, `value="server"`, `aria-invalid="true"`, `This control is disabled.`} {
		if !strings.Contains(body, expected) {
			t.Errorf("failed render discarded %q", expected)
		}
	}
	if strings.Contains(body, `value="tampered"`) || strings.Contains(body, "<script>buffer") {
		t.Fatal("disabled edit was retained or field markup was not escaped")
	}
}

func TestInvalidComponentDefinitionsNeverRenderInteractiveMarkup(t *testing.T) {
	for name, screen := range map[string]nativeui.Screen{
		"destructive full swipe": {Title: "Unsafe", List: &nativeui.List{Rows: []nativeui.Row{{ID: "one", Title: "One", Destination: "/safe", TrailingActions: []nativeui.RowAction{{ID: "pin", Label: "Trash", Role: "destructive", FullSwipe: true}}}}}},
		"duplicate options":      {Title: "Unsafe", Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "tag", Label: "Tag", Kind: "select", Options: []nativeui.Option{{Value: "work", Label: "Work"}, {Value: "work", Label: "Again"}}}}}},
		"remote tab":             {Title: "Unsafe", Text: "Text", Tabs: []nativeui.Tab{{ID: "unsafe", Label: "Unsafe", Route: "https://evil.example/unsafe"}}, ActiveTab: "unsafe"},
	} {
		t.Run(name, func(t *testing.T) {
			app, err := nativeui.New("/safe", nativeui.Route{Path: "/safe", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) { return screen, nil },
				Actions: map[string]nativeui.Handle{"pin": func(context.Context, nativeui.Request) (nativeui.Result, error) {
					t.Error("definition validation dispatched an action")
					return nativeui.Result{}, nil
				}}})
			if err != nil {
				t.Fatal(err)
			}
			renderer, err := webview.New(app, mobileui.IOS)
			if err != nil {
				t.Fatal(err)
			}
			routes := router.New()
			renderer.UIHost().Mount(routes)
			recorder := httptest.NewRecorder()
			routes.ServeHTTP(recorder, httptest.NewRequest("GET", "/safe", nil))
			if recorder.Code < 400 || strings.Contains(recorder.Body.String(), "data-gfw-row-action=") || strings.Contains(recorder.Body.String(), `data-gfw-tab="unsafe"`) {
				t.Fatalf("invalid definition rendered: %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
