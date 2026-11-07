//go:build gofastr_debugtrace

package notesnative

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/nativeui/devtrace"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.Buffer.Write(data)
}

// TestDebugTraceNeverEchoesNoteContent drives Notes through the bind export
// with the debug observer installed and checks the trace for a canary.
func TestDebugTraceNeverEchoesNoteContent(t *testing.T) {
	const canary = "CANARY-7f3a"
	recorder := devtrace.NewRecorder(devtrace.MaxSize)
	logs := &lockedBuffer{}
	logger := devtrace.NewLogger(logs, devtrace.MaxSize)
	tap := nativeui.Observer(func(event nativeui.TraceEvent) { recorder.Observe(event); logger.Observe(event) })
	debugTap.Store(&tap)
	defer debugTap.Store(nil)
	defer func() { _ = Stop(); owner.SetExtractor(nil) }()
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	var raw []string
	call := func(command nativeui.Command) nativeui.Reply {
		t.Helper()
		command.Version = nativeui.ProtocolVersion
		payload, _ := json.Marshal(command)
		encoded, err := Command(string(payload))
		if err != nil {
			t.Fatal(err)
		}
		var reply nativeui.Reply
		if err := json.Unmarshal([]byte(encoded), &reply); err != nil {
			t.Fatal(err)
		}
		raw = append(raw, reply.Connection, reply.Instance, reply.ServiceID, reply.FileID, reply.PresentationID)
		return reply
	}
	connection := call(nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityDrafts: nativeui.SupportAvailable,
		nativeui.CapabilitySharing: nativeui.SupportAvailable, nativeui.CapabilityAnnouncement: nativeui.SupportAvailable}}}).Connection
	opened := call(nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/new"})
	values := map[string]string{"title": canary + " title", "body": canary + " body"}
	call(nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: opened.Instance, Revision: 1, Values: values})
	save := nativeui.Command{Kind: "action", Connection: connection, Instance: opened.Instance, Revision: 1, RequestID: "save-" + canary, Action: "save", Values: values}
	call(save)
	save.Kind = "complete"
	saved := call(save)
	route := ""
	if saved.Response != nil && saved.Response.Saved != nil {
		route = saved.Response.Saved.Route
	}
	if route == "" {
		t.Fatalf("save did not report a note route: %+v", saved)
	}
	call(nativeui.Command{Kind: "open", Connection: connection, Route: route + "?q=" + canary})
	call(nativeui.Command{Kind: "open", Connection: connection, Route: "/notes?search=" + canary})
	call(nativeui.Command{Kind: "open", Connection: connection, Route: "/notes/" + canary})
	call(nativeui.Command{Kind: "action", Connection: connection, Instance: opened.Instance, Revision: 2, RequestID: "x", Action: canary})
	logger.Close()

	var jsonl bytes.Buffer
	if err := recorder.WriteJSONL(&jsonl); err != nil {
		t.Fatal(err)
	}
	text := jsonl.String() + logs.String()
	if strings.Contains(text, canary) || strings.Contains(text, directory) || regexp.MustCompile(`[A-Za-z]:[\\/]`).MatchString(text) {
		t.Fatalf("trace leaked note content or a path:\n%s", text)
	}
	routeID := strings.TrimPrefix(route, "/notes/")
	for _, secret := range append(raw, routeID) {
		if secret != "" && strings.Contains(text, secret) {
			t.Fatalf("trace leaked raw identity %q:\n%s", secret, text)
		}
	}
	if !strings.Contains(logs.String(), "route=/notes/{id}") || !strings.Contains(logs.String(), "action=save") || !strings.Contains(logs.String(), "action=undeclared") {
		t.Fatalf("trace lacks expected redacted facts:\n%s", logs.String())
	}
}
