package gallery

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
}

func (r *recorder) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var actions []string
	for _, call := range r.calls {
		if !strings.HasSuffix(call, " screen") {
			actions = append(actions, call)
		}
	}
	return actions
}

func everything() map[nativeui.Capability]nativeui.Support {
	capabilities := map[nativeui.Capability]nativeui.Support{}
	for _, capability := range KnownCapabilities {
		capabilities[capability] = nativeui.SupportAvailable
	}
	return capabilities
}

// reachable walks rows and tabs from Root with plain Render and returns every
// concrete destination in discovery order.
func reachable(t *testing.T, app *nativeui.App) []string {
	t.Helper()
	seen := map[string]bool{Root: true}
	queue := []string{Root}
	for index := 0; index < len(queue); index++ {
		screen, err := app.Render(context.Background(), queue[index], nil)
		if err != nil {
			t.Fatalf("render %s: %v", queue[index], err)
		}
		var next []string
		for _, tab := range screen.Tabs {
			next = append(next, tab.Route)
		}
		if screen.List != nil {
			for _, row := range screen.List.Rows {
				if row.Destination != "" && !row.Disabled {
					next = append(next, row.Destination)
				}
			}
		}
		if screen.Protection != nil {
			next = append(next, screen.Protection.LockRoute)
		}
		for _, destination := range next {
			if !seen[destination] {
				seen[destination] = true
				queue = append(queue, destination)
			}
		}
	}
	return append(queue, "/feedback/navigation/pushed", "/feedback/sheet/open")
}

func TestEveryRouteRendersAndIsReachable(t *testing.T) {
	app, err := New()
	if err != nil {
		t.Fatal(err)
	}
	patterns := map[string]bool{}
	for _, destination := range reachable(t, app) {
		if _, err := app.Render(context.Background(), destination, nil); err != nil {
			t.Fatalf("render %s: %v", destination, err)
		}
		pattern, _ := app.RoutePattern(destination)
		patterns[pattern] = true
	}
	for _, route := range app.Describe() {
		if !patterns[route.Path] {
			t.Errorf("route %s is registered but not reachable from %s", route.Path, Root)
		}
	}
	for _, missing := range []string{"/feedback/present/nope", "/feedback/effects/nope", "/lists/rows/0", "/lists/rows/61"} {
		var failure *nativeui.ActionError
		if _, err := app.Render(context.Background(), missing, nil); !errors.As(err, &failure) || failure.Code != "not_found" {
			t.Fatalf("%s: %v", missing, err)
		}
	}
}

type session struct {
	t          *testing.T
	runtime    *nativeui.Runtime
	connection string
}

func connect(t *testing.T, app *nativeui.App, renderer string, capabilities map[nativeui.Capability]nativeui.Support) *session {
	t.Helper()
	drafts, err := behavior.NewFileDraftStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{Drafts: drafts})
	if err != nil {
		t.Fatal(err)
	}
	s := &session{t: t, runtime: runtime}
	hello := s.send(nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: renderer, Capabilities: capabilities}})
	if hello.Error != nil {
		t.Fatalf("hello: %+v", hello.Error)
	}
	s.connection = hello.Connection
	return s
}

func (s *session) send(command nativeui.Command) nativeui.Reply {
	s.t.Helper()
	command.Version = nativeui.ProtocolVersion
	if command.Kind != "hello" {
		command.Connection = s.connection
	}
	encoded, _ := json.Marshal(command)
	response, err := s.runtime.CommandJSON(context.Background(), string(encoded))
	if err != nil {
		s.t.Fatal(err)
	}
	var reply nativeui.Reply
	if err := json.Unmarshal([]byte(response), &reply); err != nil {
		s.t.Fatal(err)
	}
	return reply
}

func TestEveryDeclaredActionDispatchesAndCompletes(t *testing.T) {
	calls := &recorder{}
	app, err := newApp(calls.record)
	if err != nil {
		t.Fatal(err)
	}
	s := connect(t, app, "uikit", everything())
	dispatched := map[string]bool{}
	for _, destination := range reachable(t, app) {
		opened := s.send(nativeui.Command{Kind: "open", Route: destination})
		if opened.Error != nil {
			t.Fatalf("open %s: %+v", destination, opened.Error)
		}
		screen := opened.Screen
		pattern, _ := app.RoutePattern(destination)
		type attempt struct {
			id   string
			row  string
			form map[string]string
		}
		var attempts []attempt
		values := map[string]string{}
		if screen.Form != nil {
			for _, field := range screen.Form.Fields {
				values[field.Key] = field.Value
			}
			values["name"] = "Ada"
		}
		for _, action := range screen.Actions {
			attempts = append(attempts, attempt{id: action.ID, form: values})
		}
		if screen.List != nil {
			for _, row := range screen.List.Rows {
				for _, action := range append(append([]nativeui.RowAction(nil), row.LeadingActions...), row.TrailingActions...) {
					if !dispatched[pattern+" "+action.ID] {
						attempts = append(attempts, attempt{id: action.ID, row: row.ID})
						dispatched[pattern+" "+action.ID] = true
					}
				}
			}
		}
		for index, try := range attempts {
			// A fresh instance per action, so a pending file, service or
			// presentation task from one demo cannot block the next.
			fresh := s.send(nativeui.Command{Kind: "open", Route: destination})
			revision := uint64(1)
			request := strings.ReplaceAll(pattern, "/", "-") + "-" + try.id + "-" + string(rune('a'+index))
			command := nativeui.Command{Kind: "action", Instance: fresh.Instance, Revision: revision, RequestID: request, Action: try.id, Values: map[string]string{}}
			for key, value := range try.form {
				command.Values[key] = value
			}
			if try.row != "" {
				command.Values["row"] = try.row
			}
			reply := s.send(command)
			if pattern == "/gallery/unknown" {
				if reply.Error == nil || reply.Error.Code != "unknown" {
					t.Fatalf("unknown demo: %+v", reply)
				}
				continue
			}
			if reply.Error != nil {
				t.Fatalf("%s %s: %+v", destination, try.id, reply.Error)
			}
			dispatched[pattern+" "+try.id] = true
			command.Kind = "complete"
			if completed := s.send(command); completed.Error != nil {
				t.Fatalf("complete %s %s: %+v", destination, try.id, completed.Error)
			}
			s.send(nativeui.Command{Kind: "close", Instance: fresh.Instance})
		}
		s.send(nativeui.Command{Kind: "close", Instance: opened.Instance})
	}
	for _, route := range app.Describe() {
		for _, action := range route.Actions {
			if !dispatched[route.Path+" "+action] && route.Path != "/gallery/unknown" {
				t.Errorf("action %s on %s was never dispatched", action, route.Path)
			}
		}
	}
	if len(calls.actions()) < 20 {
		t.Fatalf("too few handler calls: %v", calls.actions())
	}
}

func TestValidationReturnsFieldKeys(t *testing.T) {
	app, _ := New()
	s := connect(t, app, "uikit", everything())
	opened := s.send(nativeui.Command{Kind: "open", Route: "/gallery/fields"})
	reply := s.send(nativeui.Command{Kind: "action", Instance: opened.Instance, Revision: 1, RequestID: "v", Action: "validate", Values: map[string]string{"name": " "}})
	if reply.Error == nil || reply.Error.Code != "validation" || reply.Error.Fields["name"] == "" || len(reply.Error.Fields) != 1 {
		t.Fatalf("validation: %+v", reply.Error)
	}
}

func TestUnknownSaveBlocksRetryAndCompleteIsReadOnly(t *testing.T) {
	calls := &recorder{}
	app, _ := newApp(calls.record)
	s := connect(t, app, "uikit", everything())
	opened := s.send(nativeui.Command{Kind: "open", Route: "/gallery/unknown"})
	command := nativeui.Command{Kind: "action", Instance: opened.Instance, Revision: 1, RequestID: "first", Action: "save", Values: map[string]string{"text": "Example"}}
	if reply := s.send(command); reply.Error == nil || reply.Error.Code != "unknown" {
		t.Fatalf("first save: %+v", reply)
	}
	command.Kind = "complete"
	first, second := s.send(command), s.send(command)
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) || first.Editor == nil || !first.Editor.Unknown {
		t.Fatalf("complete is not read-only:\n%s\n%s", firstJSON, secondJSON)
	}
	command.Kind, command.RequestID = "action", "retry"
	if reply := s.send(command); reply.Error == nil {
		t.Fatalf("retry admitted: %+v", reply)
	}
	if got := calls.actions(); !slices.Equal(got, []string{"/gallery/unknown save"}) {
		t.Fatalf("save ran %v", got)
	}
	reopened := s.send(nativeui.Command{Kind: "open", Route: "/gallery/unknown"})
	if reopened.Error != nil || reopened.Editor != nil && reopened.Editor.Unknown {
		t.Fatalf("reopen did not reset the demo: %+v", reopened)
	}
}

func TestRequirementsAreHonest(t *testing.T) {
	app, _ := New()
	refusals := 0
	for _, destination := range reachable(t, app) {
		screen, err := app.Render(context.Background(), destination, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, requirement := range screen.Requires {
			if requirement.Capability == nativeui.CapabilityNavigation || requirement.Capability == nativeui.CapabilityEditor {
				// hello refuses a renderer without these, so no connection lacks them.
				continue
			}
			calls := &recorder{}
			counted, _ := newApp(calls.record)
			capabilities := everything()
			capabilities[requirement.Capability] = nativeui.SupportUnavailable
			s := connect(t, counted, "android", capabilities)
			reply := s.send(nativeui.Command{Kind: "open", Route: destination})
			if reply.Error == nil || reply.Error.Code != "unsupported" || reply.Instance != "" {
				t.Fatalf("%s opened without %s: %+v", destination, requirement.Capability, reply)
			}
			if actions := calls.actions(); len(actions) != 0 {
				t.Fatalf("%s ran handlers while refused: %v", destination, actions)
			}
			refusals++
			if requirement.AllowFallback {
				capabilities[requirement.Capability] = nativeui.SupportFallback
				fallback := connect(t, counted, "android", capabilities)
				if reply := fallback.send(nativeui.Command{Kind: "open", Route: destination}); reply.Error != nil {
					t.Fatalf("%s refused a declared fallback for %s: %+v", destination, requirement.Capability, reply.Error)
				}
			}
		}
	}
	if refusals < 12 {
		t.Fatalf("only %d requirement checks ran", refusals)
	}
	// Screens with real device effects must declare what they need.
	for destination, capability := range map[string]nativeui.Capability{"/device/files": nativeui.CapabilityFiles, "/device/clipboard": nativeui.CapabilityClipboard,
		"/device/share-text": nativeui.CapabilitySharing, "/device/protected": nativeui.CapabilityPrivacy, "/lists/live": nativeui.CapabilityLive,
		"/feedback/present/menu": nativeui.CapabilityPresentation, "/feedback/effects/haptic": nativeui.CapabilityHaptics, "/feedback/sheet": nativeui.CapabilitySheets} {
		screen, _ := app.Render(context.Background(), destination, nil)
		if !slices.ContainsFunc(screen.Requires, func(requirement nativeui.Requirement) bool { return requirement.Capability == capability }) {
			t.Errorf("%s does not require %s", destination, capability)
		}
	}
}

func TestCapabilityMatrixReportsContractOrLegacyPath(t *testing.T) {
	app, _ := New()
	legacy, err := app.Render(context.Background(), "/device/capabilities", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(legacy.Text, "privacy/v1: "+LegacyRenderPath) || !strings.Contains(legacy.Text, MatrixFooter) {
		t.Fatalf("legacy matrix:\n%s", legacy.Text)
	}
	capabilities := map[nativeui.Capability]nativeui.Support{nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable,
		nativeui.CapabilitySheets: nativeui.SupportFallback}
	s := connect(t, app, "webview", capabilities)
	opened := s.send(nativeui.Command{Kind: "open", Route: "/device/capabilities"})
	if opened.Error != nil {
		t.Fatal(opened.Error)
	}
	for _, want := range []string{"Renderer: webview, protocol 1", "sheets/v1: fallback", "editor/v1: available", "privacy/v1: not advertised", MatrixFooter} {
		if !strings.Contains(opened.Screen.Text, want) {
			t.Fatalf("matrix lacks %q:\n%s", want, opened.Screen.Text)
		}
	}
}

func TestFileImportReportsOnlyTypeAndSize(t *testing.T) {
	app, _ := New()
	s := connect(t, app, "uikit", everything())
	opened := s.send(nativeui.Command{Kind: "open", Route: "/device/files"})
	command := nativeui.Command{Kind: "action", Instance: opened.Instance, Revision: 1, RequestID: "import", Action: "import"}
	s.send(command)
	command.Kind = "complete"
	task := s.send(command)
	if task.FileID == "" {
		t.Fatalf("file task: %+v", task)
	}
	result := s.send(nativeui.Command{Kind: "file-result", Instance: opened.Instance, Revision: 1, RequestID: "picked", FileID: task.FileID,
		FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: fileDocument("secret-name.txt", "text/plain", "private body")}})
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "secret-name") || strings.Contains(string(encoded), "private body") || strings.Contains(string(encoded), "<script>") ||
		!strings.Contains(string(encoded), "12 bytes of text/plain") {
		t.Fatalf("import reply: %s", encoded)
	}
}

func TestPresentationResultAnnouncesChoice(t *testing.T) {
	app, _ := New()
	s := connect(t, app, "uikit", everything())
	opened := s.send(nativeui.Command{Kind: "open", Route: "/feedback/present/multiple"})
	command := nativeui.Command{Kind: "action", Instance: opened.Instance, Revision: 1, RequestID: "show", Action: "show"}
	s.send(command)
	command.Kind = "complete"
	shown := s.send(command)
	if shown.PresentationID == "" || shown.Response == nil || shown.Response.Presentation == nil || shown.Response.Presentation.Selection == nil {
		t.Fatalf("presentation: %+v", shown)
	}
	reply := s.send(nativeui.Command{Kind: "respond", Instance: opened.Instance, Revision: 1, RequestID: "chose", PresentationID: shown.PresentationID,
		Choice: "choose", Selected: []string{"red", "green"}})
	if reply.Error != nil || reply.Response == nil || reply.Response.Announcement != "You chose choose. Selected: green, red." {
		t.Fatalf("respond: %+v %+v", reply.Error, reply.Response)
	}
	tooMany := s.send(nativeui.Command{Kind: "open", Route: "/feedback/present/multiple"})
	command = nativeui.Command{Kind: "action", Instance: tooMany.Instance, Revision: 1, RequestID: "show2", Action: "show"}
	s.send(command)
	command.Kind = "complete"
	shown = s.send(command)
	if reply := s.send(nativeui.Command{Kind: "respond", Instance: tooMany.Instance, Revision: 1, RequestID: "three", PresentationID: shown.PresentationID,
		Choice: "choose", Selected: []string{"red", "green", "blue"}}); reply.Error == nil {
		t.Fatalf("three of two accepted: %+v", reply)
	}
}

func fileDocument(name, mediaType, body string) *files.Document {
	return &files.Document{Name: name, MediaType: mediaType, Data: []byte(body)}
}
