package webview_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
	"github.com/DonaldMurillo/gofastr-mobile/nativeui"
	"github.com/DonaldMurillo/gofastr-mobile/render/webview"
)

func TestPresentationProtocolThroughRendererActionBoundary(t *testing.T) {
	var calls int
	var submitted nativeui.Request
	app, err := nativeui.New("/choose", nativeui.Route{Path: "/choose", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Choose", Actions: []nativeui.Action{{ID: "show", Label: "Show selection"}},
			Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "title", Label: "Title", Kind: "text", Value: "Saved title"}}}}, nil
	}, Actions: map[string]nativeui.Handle{"show": func(context.Context, nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Presentation: &nativeui.Presentation{ID: "choose", Kind: nativeui.PresentationSheet, Title: "Choose <plain text>", Dismissible: true,
			Choices:   []nativeui.PresentationChoice{{ID: "apply", Label: "Apply"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}, {ID: "disabled", Label: "Unavailable", Disabled: true}},
			Selection: &nativeui.Selection{Multiple: true, Min: 1, Max: 1, Options: []nativeui.SelectionOption{{ID: "first", Label: "First", Detail: "Details"}, {ID: "disabled", Label: "Unavailable", Disabled: true}}}}}, nil
	}}, Presentations: map[string]nativeui.Respond{
		"choose": func(_ context.Context, request nativeui.Request, result nativeui.PresentationResult) (nativeui.Result, error) {
			calls++
			submitted = request
			if result.Choice != "apply" || len(result.Selected) != 1 || result.Selected[0] != "first" {
				t.Fatalf("unexpected Go result: %+v", result)
			}
			return nativeui.Result{Presentation: &nativeui.Presentation{ID: "confirm", Kind: nativeui.PresentationConfirmation, Title: "Confirm", Dismissible: true,
				Choices: []nativeui.PresentationChoice{{ID: "confirm", Label: "Confirm"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}}}, nil
		},
		"confirm": func(context.Context, nativeui.Request, nativeui.PresentationResult) (nativeui.Result, error) {
			calls++
			return nativeui.Result{Announcement: "Confirmed"}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webview.New(app, mobileui.IOS)
	if err != nil {
		t.Fatal(err)
	}
	send := func(command nativeui.Command) nativeui.Reply {
		t.Helper()
		command.Version = nativeui.ProtocolVersion
		encoded, err := json.Marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, webview.ActionPath, strings.NewReader(string(encoded)))
		request.Header.Set("X-GoFastr-Mobile-Protocol", "1")
		request.Header.Set("Content-Type", "application/json")
		writer := httptest.NewRecorder()
		renderer.Actions().ServeHTTP(writer, request)
		var reply nativeui.Reply
		if err := json.Unmarshal(writer.Body.Bytes(), &reply); err != nil || writer.Code != http.StatusOK || reply.Version != nativeui.ProtocolVersion {
			t.Fatalf("protocol response: %d %s: %v", writer.Code, writer.Body.String(), err)
		}
		if writer.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("presentation response was cacheable")
		}
		return reply
	}
	hello := send(nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityPresentation: nativeui.SupportAvailable,
	}}})
	if hello.Error != nil {
		t.Fatal(hello.Error)
	}
	opened := send(nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/choose"})
	if opened.Error != nil {
		t.Fatal(opened.Error)
	}
	command := nativeui.Command{Kind: "action", Connection: hello.Connection, Instance: opened.Instance, RequestID: "show-selection", Action: "show", Values: map[string]string{"title": "Local title"}, Revision: 1}
	if reply := send(command); reply.Error != nil || reply.PresentationID != "" {
		t.Fatalf("action must finish before the presentation is issued: %+v", reply)
	}
	command.Kind = "complete"
	shown := send(command)
	if shown.Error != nil || shown.PresentationID == "" || shown.PresentationID == "choose" || shown.Response == nil || shown.Response.Presentation == nil || shown.Response.Navigation != nil {
		t.Fatalf("presentation was not an issued overlay: %+v", shown)
	}
	command.Kind, command.PresentationID, command.Choice = "respond", shown.PresentationID, "apply"
	for _, selection := range []struct {
		requestID string
		selected  []string
	}{{"empty-selection", nil}, {"disabled-selection", []string{"disabled"}}} {
		command.RequestID, command.Selected = selection.requestID, selection.selected
		if reply := send(command); reply.Error == nil || reply.Error.Code != "validation" || reply.Response != nil || calls != 0 {
			t.Fatalf("invalid selection admitted a continuation: %+v", reply)
		}
	}
	command.RequestID, command.Selected, command.Values, command.Revision = "apply-selection", []string{"first"}, map[string]string{"title": "Newer local title"}, 2
	if reply := send(command); reply.Error != nil || reply.Response == nil || calls != 1 || submitted.Values["title"] != "Newer local title" {
		t.Fatalf("response lost the latest values: %+v, request %+v", reply, submitted)
	}
	command.Kind = "complete"
	chained := send(command)
	if chained.Error != nil || chained.PresentationID == "" || chained.PresentationID == shown.PresentationID || chained.Response.Presentation.Kind != nativeui.PresentationConfirmation {
		t.Fatalf("chained presentation was not separately issued: %+v", chained)
	}
	command.Kind, command.RequestID, command.PresentationID, command.Choice, command.Selected = "respond", "cancel-confirmation", chained.PresentationID, "cancel", []string{"untrusted"}
	if reply := send(command); reply.Error != nil || calls != 1 {
		t.Fatalf("cancellation invoked the Go continuation: %+v, calls %d", reply, calls)
	}
	command.Kind = "complete"
	if reply := send(command); reply.Error != nil || reply.PresentationID != "" || reply.Response.Presentation != nil {
		t.Fatalf("cancellation did not complete: %+v", reply)
	}
	command.Kind, command.RequestID, command.PresentationID, command.Choice, command.Selected = "action", "show-again", "", "", nil
	if reply := send(command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	command.Kind = "complete"
	if reply := send(command); reply.Error != nil || reply.PresentationID == "" {
		t.Fatalf("cancellation did not release the presentation coordinator: %+v", reply)
	}
}
