package nativeui_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/nativeui/devtrace"
)

type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *syncBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(data)
}

func (buffer *syncBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

// pathLike matches drive letters, separators and user directories that a
// leaked error message or file path would carry. Route patterns use "/", so
// separators are checked on the non-route text only.
var pathLike = regexp.MustCompile(`[A-Za-z]:\\|\\Users\\|/home/|/Users/|\\\\`)

func assertRedacted(t *testing.T, name, text string, secrets []string) {
	t.Helper()
	if strings.Contains(text, canary) || strings.Contains(strings.ToLower(text), strings.ToLower(canary)) {
		t.Errorf("%s leaked the canary:\n%s", name, text)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			t.Errorf("%s leaked raw identity or path %q:\n%s", name, secret, text)
		}
	}
	if pathLike.MatchString(text) {
		t.Errorf("%s contains a path-like pattern:\n%s", name, text)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(canary))
	if strings.Contains(text, encoded[:12]) {
		t.Errorf("%s leaked base64 canary bytes", name)
	}
}

func TestTracePrivacyCanaryNeverReachesDiagnostics(t *testing.T) {
	fixture := newTraceFixture(t)
	recorder := devtrace.NewRecorder(512)
	logs := &syncBuffer{}
	logger := devtrace.NewLogger(logs, 512)
	var runtime *nativeui.Runtime
	var inspected []string
	var inspectMu sync.Mutex
	runtime, err := nativeui.NewRuntime(fixture.app, nativeui.RuntimeOptions{Observer: func(event nativeui.TraceEvent) {
		recorder.Observe(event)
		logger.Observe(event)
		encoded, _ := json.Marshal(runtime.Inspect())
		inspectMu.Lock()
		inspected = append(inspected, string(encoded))
		inspectMu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	secrets := []string{dataDir, `C:\Users`}
	connection := wireHello(t, runtime, "uikit", allCapabilities())
	secrets = append(secrets, connection)

	list := wire(t, runtime, nativeui.Command{Kind: "open", Connection: connection, Route: "/items?q=" + canary + "&view=" + canary})
	if list.Error != nil {
		t.Fatalf("list open: %+v", list.Error)
	}
	secrets = append(secrets, list.Instance)
	archive := wire(t, runtime, nativeui.Command{Kind: "action", Connection: connection, Instance: list.Instance, RequestID: "req-" + canary, Action: "archive", Values: map[string]string{"row": canary, "q": canary, "view": canary}})
	// Row actions commit, so the unrecognized code becomes "unknown".
	if archive.Error == nil || archive.Error.Code != "unknown" {
		t.Fatalf("custom code must not pass through: %+v", archive)
	}
	wire(t, runtime, nativeui.Command{Kind: "action", Connection: connection, Instance: list.Instance, RequestID: "undeclared-" + canary, Action: canary})

	editor := wire(t, runtime, nativeui.Command{Kind: "open", Connection: connection, Route: "/items/" + canary + "?q=" + canary})
	if editor.Error != nil {
		t.Fatalf("editor open: %+v", editor.Error)
	}
	secrets = append(secrets, editor.Instance)
	revision := uint64(1)
	act := func(request, action string) nativeui.Reply {
		revision++
		return wire(t, runtime, nativeui.Command{Kind: "action", Connection: connection, Instance: editor.Instance, Revision: revision, RequestID: request, Action: action, Values: map[string]string{"title": canary}})
	}
	complete := func(request string) nativeui.Reply {
		return wire(t, runtime, nativeui.Command{Kind: "complete", Connection: connection, Instance: editor.Instance, Revision: revision, RequestID: request, Values: map[string]string{"title": canary}})
	}
	if reply := act("check-"+canary, "check"); reply.Error == nil || reply.Error.Code != "validation" {
		t.Fatalf("check: %+v", reply)
	}
	complete("check-" + canary)
	if reply := act("import-"+canary, "import"); reply.Error != nil {
		t.Fatalf("import: %+v", reply)
	}
	files := complete("import-" + canary)
	if files.FileID == "" {
		t.Fatalf("file task was not admitted: %+v", files)
	}
	secrets = append(secrets, files.FileID)
	fileResult := wire(t, runtime, nativeui.Command{Kind: "file-result", Connection: connection, Instance: editor.Instance, Revision: revision, RequestID: "file-" + canary, FileID: files.FileID,
		FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &files_document}})
	if fileResult.Error == nil {
		t.Fatalf("file continuation error was hidden: %+v", fileResult)
	}
	complete("file-" + canary)
	if reply := act("secret-"+canary, "secret"); reply.Error != nil {
		t.Fatalf("secret: %+v", reply)
	}
	service := complete("secret-" + canary)
	if service.ServiceID == "" {
		t.Fatalf("service not admitted: %+v", service)
	}
	secrets = append(secrets, service.ServiceID)
	wire(t, runtime, nativeui.Command{Kind: "service-result", Connection: connection, Instance: editor.Instance, Revision: revision, RequestID: "service-" + canary, ServiceID: service.ServiceID,
		ServiceResult: &nativeui.ServiceResult{Status: nativeui.EffectCompleted, Message: canary, SecureStorage: &securestore.Result{Found: true, Value: []byte(canary)}}})
	complete("service-" + canary)
	act("definition-"+canary, "fake-definition")
	if reply := act("save-"+canary, "save"); reply.Error == nil || reply.Error.Code != "unknown" {
		t.Fatalf("save: %+v", reply)
	}
	complete("save-" + canary)
	wire(t, runtime, nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: editor.Instance, Revision: revision + 1, Values: map[string]string{"title": canary + " newer"}})
	wire(t, runtime, nativeui.Command{Kind: "open", Connection: connection, Route: "/broken?q=" + canary})
	wire(t, runtime, nativeui.Command{Kind: "open", Connection: canary, Route: "/items"})
	wire(t, runtime, nativeui.Command{Kind: canary, Connection: connection, Instance: editor.Instance})
	if _, err := runtime.CommandJSON(t.Context(), `{"version":1,"kind":"`+canary+`","bogus":"`+canary+`"}`); err == nil {
		t.Fatal("invalid command decoded")
	}
	logger.Close()
	events := recorder.Events()
	if len(events) < 20 {
		t.Fatalf("expected a full trace, got %d events", len(events))
	}
	var jsonl bytes.Buffer
	if err := recorder.WriteJSONL(&jsonl); err != nil {
		t.Fatal(err)
	}
	assertRedacted(t, "recorder JSONL", jsonl.String(), secrets)
	assertRedacted(t, "log lines", logs.String(), secrets)
	inspectMu.Lock()
	assertRedacted(t, "Inspect JSON", strings.Join(inspected, "\n"), secrets)
	inspectMu.Unlock()
	screen, err := fixture.app.Render(t.Context(), "/items/"+canary+"?q="+canary, nil)
	if err != nil {
		t.Fatal(err)
	}
	outline, _ := json.Marshal(nativeui.Outline(fixture.app, screen))
	listScreen, _ := fixture.app.Render(t.Context(), "/items?q="+canary, nil)
	listOutline, _ := json.Marshal(nativeui.Outline(fixture.app, listScreen))
	assertRedacted(t, "Outline JSON", string(outline)+string(listOutline), secrets)

	byKind := map[string][]nativeui.TraceEvent{}
	for _, event := range events {
		byKind[event.Kind] = append(byKind[event.Kind], event)
	}
	if len(byKind["invalid"]) != 1 || len(byKind["other"]) != 1 {
		t.Fatalf("invalid and unknown kinds: %+v", byKind)
	}
	sawUndeclared, sawDefinition, sawFake, sawUnknown, sawFieldFilter := false, false, false, false, false
	for _, event := range events {
		sawUndeclared = sawUndeclared || event.Action == "undeclared"
		if event.Definition != nil && event.Definition.Part == "form" && event.Definition.Route == "/broken" && event.Kind == "open" {
			sawDefinition = true
		}
		if event.Definition != nil && event.Definition.Route == "" && event.Definition.Part == "screen" && event.Cause == "*errors.errorString" {
			// The fake DefinitionError's unregistered route and unknown part were dropped.
			sawFake = true
		}
		if event.Kind == "action" && event.Outcome == "unknown" && event.Action == "save" && event.Cause == "*errors.errorString" && event.Route == "/items/{id}" {
			sawUnknown = true
		}
		if event.Kind == "action" && event.Action == "check" && strings.Join(event.FieldKeys, ",") == "title" && event.UndeclaredFields == 1 && event.Cause == "" {
			sawFieldFilter = true
		}
		if event.Kind == "action" && event.Action == "archive" && (event.Outcome != "unknown" || event.Cause != "*nativeui.ActionError" || len(event.FieldKeys) != 0) {
			t.Errorf("custom ActionError leaked code, cause or keys: %+v", event)
		}
	}
	if !sawUndeclared || !sawDefinition || !sawFake || !sawUnknown || !sawFieldFilter {
		t.Fatalf("trace lacks expected redacted facts undeclared=%t definition=%t unknown=%t fields=%t fake=%t\n%s", sawUndeclared, sawDefinition, sawUnknown, sawFieldFilter, sawFake, jsonl.String())
	}
	t.Log(logs.String())
	if logger.Dropped() != 0 {
		t.Fatalf("logger dropped %d events", logger.Dropped())
	}
	if !strings.Contains(logs.String(), "gofastr-trace seq=") || !strings.Contains(logs.String(), "route=/items/{id} action=save") {
		t.Fatalf("log format:\n%s", logs.String())
	}
}

var files_document = files.Document{Name: canary + ".txt", MediaType: "text/plain", Data: []byte(canary)}
