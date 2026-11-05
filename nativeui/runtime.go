package nativeui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-mobile/behavior"
)

// RuntimeOptions supplies installation-owned storage and deterministic time.
// Nil Drafts means memory-only editor sessions, not durable recovery.
type RuntimeOptions struct {
	Drafts      behavior.DraftStore
	Now         func() time.Time
	MaxSessions int
	IdleTimeout time.Duration
}

// Runtime coordinates local screen instances and commands without owning a
// navigation stack. Business operations run without the session or registry lock.
type Runtime struct {
	app                *App
	options            RuntimeOptions
	mu                 sync.Mutex
	mutations          sync.Mutex
	draftWrites        sync.Mutex
	draftOwners        map[string]string
	presentationMu     sync.Mutex
	presentationOwners map[string]string
	watchSlots         chan struct{}
	connections        map[string]runtimeConnection
	sessions           map[string]*runtimeSession
}

type runtimeConnection struct {
	contract Contract
	used     time.Time
}

type runtimeSession struct {
	mu              sync.Mutex
	connection      string
	contract        Contract
	screen          Screen
	editor          *behavior.Editor
	revision        uint64
	draftKey        string
	draft           *behavior.DraftRevision
	restored        bool
	draftStored     bool
	used            time.Time
	active          bool
	closed          bool
	actions         map[string]*runtimeAction
	effects         map[string]EffectReceipt
	presentation    *runtimePresentation
	files           *runtimeFiles
	service         *runtimeService
	watchCancel     context.CancelFunc
	watchGeneration uint64
	watchRetired    bool
	watchWorker     bool
	uncertain       bool
}

type runtimeAction struct {
	action         string
	values         map[string]string
	sourceAction   string
	inputValues    map[string]string
	submitted      *behavior.Submission
	response       Response
	failure        *ActionError
	finished       bool
	settled        bool
	unknown        bool
	effectIDs      map[string]string
	presentationID string
	resolves       string
	selected       []string
	fileID         string
	serviceID      string
}

// NewRuntime creates a bounded coordinator for one application's local renderers.
func NewRuntime(app *App, options RuntimeOptions) (*Runtime, error) {
	if app == nil {
		return nil, errors.New("nativeui: runtime screens are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxSessions == 0 {
		options.MaxSessions = 128
	}
	if options.IdleTimeout == 0 {
		options.IdleTimeout = 30 * time.Minute
	}
	if options.MaxSessions < 1 || options.MaxSessions > 4096 || options.IdleTimeout < time.Second {
		return nil, errors.New("nativeui: invalid runtime limits")
	}
	return &Runtime{app: app, options: options, connections: make(map[string]runtimeConnection), sessions: make(map[string]*runtimeSession), draftOwners: make(map[string]string), presentationOwners: make(map[string]string), watchSlots: make(chan struct{}, options.MaxSessions)}, nil
}

// CommandJSON is the bounded gomobile boundary for built-in screen services.
func (runtime *Runtime) CommandJSON(ctx context.Context, encoded string) (string, error) {
	if len(encoded) > 8<<20 {
		return "", errors.New("nativeui: command is too large")
	}
	var command Command
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&command); err != nil {
		return "", errors.New("nativeui: invalid command")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", errors.New("nativeui: expected one command")
	}
	reply := runtime.Handle(ctx, command)
	result, err := json.Marshal(reply)
	return string(result), err
}

// Handle executes one local command. Repeated action IDs never execute twice
// within a retained instance; no transport or mutation is automatically retried.
func (runtime *Runtime) Handle(ctx context.Context, command Command) Reply {
	reply := Reply{Version: ProtocolVersion, Connection: command.Connection, Instance: command.Instance, RequestID: command.RequestID, Revision: command.Revision}
	if command.Version != ProtocolVersion {
		reply.Error = commandFailure("protocol", "The app and renderer use incompatible mobile protocols.")
		return reply
	}
	if command.Kind == "hello" {
		if command.Hello == nil {
			reply.Error = commandFailure("protocol", "A renderer advertisement is required.")
			return reply
		}
		contract, err := Negotiate(*command.Hello)
		if err != nil {
			reply.Error = commandFailure("protocol", err.Error())
			return reply
		}
		if runtime.options.Drafts == nil {
			contract.Capabilities[CapabilityDrafts] = SupportUnavailable
		}
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		runtime.expire()
		if len(runtime.connections) >= runtime.options.MaxSessions {
			reply.Error = commandFailure("busy", "Too many active renderer connections. Close an unused app window.")
			return reply
		}
		reply.Connection, reply.Contract = runtimeID(), &contract
		runtime.connections[reply.Connection] = runtimeConnection{contract: contract, used: runtime.options.Now()}
		return reply
	}
	runtime.mu.Lock()
	connection, exists := runtime.connections[command.Connection]
	if exists {
		connection.used = runtime.options.Now()
		runtime.connections[command.Connection] = connection
	}
	runtime.mu.Unlock()
	if !exists {
		reply.Error = commandFailure("expired", "This renderer connection expired. Reopen the screen; actions were not retried.")
		return reply
	}
	if command.Kind == "open" {
		return runtime.open(ctx, command, connection.contract, reply)
	}
	runtime.mu.Lock()
	session := runtime.sessions[command.Instance]
	runtime.mu.Unlock()
	if session == nil {
		reply.Error = commandFailure("expired", "This screen instance expired. Reopen it; actions were not retried.")
		return reply
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.connection != command.Connection {
		reply.Error = commandFailure("expired", "This screen instance is no longer active.")
		return reply
	}
	session.used = runtime.options.Now()
	if command.Kind == "ack" {
		return runtime.ack(session, command, reply)
	}
	if session.closed {
		reply.Error = commandFailure("expired", "This screen instance is no longer active.")
		return reply
	}
	if command.Kind == "close" {
		if session.active {
			reply.Error = commandFailure("pending", "Wait for the pending action before closing this screen.")
		} else {
			session.closed = true
			runtime.cancelWatch(session)
			runtime.releasePresentation(session)
			runtime.releaseFiles(session)
			runtime.releaseService(session)
			runtime.releaseDraft(session)
		}
		return reply
	}
	if command.Kind != "complete" && !runtime.ownsDraft(session) {
		reply.Error = commandFailure("superseded", "This draft was opened in a newer screen. Reopen this editor before making further changes.")
		return reply
	}
	if command.Revision < session.revision {
		reply.Error = commandFailure("stale", "A newer editor checkpoint is already present.")
		return reply
	}
	if command.Kind == "watch" {
		return runtime.watch(ctx, session, command, reply)
	}
	runtime.cancelWatch(session)
	if _, _, err := runtime.app.resolve(session.screen.Route, command.Values); err != nil {
		reply.Error = commandFailure("invalid_request", "Invalid field values.")
		return reply
	}
	session.revision = command.Revision
	if session.editor != nil && command.Kind != "action" {
		values := currentEditorValues(session, command.Values)
		if failure := disabledFieldChanges(session.screen, values); failure != nil {
			reply.Error, reply.Editor = failure, runtime.editorState(session)
			return reply
		}
		session.editor.Checkpoint(values)
	}
	switch command.Kind {
	case "checkpoint", "leave":
		if command.Kind == "leave" && (session.presentation != nil || session.files != nil || session.service != nil || session.uncertain) {
			reply.Error = commandFailure("pending", "Finish the presented task before leaving this screen.")
			return reply
		}
		if command.Choice == "discard" && session.editor != nil {
			snapshot := session.editor.Snapshot()
			if !snapshot.Pending && snapshot.Outcome != behavior.SaveUnknown {
				session.editor.Checkpoint(snapshot.Baseline)
			}
		}
		runtime.persist(ctx, session)
		reply.Editor = runtime.editorState(session)
		if command.Kind == "leave" && command.Choice == "keep" && reply.Editor != nil && !reply.Editor.Pending && reply.Editor.DraftStored {
			reply.Editor.CanLeave = true
		}
	case "action":
		return runtime.action(ctx, session, command, reply)
	case "respond":
		return runtime.respond(ctx, session, command, reply)
	case "file-result":
		return runtime.fileResult(ctx, session, command, reply)
	case "service-result":
		return runtime.serviceResult(ctx, session, command, reply)
	case "complete":
		return runtime.complete(ctx, session, command, reply)
	default:
		reply.Error = commandFailure("invalid_request", "Unknown mobile command.")
	}
	return reply
}

func (runtime *Runtime) open(ctx context.Context, command Command, contract Contract, reply Reply) Reply {
	screen, err := runtime.app.Render(ctx, command.Route, command.Values)
	if err != nil {
		reply.Error = safeFailure(err)
		return reply
	}
	if err := contract.Check(screen.Requires); err != nil {
		reply.Error = commandFailure("unsupported", err.Error())
		return reply
	}
	reply.Instance = runtimeID()
	session := &runtimeSession{connection: command.Connection, contract: contract, screen: screen, used: runtime.options.Now(), actions: make(map[string]*runtimeAction), effects: make(map[string]EffectReceipt)}
	previousOwner := ""
	rollbackOwner := func() {
		runtime.draftWrites.Lock()
		defer runtime.draftWrites.Unlock()
		if runtime.draftOwners[session.draftKey] == reply.Instance {
			if previousOwner == "" {
				delete(runtime.draftOwners, session.draftKey)
			} else {
				runtime.draftOwners[session.draftKey] = previousOwner
			}
		}
	}
	if screen.Form != nil {
		baseline := make(behavior.Values)
		for _, field := range screen.Form.Fields {
			baseline[field.Key] = field.Value
		}
		session.editor, err = behavior.NewEditor(reply.Instance, screen.Route, baseline)
		if err != nil {
			reply.Error = commandFailure("internal", "The editor could not be initialized safely.")
			return reply
		}
		if screen.Form.Draft != nil {
			session.draftKey = screen.Form.Draft.Key
		}
		if runtime.options.Drafts != nil && session.draftKey != "" {
			runtime.draftWrites.Lock()
			draft, found, loadErr := runtime.options.Drafts.Load(ctx, session.draftKey)
			if loadErr == nil {
				previousOwner = runtime.draftOwners[session.draftKey]
				runtime.draftOwners[session.draftKey] = reply.Instance
			}
			runtime.draftWrites.Unlock()
			if loadErr != nil {
				reply.Error = commandFailure("draft", "Local draft recovery is unavailable. Your saved note has not changed.")
				return reply
			}
			if found {
				current := map[string]string(editorValues(screen, draft.Current))
				for _, field := range screen.Form.Fields {
					if field.Disabled {
						current[field.Key] = field.Value
					}
				}
				draft.Current = behavior.Values(current)
				if err := session.editor.Restore(draft); err != nil {
					rollbackOwner()
					reply.Error = commandFailure("draft", "The local draft could not be restored safely.")
					return reply
				}
				session.restored = true
				revision := draft.DraftRevision()
				session.draft, session.draftStored = &revision, true
			}
		}
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.expire()
	if len(runtime.sessions) >= runtime.options.MaxSessions {
		rollbackOwner()
		reply.Error = commandFailure("busy", "Too many active screens. Close an unused screen before continuing.")
		return reply
	}
	runtime.sessions[reply.Instance] = session
	for _, previous := range runtime.sessions {
		if previous == session {
			continue
		}
		previous.mu.Lock()
		if previous.connection == command.Connection || runtime.options.Drafts != nil && session.draftKey != "" && previous.draftKey == session.draftKey {
			runtime.cancelWatch(previous)
			previous.watchRetired = true
		}
		previous.mu.Unlock()
	}
	reply.Screen, reply.Editor = &screen, runtime.editorState(session)
	if screen.Live != nil {
		reply.DataVersion = screen.Live.Version
	}
	return reply
}

func (runtime *Runtime) action(ctx context.Context, session *runtimeSession, command Command, reply Reply) Reply {
	if session.presentation != nil || session.files != nil || session.service != nil || session.uncertain {
		reply.Error = commandFailure("pending", "Finish the presented task before starting another action.")
		return reply
	}
	if command.RequestID == "" || len(command.RequestID) > 128 {
		reply.Error = commandFailure("invalid_request", "An action request identity is required.")
		return reply
	}
	if previous := session.actions[command.RequestID]; previous != nil {
		if previous.action != command.Action || !maps.Equal(previous.values, command.Values) {
			reply.Error = commandFailure("conflict", "An action identity cannot be reused for different input.")
		} else if !previous.finished {
			reply.Error = commandFailure("pending", "This action is already running.")
		} else {
			response := previous.response
			response.Service = cloneService(response.Service)
			reply.Response, reply.Error = &response, previous.failure
		}
		reply.Editor = runtime.editorState(session)
		return reply
	}
	if session.active || len(session.actions) >= 64 {
		reply.Error = commandFailure("pending", "An action is pending, or this screen must be reopened before another action.")
		return reply
	}
	values := maps.Clone(command.Values)
	if session.editor != nil {
		values = map[string]string(currentEditorValues(session, command.Values))
		for key, value := range command.Values {
			values[key] = value
		}
	}
	inputValues := values
	values, definition, err := submittedComponents(session.screen, command.Action, values)
	if err != nil {
		if session.editor != nil {
			fields := editorValues(session.screen, inputValues)
			_, unavailable := availableAction(session.screen, command.Action, inputValues)
			if unavailable == nil && disabledFieldChanges(session.screen, fields) == nil {
				session.editor.Checkpoint(fields)
				runtime.persist(ctx, session)
			}
		}
		reply.Error, reply.Editor = safeFailure(err), runtime.editorState(session)
		if errors.Is(err, ErrUnknownAction) {
			reply.Error = commandFailure("invalid_request", "Choose an available action.")
		}
		return reply
	}
	record := &runtimeAction{action: command.Action, values: maps.Clone(command.Values), sourceAction: command.Action, inputValues: maps.Clone(values), effectIDs: make(map[string]string)}
	if session.editor != nil {
		session.editor.Checkpoint(editorValues(session.screen, values))
	}
	if definition.Commit && session.editor != nil {
		begin := session.editor.Begin(editorValues(session.screen, values))
		if !begin.Admitted {
			reply.Error = commandFailure(string(begin.Reason), "This save cannot be retried yet. Inspect the saved notes before trying again.")
			reply.Editor = runtime.editorState(session)
			return reply
		}
		record.submitted = begin.Submission
		runtime.persist(ctx, session)
	}
	session.actions[command.RequestID], session.active = record, true
	screen := session.screen
	session.mu.Unlock()
	if definition.Commit {
		runtime.mutations.Lock()
	}
	response, err := runtime.app.dispatch(ctx, screen.Route, command.Action, values, &screen)
	if definition.Commit {
		runtime.mutations.Unlock()
	}
	session.mu.Lock()
	session.active, record.finished, record.response = false, true, response
	if err != nil {
		record.failure = safeFailure(err)
		record.unknown = definition.Commit && record.failure.Code == "internal"
		if record.unknown {
			record.failure = commandFailure("unknown", "The save outcome is unknown. It may have succeeded. Your draft is retained; inspect the notes before saving again.")
			if session.editor == nil {
				session.uncertain = true
			}
		}
	}
	observed := record.response
	observed.Service = cloneService(observed.Service)
	reply.Response, reply.Error, reply.Editor = &observed, record.failure, runtime.editorState(session)
	return reply
}

func (runtime *Runtime) complete(ctx context.Context, session *runtimeSession, command Command, reply Reply) Reply {
	record := session.actions[command.RequestID]
	if record == nil || !record.finished {
		reply.Error = commandFailure("pending", "No completed action is available for this request.")
		return reply
	}
	if !record.settled && record.submitted != nil {
		outcome := behavior.SaveSucceeded
		if record.failure != nil {
			outcome = behavior.SaveFailed
		}
		if record.unknown {
			outcome = behavior.SaveUnknown
		}
		result := behavior.Result{InstanceID: record.submitted.InstanceID, Token: record.submitted.Token, Revision: record.submitted.Revision, Outcome: outcome}
		if record.failure != nil {
			result.Message, result.FieldErrors = record.failure.Message, behavior.FieldErrors(record.failure.Fields)
		}
		if record.response.Saved != nil {
			result.Values = maps.Clone(record.submitted.Values)
			maps.Copy(result.Values, behavior.Values(record.response.Saved.Values))
			result.SavedRoute = record.response.Saved.Route
		}
		session.editor.Complete(result, currentEditorValues(session, command.Values))
		if record.response.Saved != nil && record.failure == nil {
			saved := record.response.Saved
			form := *session.screen.Form
			form.Fields = append([]Field(nil), form.Fields...)
			for index := range form.Fields {
				form.Fields[index].Value = result.Values[form.Fields[index].Key]
			}
			session.screen.Form = &form
			if saved.DraftKey != "" && saved.DraftKey != session.draftKey {
				runtime.clearDraft(ctx, session)
				runtime.releaseDraft(session)
				session.draftKey, session.draft = saved.DraftKey, nil
				runtime.draftWrites.Lock()
				if runtime.draftOwners[saved.DraftKey] == "" {
					runtime.draftOwners[saved.DraftKey] = session.editor.Snapshot().InstanceID
				}
				runtime.draftWrites.Unlock()
			}
			session.screen.Route = saved.Route
		}
	}
	record.settled = true
	if record.resolves != "" && session.presentation != nil && session.presentation.id == record.resolves {
		runtime.releasePresentation(session)
	}
	if record.resolves != "" && session.files != nil && session.files.id == record.resolves {
		runtime.releaseFiles(session)
	}
	if record.resolves != "" && session.service != nil && session.service.id == record.resolves {
		runtime.releaseService(session)
	}
	runtime.persist(ctx, session)
	reply.Editor, reply.Error = runtime.editorState(session), record.failure
	response := record.response
	if reply.Editor != nil && !reply.Editor.CanNavigate {
		response.ClearDraft = false
		if response.Navigation != nil {
			forward := response.Navigation.Kind == "push" || response.Navigation.Kind == "sheet"
			preserved := record.submitted == nil && forward && !reply.Editor.Pending && !reply.Editor.Unknown && reply.Editor.DraftStored
			if !preserved {
				response.Navigation = nil
				if record.submitted == nil && reply.Error == nil {
					reply.Error = commandFailure("unsaved_changes", "Save your changes before opening another screen; the current draft could not be safely preserved.")
				}
			}
		}
	}
	if record.response.Navigation != nil && (record.response.Navigation.Kind == "push" || record.response.Navigation.Kind == "sheet") {
		response.Screen = nil
	}
	for name, present := range map[string]bool{"share": response.Share != "", "haptic": response.Haptic != "", "navigation": response.Navigation != nil, "announcement": response.Announcement != ""} {
		if present && record.effectIDs[name] == "" {
			record.effectIDs[name] = runtimeID()
		}
	}
	reply.Response, reply.EffectIDs = &response, maps.Clone(record.effectIDs)
	runtime.present(session, record, &reply)
	runtime.presentFiles(session, record, &reply)
	runtime.presentService(ctx, session, record, command, &reply)
	return reply
}

func (runtime *Runtime) editorState(session *runtimeSession) *EditorState {
	if session.editor == nil {
		return nil
	}
	snapshot := session.editor.Snapshot()
	leave := session.editor.Leave(snapshot.Current)
	return &EditorState{Baseline: snapshot.Baseline, Values: snapshot.Current, Revision: session.revision, Dirty: snapshot.Dirty, Pending: snapshot.Pending,
		Unknown: snapshot.Outcome == behavior.SaveUnknown || session.uncertain, Restored: session.restored, DraftStored: session.draftStored,
		CanLeave: leave.Disposition == behavior.LeaveAllow && !session.uncertain && session.service == nil, CanNavigate: !snapshot.Dirty && !snapshot.Pending && snapshot.Outcome != behavior.SaveUnknown && !session.uncertain,
		Fields: snapshot.FieldErrors, Message: snapshot.Message}
}

func (runtime *Runtime) persist(ctx context.Context, session *runtimeSession) {
	if session.editor == nil || runtime.options.Drafts == nil || session.draftKey == "" {
		return
	}
	runtime.draftWrites.Lock()
	defer runtime.draftWrites.Unlock()
	if runtime.draftOwners[session.draftKey] != session.editor.Snapshot().InstanceID {
		session.draftStored = false
		return
	}
	snapshot := session.editor.Snapshot()
	if !snapshot.Dirty && !snapshot.Pending && snapshot.Outcome != behavior.SaveUnknown {
		runtime.clearDraftLocked(ctx, session)
		return
	}
	if err := runtime.options.Drafts.Put(ctx, session.draftKey, snapshot); err != nil {
		session.draftStored = false
		return
	}
	revision := snapshot.DraftRevision()
	session.draft, session.draftStored = &revision, true
}

func (runtime *Runtime) clearDraft(ctx context.Context, session *runtimeSession) {
	runtime.draftWrites.Lock()
	defer runtime.draftWrites.Unlock()
	runtime.clearDraftLocked(ctx, session)
}

func (runtime *Runtime) clearDraftLocked(ctx context.Context, session *runtimeSession) {
	if runtime.options.Drafts != nil && session.editor != nil && runtime.draftOwners[session.draftKey] != session.editor.Snapshot().InstanceID {
		return
	}
	if runtime.options.Drafts != nil && session.draft != nil {
		_, err := runtime.options.Drafts.ClearIfRevision(ctx, session.draftKey, *session.draft)
		session.draftStored = err == nil
		if err == nil {
			session.draft = nil
		}
	}
}

func (runtime *Runtime) ownsDraft(session *runtimeSession) bool {
	if runtime.options.Drafts == nil || session.draftKey == "" || session.editor == nil {
		return true
	}
	runtime.draftWrites.Lock()
	defer runtime.draftWrites.Unlock()
	return runtime.draftOwners[session.draftKey] == session.editor.Snapshot().InstanceID
}

func (runtime *Runtime) releaseDraft(session *runtimeSession) {
	if session.editor == nil {
		return
	}
	runtime.draftWrites.Lock()
	defer runtime.draftWrites.Unlock()
	if runtime.draftOwners[session.draftKey] == session.editor.Snapshot().InstanceID {
		delete(runtime.draftOwners, session.draftKey)
	}
}

func (runtime *Runtime) ack(session *runtimeSession, command Command, reply Reply) Reply {
	receipt := command.Receipt
	record := session.actions[command.RequestID]
	valid := false
	if receipt != nil && record != nil && len(receipt.Message) <= 512 {
		for name, identifier := range record.effectIDs {
			capability := map[string]Capability{"share": CapabilityShare, "haptic": CapabilityHaptics, "navigation": CapabilityNavigation, "announcement": CapabilityAnnouncement}[name]
			valid = valid || (identifier == receipt.ID && capability == receipt.Capability)
		}
	}
	if !valid || !knownCapability(receipt.Capability) {
		reply.Error = commandFailure("invalid_request", "Unknown presentation effect receipt.")
		return reply
	}
	switch receipt.Status {
	case EffectCompleted, EffectCancelled, EffectUnsupported, EffectDenied, EffectFailed:
	default:
		reply.Error = commandFailure("invalid_request", "Unknown presentation effect outcome.")
		return reply
	}
	if previous, exists := session.effects[receipt.ID]; exists && previous != *receipt {
		reply.Error = commandFailure("conflict", "This effect already has an observed outcome.")
		return reply
	}
	session.effects[receipt.ID] = *receipt
	reply.Effects = []EffectReceipt{*receipt}
	return reply
}

func (runtime *Runtime) expire() {
	now := runtime.options.Now()
	for identifier, session := range runtime.sessions {
		session.mu.Lock()
		if !session.active && (session.closed || now.Sub(session.used) > runtime.options.IdleTimeout) {
			session.closed = true
			runtime.cancelWatch(session)
			runtime.releasePresentation(session)
			runtime.releaseFiles(session)
			runtime.releaseService(session)
			runtime.releaseDraft(session)
			delete(runtime.sessions, identifier)
		}
		session.mu.Unlock()
	}
	for identifier, connection := range runtime.connections {
		if now.Sub(connection.used) > runtime.options.IdleTimeout {
			delete(runtime.connections, identifier)
		}
	}
}

func editorValues(screen Screen, values map[string]string) behavior.Values {
	fields := make(behavior.Values)
	if screen.Form != nil {
		for _, field := range screen.Form.Fields {
			value, exists := values[field.Key]
			if !exists {
				value = field.Value
			}
			fields[field.Key] = value
		}
	}
	return fields
}

func currentEditorValues(session *runtimeSession, values map[string]string) behavior.Values {
	current := session.editor.Snapshot().Current
	if current == nil {
		current = make(behavior.Values)
	}
	maps.Copy(current, behavior.Values(values))
	return editorValues(session.screen, current)
}

func continuationValues(session *runtimeSession, action string, original, submitted map[string]string) (map[string]string, error) {
	values := maps.Clone(original)
	if values == nil {
		values = make(map[string]string)
	}
	if session.editor != nil {
		maps.Copy(values, map[string]string(currentEditorValues(session, submitted)))
	}
	maps.Copy(values, submitted)
	if values["row"] != original["row"] {
		return nil, invalidComponents(map[string]string{"row": "The presented row target cannot change."})
	}
	if session.screen.List != nil && session.screen.List.Selection != nil {
		selection := session.screen.List.Selection
		selected, err := behavior.DecodeSelection(values[selection.Key], listRows(*session.screen.List), selection.Maximum)
		if err != nil {
			return nil, invalidComponents(map[string]string{selection.Key: err.Error()})
		}
		encoded, _ := json.Marshal(selected)
		if string(encoded) != original[selection.Key] {
			return nil, invalidComponents(map[string]string{selection.Key: "The presented collection selection cannot change."})
		}
	}
	values, _, err := submittedComponents(session.screen, action, values)
	return values, err
}

func disabledFieldChanges(screen Screen, values map[string]string) *ActionError {
	fields := make(map[string]string)
	if screen.Form != nil {
		for _, field := range screen.Form.Fields {
			if field.Disabled && values[field.Key] != field.Value {
				fields[field.Key] = "This control is disabled."
			}
		}
	}
	if len(fields) != 0 {
		return invalidComponents(fields)
	}
	return nil
}

func commandFailure(code, message string) *ActionError {
	return &ActionError{Code: code, Message: message}
}

func safeFailure(err error) *ActionError {
	var failure *ActionError
	if errors.As(err, &failure) {
		switch failure.Code {
		case "validation", "not_found", "forbidden", "conflict":
			return &ActionError{Code: failure.Code, Message: failure.Message, Fields: maps.Clone(failure.Fields)}
		}
	}
	return commandFailure("internal", "Could not complete the action. Your text is still here.")
}

func runtimeID() string {
	var identifier [16]byte
	if _, err := rand.Read(identifier[:]); err != nil {
		panic("nativeui: secure instance identity unavailable")
	}
	return hex.EncodeToString(identifier[:])
}
