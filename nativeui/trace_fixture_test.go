package nativeui_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

// canary is planted in every value, label, message, query, file name, file
// body and secret the trace fixture handles. Diagnostics must never echo it.
const canary = "CANARY-7f3a"

type traceFixture struct {
	app       *nativeui.App
	release   chan struct{}
	entered   chan struct{}
	calls     atomic.Int32
	blockOnce sync.Once
}

func newTraceFixture(t *testing.T) *traceFixture {
	t.Helper()
	fixture := &traceFixture{release: make(chan struct{}), entered: make(chan struct{}, 1)}
	list := func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "List " + canary, Prompt: canary, List: &nativeui.List{SearchKey: "q", Header: canary,
			Filters: []nativeui.Field{{Key: "view", Label: canary, Kind: "select", Value: request.Values["view"], Options: []nativeui.Option{{Value: "", Label: canary}, {Value: "all", Label: canary}, {Value: canary, Label: canary}}}},
			Rows:    []nativeui.Row{{ID: canary, Title: canary, Subtitle: canary, Destination: "/items/" + canary + "?q=" + canary, TrailingActions: []nativeui.RowAction{{ID: "archive", Label: canary}}}}}}, nil
	}
	editor := func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: canary, Prompt: canary, Form: &nativeui.Form{Status: canary, Fields: []nativeui.Field{{Key: "title", Label: canary, Placeholder: canary, Kind: "text", Value: canary + request.Params["id"]}}},
			Actions: []nativeui.Action{{ID: "save", Label: canary, Commit: true}, {ID: "check", Label: canary}, {ID: "import", Label: canary}, {ID: "secret", Label: canary}, {ID: "block", Label: canary}, {ID: "fake-definition", Label: canary}}}, nil
	}
	editorActions := map[string]nativeui.Handle{
		"save": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			fixture.calls.Add(1)
			return nativeui.Result{}, errors.New("write lost for " + canary + ` at C:\Users\someone\data\` + canary)
		},
		"check": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			fixture.calls.Add(1)
			return nativeui.Result{}, &nativeui.ActionError{Code: "validation", Message: canary, Fields: map[string]string{"title": canary, canary: canary}}
		},
		"import": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			fixture.calls.Add(1)
			return nativeui.Result{Files: &nativeui.FileRequest{ID: "import", Pick: &files.Pick{Title: canary, MaxBytes: 1 << 10}}}, nil
		},
		"secret": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			fixture.calls.Add(1)
			return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "secret", SecureStorage: &securestore.Request{Operation: securestore.Get, Key: "demo", Protection: securestore.Device}}}, nil
		},
		"block": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			fixture.calls.Add(1)
			fixture.blockOnce.Do(func() {
				fixture.entered <- struct{}{}
				<-fixture.release
			})
			return nativeui.Result{Announcement: canary}, nil
		},
		"fake-definition": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{}, &nativeui.DefinitionError{Route: "/" + canary, Part: canary, Err: errors.New(canary)}
		},
	}
	listActions := map[string]nativeui.Handle{
		"archive": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			fixture.calls.Add(1)
			return nativeui.Result{}, &nativeui.ActionError{Code: canary, Message: canary, Fields: map[string]string{canary: canary}}
		},
	}
	broken := func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: canary, Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "when", Label: canary, Kind: "date", Value: canary}}}}, nil
	}
	app, err := nativeui.New("/items",
		nativeui.Route{Path: "/items", Screen: list, Actions: listActions},
		nativeui.Route{Path: "/items/{id}", Screen: editor, Actions: editorActions,
			Files: map[string]nativeui.FileRespond{"import": func(_ context.Context, _ nativeui.Request, result nativeui.FileResult) (nativeui.Result, error) {
				return nativeui.Result{}, errors.New("rejected " + result.File.Name + string(result.File.Data))
			}},
			Services: map[string]nativeui.ServiceRespond{"secret": func(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
				return nativeui.Result{Announcement: string(result.SecureStorage.Value)}, nil
			}}},
		nativeui.Route{Path: "/broken", Screen: broken},
	)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = app
	return fixture
}

func allCapabilities() map[nativeui.Capability]nativeui.Support {
	capabilities := map[nativeui.Capability]nativeui.Support{}
	for _, capability := range []nativeui.Capability{nativeui.CapabilityNavigation, nativeui.CapabilityEditor, nativeui.CapabilityDrafts, nativeui.CapabilityShare,
		nativeui.CapabilityHaptics, nativeui.CapabilitySheets, nativeui.CapabilityAnnouncement, nativeui.CapabilityPresentation, nativeui.CapabilityFiles,
		nativeui.CapabilitySecureStorage, nativeui.CapabilityClipboard, nativeui.CapabilitySharing, nativeui.CapabilityLive} {
		capabilities[capability] = nativeui.SupportAvailable
	}
	return capabilities
}

func wire(t *testing.T, runtime *nativeui.Runtime, command nativeui.Command) nativeui.Reply {
	t.Helper()
	command.Version = nativeui.ProtocolVersion
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	response, err := runtime.CommandJSON(context.Background(), string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	var reply nativeui.Reply
	if err := json.Unmarshal([]byte(response), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func wireHello(t *testing.T, runtime *nativeui.Runtime, renderer string, capabilities map[nativeui.Capability]nativeui.Support) string {
	t.Helper()
	reply := wire(t, runtime, nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: renderer, Capabilities: capabilities}})
	if reply.Error != nil {
		t.Fatalf("hello: %+v", reply.Error)
	}
	return reply.Connection
}

var hexIdentity = regexp.MustCompile(`[0-9a-f]{32}`)

// normalizeIDs replaces random 128-bit handles by their order of appearance.
func normalizeIDs(encoded string) string {
	seen := map[string]string{}
	return hexIdentity.ReplaceAllStringFunc(encoded, func(identity string) string {
		if seen[identity] == "" {
			seen[identity] = "<id" + string(rune('A'+len(seen))) + ">"
		}
		return seen[identity]
	})
}
