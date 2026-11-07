package nativeui_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/nativeui/devtrace"
)

type observerMode struct {
	name     string
	observer func(*nativeui.Runtime) nativeui.Observer
}

func observerModes() []observerMode {
	return []observerMode{
		{"nil", func(*nativeui.Runtime) nativeui.Observer { return nil }},
		{"recorder", func(*nativeui.Runtime) nativeui.Observer { return devtrace.NewRecorder(64).Observe }},
		{"panic", func(*nativeui.Runtime) nativeui.Observer {
			return func(nativeui.TraceEvent) { panic("observer failure") }
		}},
	}
}

// settlementRun drives one scenario and returns every reply as normalized JSON.
type settlementRun struct {
	t       *testing.T
	runtime *nativeui.Runtime
	replies []string
}

func (run *settlementRun) send(command nativeui.Command) nativeui.Reply {
	run.t.Helper()
	command.Version = nativeui.ProtocolVersion
	encoded, _ := json.Marshal(command)
	response, err := run.runtime.CommandJSON(context.Background(), string(encoded))
	if err != nil {
		run.t.Fatal(err)
	}
	run.replies = append(run.replies, response)
	var reply nativeui.Reply
	if err := json.Unmarshal([]byte(response), &reply); err != nil {
		run.t.Fatal(err)
	}
	return reply
}

func (run *settlementRun) normalized() string {
	encoded, _ := json.Marshal(run.replies)
	return normalizeIDs(string(encoded))
}

var editorHello = &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{
	nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityHaptics: nativeui.SupportAvailable}}

func runSettlement(t *testing.T, mode observerMode, scenario func(*testing.T, *settlementRun, *reliabilityApp, *reliabilityStore)) (string, *reliabilityApp) {
	t.Helper()
	fixture, store := newReliabilityApp(t), reliabilityDisk(t)
	run := &settlementRun{t: t}
	var observer nativeui.Observer
	runtime, err := nativeui.NewRuntime(fixture.app, nativeui.RuntimeOptions{Drafts: store, Observer: func(event nativeui.TraceEvent) {
		if observer != nil {
			observer(event)
		}
	}})
	if mode.name == "nil" {
		runtime, err = nativeui.NewRuntime(fixture.app, nativeui.RuntimeOptions{Drafts: store})
	}
	if err != nil {
		t.Fatal(err)
	}
	observer = mode.observer(runtime)
	run.runtime = runtime
	scenario(t, run, fixture, store)
	return run.normalized(), fixture
}

func openEditor(run *settlementRun) nativeui.Command {
	hello := run.send(nativeui.Command{Kind: "hello", Hello: editorHello})
	opened := run.send(nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/editor"})
	return nativeui.Command{Connection: hello.Connection, Instance: opened.Instance, Revision: 1, RequestID: "save", Action: "save", Values: map[string]string{"title": "submitted"}}
}

func TestTraceObserversDoNotChangeSettlement(t *testing.T) {
	scenarios := map[string]func(*testing.T, *settlementRun, *reliabilityApp, *reliabilityStore){
		"unknown save is not retried": func(t *testing.T, run *settlementRun, fixture *reliabilityApp, _ *reliabilityStore) {
			fixture.save = func(context.Context, nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{}, errors.New("response lost after write")
			}
			command := openEditor(run)
			command.Kind = "action"
			if reply := run.send(command); reply.Error == nil || reply.Error.Code != "unknown" {
				t.Fatalf("unknown: %+v", reply)
			}
			command.Kind = "complete"
			first := run.send(command)
			second := run.send(command)
			if first.Editor == nil || !first.Editor.Unknown || second.Editor == nil || !second.Editor.Unknown {
				t.Fatalf("complete changed uncertainty: %+v %+v", first, second)
			}
			command.Kind, command.RequestID = "action", "retry"
			if reply := run.send(command); reply.Error == nil {
				t.Fatalf("retry admitted: %+v", reply)
			}
			if fixture.writes.Load() != 1 {
				t.Fatalf("writes = %d", fixture.writes.Load())
			}
		},
		"failed checkpoint blocks the action": func(t *testing.T, run *settlementRun, fixture *reliabilityApp, store *reliabilityStore) {
			command := openEditor(run)
			store.failPut = true
			command.Kind = "action"
			if reply := run.send(command); reply.Error == nil || reply.Error.Code != "draft" {
				t.Fatalf("draft failure: %+v", reply)
			}
			store.failPut = false
			if reply := run.send(command); reply.Error != nil {
				t.Fatalf("retry of unexecuted action: %+v", reply)
			}
			if fixture.writes.Load() != 1 || fixture.effects.Load() != 0 {
				t.Fatalf("writes=%d effects=%d", fixture.writes.Load(), fixture.effects.Load())
			}
		},
		"superseded completion emits no effects": func(t *testing.T, run *settlementRun, _ *reliabilityApp, _ *reliabilityStore) {
			command := openEditor(run)
			command.Kind = "action"
			run.send(command)
			newer := run.send(nativeui.Command{Kind: "open", Connection: command.Connection, Route: "/editor"})
			run.send(nativeui.Command{Kind: "checkpoint", Connection: command.Connection, Instance: newer.Instance, Revision: 1, Values: map[string]string{"title": "newer"}})
			command.Kind = "complete"
			if reply := run.send(command); reply.Error == nil || reply.Error.Code != "superseded" || reply.Response != nil || len(reply.EffectIDs) != 0 {
				t.Fatalf("superseded: %+v", reply)
			}
		},
	}
	for name, scenario := range scenarios {
		t.Run(name, func(t *testing.T) {
			var baseline string
			for _, mode := range observerModes() {
				replies, _ := runSettlement(t, mode, scenario)
				if baseline == "" {
					baseline = replies
				} else if replies != baseline {
					t.Fatalf("observer %s changed replies:\nwant %s\ngot  %s", mode.name, baseline, replies)
				}
			}
		})
	}
}

func TestTraceCorrelationJoinsOneRequestAndIsRuntimeLocal(t *testing.T) {
	fixture := newReliabilityApp(t)
	store := reliabilityDisk(t)
	recorders := []*devtrace.Recorder{devtrace.NewRecorder(0), devtrace.NewRecorder(0)}
	for _, recorder := range recorders {
		runtime, err := nativeui.NewRuntime(fixture.app, nativeui.RuntimeOptions{Drafts: store, Observer: recorder.Observe})
		if err != nil {
			t.Fatal(err)
		}
		run := &settlementRun{t: t, runtime: runtime}
		command := openEditor(run)
		command.Kind, command.Action, command.RequestID = "action", "effect", "same-request"
		run.send(command)
		command.Kind = "complete"
		completed := run.send(command)
		run.send(nativeui.Command{Kind: "ack", Connection: command.Connection, Instance: command.Instance, RequestID: command.RequestID,
			Receipt: &nativeui.EffectReceipt{ID: completed.EffectIDs["haptic"], Capability: nativeui.CapabilityHaptics, Status: nativeui.EffectCompleted}})
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{12}$`)
	correlations := make([]string, 2)
	for index, recorder := range recorders {
		kinds := map[string]string{}
		for _, event := range recorder.Events() {
			if event.Correlation != "" {
				kinds[event.Kind] = event.Correlation
			}
			if event.Kind == "ack" && (event.Receipt == nil || event.Receipt.Capability != nativeui.CapabilityHaptics || event.Receipt.Status != nativeui.EffectCompleted) {
				t.Fatalf("ack receipt: %+v", event)
			}
		}
		if kinds["action"] == "" || kinds["action"] != kinds["complete"] || kinds["complete"] != kinds["ack"] || !pattern.MatchString(kinds["action"]) {
			t.Fatalf("correlations within one runtime: %+v", kinds)
		}
		correlations[index] = kinds["action"]
	}
	if correlations[0] == correlations[1] {
		t.Fatal("two runtimes produced the same correlation; the salt is not per runtime")
	}
}

func TestTraceObserverMayInspectWithoutDeadlock(t *testing.T) {
	fixture := newReliabilityApp(t)
	var runtime *nativeui.Runtime
	var snapshots atomic.Int32
	runtime, err := nativeui.NewRuntime(fixture.app, nativeui.RuntimeOptions{Observer: func(nativeui.TraceEvent) {
		runtime.Inspect()
		snapshots.Add(1)
	}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		run := &settlementRun{t: t, runtime: runtime}
		command := openEditor(run)
		command.Kind = "action"
		run.send(command)
		command.Kind = "complete"
		run.send(command)
		run.send(nativeui.Command{Kind: "close", Connection: command.Connection, Instance: command.Instance})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("observer calling Inspect deadlocked")
	}
	if snapshots.Load() != 5 {
		t.Fatalf("observer calls = %d, want 5", snapshots.Load())
	}
}

func TestInspectDuringBlockedActionReturnsPromptly(t *testing.T) {
	fixture := newTraceFixture(t)
	runtime, err := nativeui.NewRuntime(fixture.app, nativeui.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	connection := wireHello(t, runtime, "android", allCapabilities())
	opened := wire(t, runtime, nativeui.Command{Kind: "open", Connection: connection, Route: "/items/one"})
	go wireAsync(t, runtime, nativeui.Command{Kind: "action", Connection: connection, Instance: opened.Instance, Revision: 1, RequestID: "blocked", Action: "block"})
	<-fixture.entered
	defer close(fixture.release)
	finished := make(chan nativeui.RuntimeSnapshot, 1)
	go func() { finished <- runtime.Inspect() }()
	select {
	case snapshot := <-finished:
		if len(snapshot.Sessions) != 1 || len(snapshot.Connections) != 1 || snapshot.Connections[0].Renderer != "android" {
			t.Fatalf("snapshot: %+v", snapshot)
		}
		session := snapshot.Sessions[0]
		if !session.Busy && (!session.Active || session.Actions.Unfinished != 1 || session.Route != "/items/{id}") {
			t.Fatalf("blocked action not visible: %+v", session)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Inspect blocked behind a running action")
	}
}

func TestInspectReportsBusyWithoutWaitingForSessionLock(t *testing.T) {
	fixture := newTraceFixture(t)
	held := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var runtime *nativeui.Runtime
	// The observer runs after locks are released, so to hold a session lock we
	// use a draft store whose Put blocks; persist runs under the session lock.
	store := &blockingDrafts{held: held, release: release, once: &once}
	runtime, err := nativeui.NewRuntime(draftedTraceApp(t), nativeui.RuntimeOptions{Drafts: store})
	if err != nil {
		t.Fatal(err)
	}
	_ = fixture
	connection := wireHello(t, runtime, "uikit", allCapabilities())
	opened := wire(t, runtime, nativeui.Command{Kind: "open", Connection: connection, Route: "/draft"})
	store.armed.Store(true)
	go wireAsync(t, runtime, nativeui.Command{Kind: "checkpoint", Connection: connection, Instance: opened.Instance, Revision: 1, Values: map[string]string{"title": "changed"}})
	<-held
	defer close(release)
	finished := make(chan nativeui.RuntimeSnapshot, 1)
	go func() { finished <- runtime.Inspect() }()
	select {
	case snapshot := <-finished:
		if len(snapshot.Sessions) != 1 || !snapshot.Sessions[0].Busy || snapshot.Sessions[0].Route != "" {
			t.Fatalf("busy session: %+v", snapshot.Sessions)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Inspect waited for a held session lock")
	}
}

type blockingDrafts struct {
	behavior.DraftStore
	armed   atomic.Bool
	held    chan struct{}
	release chan struct{}
	once    *sync.Once
}

func (store *blockingDrafts) Load(context.Context, string) (behavior.Snapshot, bool, error) {
	return behavior.Snapshot{}, false, nil
}

func (store *blockingDrafts) Put(context.Context, string, behavior.Snapshot) error {
	if store.armed.Load() {
		store.once.Do(func() {
			close(store.held)
			<-store.release
		})
	}
	return nil
}

func (store *blockingDrafts) ClearIfRevision(context.Context, string, behavior.DraftRevision) (bool, error) {
	return true, nil
}

func draftedTraceApp(t *testing.T) *nativeui.App {
	t.Helper()
	app, err := nativeui.New("/draft", nativeui.Route{Path: "/draft", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Draft", Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "title", Label: "Title", Kind: "text"}}, Draft: &nativeui.Draft{Key: "draft"}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// wireAsync is wire for a background goroutine: it reports with t.Error,
// because t.Fatal must run on the test goroutine.
func wireAsync(t *testing.T, runtime *nativeui.Runtime, command nativeui.Command) {
	command.Version = nativeui.ProtocolVersion
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Error(err)
		return
	}
	if _, err := runtime.CommandJSON(context.Background(), string(encoded)); err != nil {
		t.Error(err)
	}
}
