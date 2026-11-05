package nativeui_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

type reliabilityStore struct {
	behavior.DraftStore
	failPut       bool
	afterPut      bool
	failClear     bool
	conflictClear bool
}

func (store *reliabilityStore) Put(ctx context.Context, key string, snapshot behavior.Snapshot) error {
	if store.failPut && !store.afterPut {
		return errors.New("checkpoint unavailable")
	}
	if err := store.DraftStore.Put(ctx, key, snapshot); err != nil {
		return err
	}
	if store.failPut {
		return errors.New("checkpoint acknowledgement lost")
	}
	return nil
}

func (store *reliabilityStore) ClearIfRevision(ctx context.Context, key string, revision behavior.DraftRevision) (bool, error) {
	if store.failClear {
		return false, errors.New("cleanup unavailable")
	}
	if store.conflictClear {
		return false, nil
	}
	return store.DraftStore.ClearIfRevision(ctx, key, revision)
}

type reliabilityApp struct {
	app     *nativeui.App
	mu      sync.Mutex
	title   string
	writes  atomic.Int32
	effects atomic.Int32
	save    func(context.Context, nativeui.Request) (nativeui.Result, error)
}

func newReliabilityApp(t *testing.T) *reliabilityApp {
	t.Helper()
	fixture := &reliabilityApp{title: "saved"}
	screen := func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		key := "draft"
		if request.Route == "/created" {
			key = "saved-draft"
		}
		return nativeui.Screen{Title: "Editor", Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "title", Label: "Title", Kind: "text", Value: fixture.title}}, Draft: &nativeui.Draft{Key: key}}, Actions: []nativeui.Action{{ID: "save", Label: "Save", Commit: true}, {ID: "effect", Label: "Effect"}}}, nil
	}
	actions := map[string]nativeui.Handle{
		"save": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
			fixture.writes.Add(1)
			fixture.mu.Lock()
			fixture.title = request.Values["title"]
			fixture.mu.Unlock()
			if fixture.save != nil {
				return fixture.save(ctx, request)
			}
			return nativeui.Result{Saved: &nativeui.SavedState{Route: "/editor", Values: request.Values}, ClearDraft: true, Navigation: &nativeui.Navigation{Kind: "pop"}, Haptic: "success"}, nil
		},
		"effect": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			fixture.effects.Add(1)
			return nativeui.Result{Haptic: "success"}, nil
		},
	}
	app, err := nativeui.New("/editor", nativeui.Route{Path: "/editor", Screen: screen, Actions: actions}, nativeui.Route{Path: "/created", Screen: screen, Actions: actions})
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = app
	return fixture
}

func reliabilityCommand(t *testing.T, runtime *nativeui.Runtime, command nativeui.Command) nativeui.Reply {
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

func reliabilityOpen(t *testing.T, app *nativeui.App, store behavior.DraftStore) (*nativeui.Runtime, nativeui.Command, nativeui.Reply) {
	t.Helper()
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{Drafts: store})
	if err != nil {
		t.Fatal(err)
	}
	hello := reliabilityCommand(t, runtime, nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{nativeui.CapabilityEditor: nativeui.SupportAvailable, nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityHaptics: nativeui.SupportAvailable}}})
	opened := reliabilityCommand(t, runtime, nativeui.Command{Kind: "open", Connection: hello.Connection, Route: "/editor"})
	if hello.Error != nil || opened.Error != nil || opened.Editor == nil {
		t.Fatalf("open: hello=%+v opened=%+v", hello, opened)
	}
	return runtime, nativeui.Command{Connection: hello.Connection, Instance: opened.Instance, Revision: 1, RequestID: "save", Action: "save", Values: map[string]string{"title": "submitted"}}, opened
}

func reliabilityDisk(t *testing.T) *reliabilityStore {
	t.Helper()
	store, err := behavior.NewFileDraftStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &reliabilityStore{DraftStore: store}
}

func TestRuntimeWireRestartBlocksInterruptedAndUnknownSaves(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown=%t", unknown), func(t *testing.T) {
			fixture, store := newReliabilityApp(t), reliabilityDisk(t)
			if unknown {
				fixture.save = func(context.Context, nativeui.Request) (nativeui.Result, error) {
					return nativeui.Result{}, errors.New("response lost after write")
				}
			}
			runtime, command, _ := reliabilityOpen(t, fixture.app, store)
			command.Kind = "action"
			action := reliabilityCommand(t, runtime, command)
			if unknown && (action.Error == nil || action.Error.Code != "unknown") {
				t.Fatalf("write did not report uncertainty: %+v", action)
			}
			command.Kind, command.Revision, command.Values["title"] = "checkpoint", 2, "newest typing"
			if reply := reliabilityCommand(t, runtime, command); reply.Error != nil || !reply.Editor.DraftStored {
				t.Fatalf("pending checkpoint: %+v", reply)
			}
			if unknown {
				command.Kind = "complete"
				reliabilityCommand(t, runtime, command)
			}
			restarted, recoveredCommand, recovered := reliabilityOpen(t, fixture.app, store)
			if !recovered.Editor.Restored || !recovered.Editor.Unknown || recovered.Editor.CanLeave || recovered.Editor.Values["title"] != "newest typing" || recovered.Editor.Baseline["title"] != "submitted" || recovered.Response != nil {
				t.Fatalf("restart lost uncertainty/current repository baseline: %+v", recovered)
			}
			for _, choice := range []string{"", "keep", "discard"} {
				recoveredCommand.Kind, recoveredCommand.Choice, recoveredCommand.Values["title"] = "leave", choice, "newest typing"
				if reply := reliabilityCommand(t, restarted, recoveredCommand); reply.Editor == nil || reply.Editor.CanLeave || !reply.Editor.Unknown {
					t.Fatalf("unknown save escaped through leave %q: %+v", choice, reply)
				}
			}
			recoveredCommand.Kind, recoveredCommand.RequestID = "action", "new-save"
			if reply := reliabilityCommand(t, restarted, recoveredCommand); reply.Error == nil || fixture.writes.Load() != 1 || fixture.effects.Load() != 0 {
				t.Fatalf("restart replayed a write/effect: %+v writes=%d", reply, fixture.writes.Load())
			}
			command.Kind = "complete"
			if reply := reliabilityCommand(t, restarted, command); reply.Error == nil || reply.Response != nil {
				t.Fatalf("old completion crossed process identity: %+v", reply)
			}
		})
	}
}

func TestRuntimeWireFailedCheckpointPreventsAction(t *testing.T) {
	for _, afterPut := range []bool{false, true} {
		for _, action := range []string{"save", "effect"} {
			t.Run(fmt.Sprintf("afterPut=%t/action=%s", afterPut, action), func(t *testing.T) {
				fixture, store := newReliabilityApp(t), reliabilityDisk(t)
				runtime, command, _ := reliabilityOpen(t, fixture.app, store)
				store.failPut, store.afterPut = true, afterPut
				command.Kind, command.Action = "action", action
				reply := reliabilityCommand(t, runtime, command)
				if reply.Error == nil || reply.Error.Code != "draft" || fixture.writes.Load() != 0 || fixture.effects.Load() != 0 || reply.Response != nil || reply.Editor == nil || reply.Editor.DraftStored || reply.Editor.Values["title"] != "submitted" {
					t.Fatalf("checkpoint failure dispatched action: %+v writes=%d effects=%d", reply, fixture.writes.Load(), fixture.effects.Load())
				}
				store.failPut = false
				if reply := reliabilityCommand(t, runtime, command); reply.Error != nil {
					t.Fatalf("known unexecuted action could not retry: %+v", reply)
				}
			})
		}
	}
}

func TestRuntimeWireFailedCheckpointAndDiscardBlockLeave(t *testing.T) {
	for _, choice := range []string{"keep", "discard"} {
		t.Run(choice, func(t *testing.T) {
			fixture, store := newReliabilityApp(t), reliabilityDisk(t)
			runtime, command, _ := reliabilityOpen(t, fixture.app, store)
			command.Kind = "checkpoint"
			if reply := reliabilityCommand(t, runtime, command); reply.Error != nil || !reply.Editor.DraftStored {
				t.Fatalf("initial checkpoint: %+v", reply)
			}
			store.failPut, store.failClear = choice == "keep", choice == "discard"
			command.Kind, command.Choice, command.Revision, command.Values["title"] = "leave", choice, 2, "newest typing"
			reply := reliabilityCommand(t, runtime, command)
			if reply.Error == nil || reply.Error.Code != "draft" || reply.Editor == nil || reply.Editor.CanLeave || !reply.Editor.Dirty || reply.Editor.Values["title"] != "newest typing" {
				t.Fatalf("failed leave lost current buffer or allowed leaving: %+v", reply)
			}
			store.failPut, store.failClear = false, false
			if reply := reliabilityCommand(t, runtime, command); reply.Error != nil || !reply.Editor.CanLeave {
				t.Fatalf("leave did not recover after storage repair: %+v", reply)
			}
		})
	}
}

func TestRuntimeWireFailedCompletionCleanupDoesNotRestoreCleanData(t *testing.T) {
	fixture, store := newReliabilityApp(t), reliabilityDisk(t)
	runtime, command, _ := reliabilityOpen(t, fixture.app, store)
	command.Kind = "action"
	if reply := reliabilityCommand(t, runtime, command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	store.failClear = true
	command.Kind = "complete"
	reply := reliabilityCommand(t, runtime, command)
	if reply.Error == nil || reply.Error.Code != "draft" || reply.Editor == nil || reply.Editor.CanLeave || reply.Editor.CanNavigate || reply.Response == nil || reply.Response.Navigation != nil || reply.Response.ClearDraft {
		t.Fatalf("failed cleanup authorized leaving: %+v", reply)
	}
	fixture.mu.Lock()
	fixture.title = "authoritative later edit"
	fixture.mu.Unlock()
	_, _, reopened := reliabilityOpen(t, fixture.app, store)
	if reopened.Editor.Restored || reopened.Editor.Unknown || reopened.Editor.Values["title"] != "authoritative later edit" {
		t.Fatalf("clean cleanup residue replaced fresh repository data: %+v", reopened)
	}
}

func TestRuntimeWireFailedPutAcknowledgementCanDiscardWithoutRevivingPendingSave(t *testing.T) {
	fixture, store := newReliabilityApp(t), reliabilityDisk(t)
	runtime, command, _ := reliabilityOpen(t, fixture.app, store)
	store.failPut, store.afterPut = true, true
	command.Kind = "action"
	if reply := reliabilityCommand(t, runtime, command); reply.Error == nil || fixture.writes.Load() != 0 {
		t.Fatalf("failed checkpoint launched mutation: %+v", reply)
	}
	store.failPut = false
	command.Kind, command.Choice = "leave", "discard"
	if reply := reliabilityCommand(t, runtime, command); reply.Error != nil || !reply.Editor.CanLeave {
		t.Fatalf("unexecuted checkpoint could not discard: %+v", reply)
	}
	_, _, recovered := reliabilityOpen(t, fixture.app, store)
	if recovered.Editor.Restored || recovered.Editor.Unknown || recovered.Editor.Values["title"] != "saved" {
		t.Fatalf("discard resurrected an unexecuted pending save: %+v", recovered)
	}
}

func TestRuntimeWireConditionalCleanupConflictBlocksDiscard(t *testing.T) {
	fixture, store := newReliabilityApp(t), reliabilityDisk(t)
	runtime, command, _ := reliabilityOpen(t, fixture.app, store)
	command.Kind = "checkpoint"
	if reply := reliabilityCommand(t, runtime, command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	store.conflictClear = true
	command.Kind, command.Choice = "leave", "discard"
	if reply := reliabilityCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "draft" || reply.Editor.CanLeave || reply.Editor.Values["title"] != "submitted" {
		t.Fatalf("conditional cleanup conflict was treated as success: %+v", reply)
	}
	stored, exists, err := store.Load(context.Background(), "draft")
	if err != nil || !exists || stored.Current["title"] != "submitted" {
		t.Fatalf("conflicting cleanup erased persisted text: %+v %t %v", stored, exists, err)
	}
}

func TestRuntimeWireFailedCompletionCheckpointBlocksEffectsAndRetry(t *testing.T) {
	fixture, store := newReliabilityApp(t), reliabilityDisk(t)
	fixture.save = func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Saved: &nativeui.SavedState{Route: "/editor", Values: request.Values}, Navigation: &nativeui.Navigation{Kind: "push", Route: "/created"}, Haptic: "success"}, nil
	}
	runtime, command, _ := reliabilityOpen(t, fixture.app, store)
	command.Kind = "action"
	if reply := reliabilityCommand(t, runtime, command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	store.failPut = true
	command.Kind = "complete"
	reply := reliabilityCommand(t, runtime, command)
	if reply.Error == nil || reply.Error.Code != "draft" || reply.Editor.CanLeave || reply.Editor.CanNavigate || reply.Response.Screen != nil || reply.Response.Navigation != nil || reply.Response.Haptic != "" || len(reply.EffectIDs) != 0 {
		t.Fatalf("failed completion checkpoint emitted effects: %+v", reply)
	}
	_, _, recovered := reliabilityOpen(t, fixture.app, store)
	if !recovered.Editor.Unknown || recovered.Editor.CanLeave {
		t.Fatalf("restart forgot interrupted completion: %+v", recovered)
	}
	store.failPut = false
	reply = reliabilityCommand(t, runtime, command)
	if reply.Error != nil || !reply.Editor.CanLeave || reply.Response.Navigation == nil || fixture.writes.Load() != 1 {
		t.Fatalf("retrying completion replayed mutation or failed cleanup: %+v writes=%d", reply, fixture.writes.Load())
	}
}

func TestRuntimeWireActiveSaveCannotSettleIntoNewDraftOwner(t *testing.T) {
	fixture, store := newReliabilityApp(t), reliabilityDisk(t)
	started, release := make(chan struct{}), make(chan struct{})
	fixture.save = func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		close(started)
		<-release
		return nativeui.Result{Saved: &nativeui.SavedState{Route: "/editor", Values: request.Values}, Navigation: &nativeui.Navigation{Kind: "pop"}, Haptic: "success"}, nil
	}
	runtime, command, _ := reliabilityOpen(t, fixture.app, store)
	command.Kind, command.Version = "action", nativeui.ProtocolVersion
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := runtime.CommandJSON(context.Background(), string(encoded)); done <- err }()
	<-started
	newOwner := reliabilityCommand(t, runtime, nativeui.Command{Kind: "open", Connection: command.Connection, Route: "/editor"})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if newOwner.Error != nil || !newOwner.Editor.Unknown || newOwner.Editor.CanLeave {
		t.Fatalf("active write was not durably restored as unknown: %+v", newOwner)
	}
	command.Kind = "complete"
	if reply := reliabilityCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "superseded" || reply.Response != nil {
		t.Fatalf("active save settled over newer owner: %+v", reply)
	}
	_, _, restarted := reliabilityOpen(t, fixture.app, store)
	if !restarted.Editor.Unknown || fixture.writes.Load() != 1 {
		t.Fatalf("active save lost durable uncertainty: %+v writes=%d", restarted, fixture.writes.Load())
	}
}

func TestRuntimeWireSupersededCompletionCannotClearNewerDraftOrEmitEffects(t *testing.T) {
	fixture, store := newReliabilityApp(t), reliabilityDisk(t)
	runtime, command, _ := reliabilityOpen(t, fixture.app, store)
	command.Kind = "action"
	if reply := reliabilityCommand(t, runtime, command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	newOwner := reliabilityCommand(t, runtime, nativeui.Command{Kind: "open", Connection: command.Connection, Route: "/editor"})
	newCheckpoint := nativeui.Command{Kind: "checkpoint", Connection: command.Connection, Instance: newOwner.Instance, Revision: 1, Values: map[string]string{"title": "new owner's text"}}
	if reply := reliabilityCommand(t, runtime, newCheckpoint); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	command.Kind = "complete"
	reply := reliabilityCommand(t, runtime, command)
	if reply.Error == nil || reply.Error.Code != "superseded" || reply.Response != nil || len(reply.EffectIDs) != 0 {
		t.Fatalf("superseded completion emitted stale effects: %+v", reply)
	}
	stored, exists, err := store.Load(context.Background(), "draft")
	if err != nil || !exists || stored.Current["title"] != "new owner's text" || stored.Outcome != behavior.SaveUnknown {
		t.Fatalf("stale completion changed new owner: %+v exists=%t err=%v", stored, exists, err)
	}
}

func TestRuntimeWireDraftKeyMigrationCheckpointsBeforeCleanup(t *testing.T) {
	for _, failure := range []string{"put", "clear"} {
		t.Run(failure, func(t *testing.T) {
			fixture, store := newReliabilityApp(t), reliabilityDisk(t)
			fixture.save = func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{Saved: &nativeui.SavedState{Route: "/created", DraftKey: "saved-draft", Values: request.Values}, Navigation: &nativeui.Navigation{Kind: "pop"}}, nil
			}
			runtime, command, _ := reliabilityOpen(t, fixture.app, store)
			command.Kind = "action"
			if reply := reliabilityCommand(t, runtime, command); reply.Error != nil {
				t.Fatal(reply.Error)
			}
			command.Kind, command.Revision, command.Values["title"] = "checkpoint", 2, "newer create text"
			if reply := reliabilityCommand(t, runtime, command); reply.Error != nil {
				t.Fatal(reply.Error)
			}
			store.failPut, store.failClear = failure == "put", failure == "clear"
			command.Kind = "complete"
			if reply := reliabilityCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "draft" || reply.Editor.CanLeave || reply.Editor.Values["title"] != "newer create text" || reply.Response.Navigation != nil {
				t.Fatalf("migration failure lost newer edits: %+v", reply)
			}
			old, exists, err := store.Load(context.Background(), "draft")
			if err != nil || !exists || !old.Pending || old.Current["title"] != "newer create text" {
				t.Fatalf("migration cleared recovery before replacement checkpoint: %+v %t %v", old, exists, err)
			}
			if failure == "clear" {
				saved, exists, err := store.Load(context.Background(), "saved-draft")
				if err != nil || !exists || saved.Pending || saved.Current["title"] != "newer create text" || saved.Baseline["title"] != "submitted" {
					t.Fatalf("migration did not checkpoint canonical draft first: %+v %t %v", saved, exists, err)
				}
			}
			store.failPut, store.failClear = false, false
			if reply := reliabilityCommand(t, runtime, command); reply.Error != nil || !reply.Editor.Dirty || reply.Response.Navigation != nil || fixture.writes.Load() != 1 {
				t.Fatalf("migration settlement did not recover without replay: %+v", reply)
			}
			if _, exists, err := store.Load(context.Background(), "draft"); err != nil || exists {
				t.Fatalf("old semantic key was not cleared: %t %v", exists, err)
			}
			restarted, restartedCommand, _ := reliabilityOpen(t, fixture.app, store)
			reopened := reliabilityCommand(t, restarted, nativeui.Command{Kind: "open", Connection: restartedCommand.Connection, Route: "/created"})
			if reopened.Error != nil || !reopened.Editor.Restored || reopened.Editor.Unknown || reopened.Editor.Values["title"] != "newer create text" || reopened.Editor.Baseline["title"] != "submitted" {
				t.Fatalf("canonical identity lost migrated newer text: %+v", reopened)
			}
		})
	}
}

func TestRuntimeWireNewCanonicalOwnerBlocksOldMigrationDiscard(t *testing.T) {
	fixture, store := newReliabilityApp(t), reliabilityDisk(t)
	fixture.save = func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Saved: &nativeui.SavedState{Route: "/created", DraftKey: "saved-draft", Values: request.Values}}, nil
	}
	runtime, command, _ := reliabilityOpen(t, fixture.app, store)
	command.Kind = "action"
	if reply := reliabilityCommand(t, runtime, command); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	store.failClear = true
	command.Kind = "complete"
	if reply := reliabilityCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "draft" {
		t.Fatalf("expected cleanup fault: %+v", reply)
	}
	newOwner := reliabilityCommand(t, runtime, nativeui.Command{Kind: "open", Connection: command.Connection, Route: "/created"})
	if newOwner.Error != nil {
		t.Fatal(newOwner.Error)
	}
	if reply := reliabilityCommand(t, runtime, nativeui.Command{Kind: "checkpoint", Connection: command.Connection, Instance: newOwner.Instance, Revision: 1, Values: map[string]string{"title": "new canonical owner"}}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	store.failClear = false
	command.Kind, command.Choice = "leave", "discard"
	if reply := reliabilityCommand(t, runtime, command); reply.Error == nil || reply.Error.Code != "superseded" {
		t.Fatalf("old migration owner could discard: %+v", reply)
	}
	if _, exists, err := store.Load(context.Background(), "draft"); err != nil || !exists {
		t.Fatalf("superseded discard cleared old uncertainty: %t %v", exists, err)
	}
	stored, exists, err := store.Load(context.Background(), "saved-draft")
	if err != nil || !exists || stored.Current["title"] != "new canonical owner" {
		t.Fatalf("superseded migration changed newer canonical draft: %+v %t %v", stored, exists, err)
	}
}
