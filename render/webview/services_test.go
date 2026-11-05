package webview_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-mobile/device/audio"
	"github.com/DonaldMurillo/gofastr-mobile/device/files"
	"github.com/DonaldMurillo/gofastr-mobile/device/sharing"
	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
	"github.com/DonaldMurillo/gofastr-mobile/nativeui"
	"github.com/DonaldMurillo/gofastr-mobile/render/webview"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func rendererCommand(t *testing.T, renderer *webview.Renderer, ctx context.Context, command nativeui.Command) nativeui.Reply {
	t.Helper()
	command.Version = nativeui.ProtocolVersion
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, webview.ActionPath, strings.NewReader(string(encoded)))
	request.Header.Set("X-GoFastr-Mobile-Protocol", "1")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	renderer.Actions().ServeHTTP(response, request)
	var reply nativeui.Reply
	if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil || response.Code != http.StatusOK || reply.Version != nativeui.ProtocolVersion {
		t.Fatalf("command %s: %d %s: %v", command.Kind, response.Code, response.Body.String(), err)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("service/live outcome must not be cached")
	}
	return reply
}

func rendererHello(t *testing.T, renderer *webview.Renderer) nativeui.Reply {
	t.Helper()
	return rendererCommand(t, renderer, context.Background(), nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{
		Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[nativeui.Capability]nativeui.Support{
			nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable,
			nativeui.CapabilityClipboard: nativeui.SupportFallback, nativeui.CapabilityLive: nativeui.SupportAvailable,
		},
	}})
}

func TestDeviceServiceResultThroughWebViewHTTPBoundary(t *testing.T) {
	for _, outcome := range []nativeui.EffectStatus{nativeui.EffectCompleted, nativeui.EffectCancelled, nativeui.EffectDenied, nativeui.EffectUnsupported, nativeui.EffectFailed} {
		t.Run(string(outcome), func(t *testing.T) {
			var calls int
			var received nativeui.Request
			app, err := nativeui.New("/items", nativeui.Route{Path: "/items", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
				return nativeui.Screen{Title: "Edit", Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "title", Label: "Title", Kind: "text", Value: "Saved"}}},
					Actions: []nativeui.Action{{ID: "copy", Label: "Copy"}}}, nil
			}, Actions: map[string]nativeui.Handle{"copy": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "copy-result", Clipboard: &sharing.Clipboard{Text: "Private text"}}}, nil
			}}, Services: map[string]nativeui.ServiceRespond{"copy-result": func(_ context.Context, request nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
				calls++
				received = request
				if result.Status != outcome || result.File != nil {
					t.Fatalf("wrong observed service outcome: %+v", result)
				}
				return nativeui.Result{Announcement: "Observed " + string(result.Status)}, nil
			}}})
			if err != nil {
				t.Fatal(err)
			}
			renderer, err := webview.New(app, mobileui.IOS)
			if err != nil {
				t.Fatal(err)
			}
			send := func(command nativeui.Command) nativeui.Reply {
				return rendererCommand(t, renderer, context.Background(), command)
			}
			hello := rendererHello(t, renderer)
			if hello.Error != nil {
				t.Fatal(hello.Error)
			}
			opened := send(nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/items"})
			command := nativeui.Command{Kind: "action", Connection: hello.Connection, Instance: opened.Instance, RequestID: "copy-once", Action: "copy", Revision: 1, Values: map[string]string{"title": "Before copy"}}
			if reply := send(command); reply.Error != nil || reply.ServiceID != "" {
				t.Fatalf("service issued before observed completion: %+v", reply)
			}
			command.Kind = "complete"
			admitted := send(command)
			if admitted.Error != nil || admitted.ServiceID == "" || admitted.ServiceID == "copy-result" || admitted.Response == nil || admitted.Response.Service == nil {
				t.Fatalf("service was not admitted with opaque identity: %+v", admitted)
			}
			command.Kind, command.RequestID, command.ServiceID = "service-result", "foreign", "not-issued"
			command.ServiceResult = &nativeui.ServiceResult{Status: outcome}
			if reply := send(command); reply.Error == nil || calls != 0 {
				t.Fatalf("foreign service identity reached the continuation: %+v", reply)
			}
			command.RequestID, command.ServiceID = "settle-once", admitted.ServiceID
			command.Values, command.Revision = map[string]string{"title": "Latest editor value"}, 2
			if reply := send(command); reply.Error != nil || calls != 1 || received.Values["title"] != "Latest editor value" {
				t.Fatalf("service did not settle with latest fields: %+v, calls %d, input %+v", reply, calls, received)
			}
			command.Kind = "complete"
			for range 2 {
				reply := send(command)
				if reply.Error != nil || reply.Response == nil || reply.Response.Announcement != "Observed "+string(outcome) || calls != 1 {
					t.Fatalf("completion replayed or lost service settlement: %+v, calls %d", reply, calls)
				}
			}
		})
	}
}

func TestLiveWatchDoesNotHoldUpHTTPLeave(t *testing.T) {
	started := make(chan struct{}, 1)
	app, err := nativeui.New("/items", nativeui.Route{Path: "/items", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Live items", Live: &nativeui.Live{Version: "epoch:1"}, List: &nativeui.List{Header: "Items", SearchKey: "q"}}, nil
	}, Watch: func(ctx context.Context, request nativeui.Request, version string) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return "", ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webview.New(app, mobileui.IOS)
	if err != nil {
		t.Fatal(err)
	}
	hello := rendererHello(t, renderer)
	opened := rendererCommand(t, renderer, context.Background(), nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/items?q=current"})
	if opened.Error != nil || opened.DataVersion != "epoch:1" {
		t.Fatalf("live snapshot missing: %+v", opened)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan nativeui.Reply, 1)
	go func() {
		finished <- rendererCommand(t, renderer, ctx, nativeui.Command{Kind: "watch", Connection: hello.Connection, Instance: opened.Instance, DataVersion: opened.DataVersion, Values: map[string]string{"q": "current"}})
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("watch did not start")
	}
	left := make(chan nativeui.Reply, 1)
	go func() {
		left <- rendererCommand(t, renderer, ctx, nativeui.Command{Kind: "leave", Connection: hello.Connection, Instance: opened.Instance, Values: map[string]string{"q": "current"}})
	}()
	select {
	case reply := <-left:
		if reply.Error != nil {
			t.Fatal(reply.Error)
		}
	case <-ctx.Done():
		t.Fatal("long poll held up the leave command")
	}
	select {
	case reply := <-finished:
		if reply.Error == nil || reply.Changed {
			t.Fatalf("obsolete watch was not cancelled: %+v", reply)
		}
	case <-ctx.Done():
		t.Fatal("watch did not retire after leave")
	}
}

func TestServiceLiveAssetsAndBoundedAudioSSR(t *testing.T) {
	fixture := newFixture(t, mobileui.IOS)
	for name, markers := range map[string][]string{
		"gfm-webview-foreground": {"claim(candidate)", "release(candidate)"},
		"gfm-webview-services":   {"gofastrServices", "capabilities", "perform", "data-gfw-audio-record", "data-gfw-service-complete"},
		"gfm-webview-live":       {"data-gfw-live-retry", "data-gfw-live-cancel", "AbortController", "data-gfw-collection-content"},
	} {
		status, _, source := fixture.request(t, "GET", "/__gofastr/runtime/"+name+".js", "", "text/javascript", "")
		if status != http.StatusOK {
			t.Fatalf("%s asset unavailable: %d", name, status)
		}
		for _, marker := range markers {
			if !strings.Contains(source, marker) {
				t.Fatalf("%s missing %q", name, marker)
			}
		}
	}
	data := make([]byte, 46)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], 38)
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 2)
	app, err := nativeui.New("/audio", nativeui.Route{Path: "/audio", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Audio", Text: "Private recording", Attachments: []nativeui.Attachment{{ID: "audio-1", Name: "recording.wav", MediaType: "audio/wav", Size: len(data), Destination: "/audio",
			Audio: &audio.Document{File: files.Document{Name: "recording.wav", MediaType: "audio/wav", Data: data}, DurationMillis: 1000}}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := webview.New(app, mobileui.IOS)
	if err != nil {
		t.Fatal(err)
	}
	routes := router.New()
	renderer.UIHost().Mount(routes)
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audio", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `data-gfw-audio-pending="audio-1"`) || strings.Contains(response.Body.String(), "data:audio") {
		t.Fatalf("audio SSR must declare explicit, locally hydrated playback: %d %s", response.Code, response.Body.String())
	}
}
