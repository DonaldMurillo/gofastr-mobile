package nativeui

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/securestore"
)

func TestLockFencesActiveProtectedServiceAndScrubsCachedContent(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var locked atomic.Int32
	app, err := New("/locked", Route{Path: "/locked", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Locked", Text: "Authenticate"}, nil
	}},
		Route{Path: "/private", Screen: func(context.Context, Request) (Screen, error) {
			return Screen{Title: "Protected", Protection: &ScreenProtection{LockRoute: "/locked"}, Form: &Form{Fields: []Field{{Key: "body", Label: "Note", Kind: "multiline", Value: "private fixture text"}}}, Actions: []Action{{ID: "read", Label: "Read"}}}, nil
		}, Actions: map[string]Handle{
			"read": func(context.Context, Request) (Result, error) {
				return Result{Service: &ServiceRequest{ID: "read", Commit: true, SecureStorage: &securestore.Request{Operation: securestore.Get, Key: "note", Protection: securestore.Biometric, Reason: "Open note"}}}, nil
			},
		}, Services: map[string]ServiceRespond{"read": func(context.Context, Request, ServiceResult) (Result, error) {
			close(entered)
			<-release
			return Result{Announcement: "private returned fixture text"}, nil
		}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(app, RuntimeOptions{OnLock: func() { locked.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	hello := serviceCommand(t, ctx, runtime, Command{Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "android", Capabilities: map[Capability]Support{CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, CapabilityPrivacy: SupportAvailable, CapabilitySecureStorage: SupportAvailable}}})
	opened := serviceCommand(t, ctx, runtime, Command{Kind: "open", Connection: hello.Connection, Route: "/private"})
	other := serviceCommand(t, ctx, runtime, Command{Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "android", Capabilities: map[Capability]Support{CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, CapabilityPrivacy: SupportAvailable}}})
	otherOpened := serviceCommand(t, ctx, runtime, Command{Kind: "open", Connection: other.Connection, Route: "/private"})
	if otherOpened.Error != nil {
		t.Fatal(otherOpened.Error)
	}
	base := Command{Connection: hello.Connection, Instance: opened.Instance, Action: "read", RequestID: "read"}
	base.Kind = "action"
	if reply := serviceCommand(t, ctx, runtime, base); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	base.Kind = "complete"
	shown := serviceCommand(t, ctx, runtime, base)
	if shown.Error != nil || shown.ServiceID == "" {
		t.Fatal("protected service was not admitted")
	}
	result := make(chan Reply, 1)
	go func() {
		result <- runtime.Handle(ctx, Command{Version: 1, Kind: "service-result", Connection: hello.Connection, Instance: opened.Instance, RequestID: "received", ServiceID: shown.ServiceID, ServiceResult: &ServiceResult{Status: EffectCompleted, SecureStorage: &securestore.Result{Found: true, Value: []byte("read bytes")}}})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("service did not start")
	}
	if reply := serviceCommand(t, ctx, runtime, Command{Kind: "lock", Connection: hello.Connection}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	close(release)
	var reply Reply
	select {
	case reply = <-result:
	case <-time.After(5 * time.Second):
		t.Fatal("revoked service did not settle")
	}
	encoded, _ := json.Marshal(reply)
	if reply.Error == nil || strings.Contains(string(encoded), "private returned") {
		t.Fatal("locked service returned cached protected content")
	}
	session := runtime.sessions[opened.Instance]
	if session.editor != nil || session.screen.Form != nil || session.service != nil || len(session.actions) != 0 || locked.Load() != 1 {
		t.Fatal("protected runtime retained live content after lock")
	}
	if reply := serviceCommand(t, ctx, runtime, Command{Kind: "complete", Connection: hello.Connection, Instance: opened.Instance, RequestID: "received"}); reply.Error == nil {
		t.Fatal("locked service replay remained readable")
	}
	if reply := serviceCommand(t, ctx, runtime, Command{Kind: "checkpoint", Connection: other.Connection, Instance: otherOpened.Instance}); reply.Error == nil {
		t.Fatal("application-wide lock retained another connection's protected editor")
	}
}

func TestProtectedFormRejectsDurableDrafts(t *testing.T) {
	app, err := New("/locked", Route{Path: "/locked", Screen: func(context.Context, Request) (Screen, error) { return Screen{Title: "Locked", Text: "Locked"}, nil }}, Route{Path: "/private", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Private", Protection: &ScreenProtection{LockRoute: "/locked"}, Form: &Form{Fields: []Field{{Key: "body", Label: "Note", Kind: "multiline"}}, Draft: &Draft{Key: "plaintext"}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Render(context.Background(), "/private", nil); err == nil {
		t.Fatal("protected form could write plaintext drafts")
	}
}

func TestLockFencesProtectedScreenStillOpening(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	app, err := New("/locked", Route{Path: "/locked", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Locked", Text: "Authenticate"}, nil
	}}, Route{Path: "/private", Screen: func(context.Context, Request) (Screen, error) {
		close(entered)
		<-release
		return Screen{Title: "Private", Text: "private opening fixture", Protection: &ScreenProtection{LockRoute: "/locked"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(app, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hello := serviceCommand(t, context.Background(), runtime, Command{Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "android", Capabilities: map[Capability]Support{CapabilityPrivacy: SupportAvailable, CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable}}})
	if hello.Error != nil {
		t.Fatal(hello.Error)
	}
	result := make(chan Reply, 1)
	go func() {
		result <- runtime.Handle(context.Background(), Command{Version: 1, Kind: "open", Connection: hello.Connection, Route: "/private"})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("screen did not start opening")
	}
	if reply := serviceCommand(t, context.Background(), runtime, Command{Kind: "lock", Connection: hello.Connection}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	close(release)
	select {
	case reply := <-result:
		if reply.Error == nil || reply.Screen != nil || reply.Instance != "" {
			t.Fatal("locked in-flight open returned private content")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("opening screen did not settle")
	}
}
