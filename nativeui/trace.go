package nativeui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// Observer receives one TraceEvent after each Runtime command, once the reply
// is final and every runtime and session lock is released. A nil Observer
// (the default) disables tracing entirely.
//
// Observers run on the command's goroutine, so they must not block. They must
// not call this Runtime's Handle or CommandJSON. Calling Inspect is allowed.
// A panicking observer is recovered and cannot change the reply. The event
// is a copy the observer may keep.
type Observer func(TraceEvent)

// TraceEvent is a redacted record of one command. It holds registered route
// patterns, declared tokens, fixed codes, Go type names and salted hashes.
// It never holds titles, values, messages, concrete routes, queries, file
// names, bytes or raw connection, instance and request identities.
type TraceEvent struct {
	Seq      uint64        `json:"seq"`
	At       time.Time     `json:"at"`
	Duration time.Duration `json:"durationNs"`
	// Kind is the command kind, "other" for an unrecognized kind, or "invalid"
	// when CommandJSON could not decode the payload.
	Kind     string `json:"kind"`
	Renderer string `json:"renderer,omitempty"`
	// Route is the registered pattern of the screen, never the destination.
	Route string `json:"route,omitempty"`
	// Action is the action ID when the route registers it, else "undeclared".
	Action string `json:"action,omitempty"`
	// Correlation joins one request's action, respond, complete and ack events.
	// It is a salted 12-hex hash and is not linkable across Runtime instances.
	Correlation string `json:"correlation,omitempty"`
	Instance    string `json:"instance,omitempty"`
	// Outcome is "ok" or the reply's error code, "other" if the code is unknown.
	Outcome string `json:"outcome"`
	// FieldKeys lists reply field-error keys the screen declares. Others are
	// only counted in UndeclaredFields. Messages are never recorded.
	FieldKeys        []string `json:"fieldKeys,omitempty"`
	UndeclaredFields int      `json:"undeclaredFields,omitempty"`
	// Capability is the first declared requirement an open was refused for.
	Capability Capability `json:"capability,omitempty"`
	// Definition locates a rejected screen definition, without its message.
	Definition *TraceDefinition `json:"definition,omitempty"`
	// Cause lists up to four Go type names from the unexpected error's unwrap
	// chain, for example "*fs.PathError>syscall.Errno". Never err.Error().
	Cause      string        `json:"cause,omitempty"`
	Editor     *TraceEditor  `json:"editor,omitempty"`
	Foreground string        `json:"foreground,omitempty"`
	Receipt    *TraceReceipt `json:"receipt,omitempty"`
	Effects    []string      `json:"effects,omitempty"`
	Changed    bool          `json:"changed"`
}

// TraceDefinition is a DefinitionError's registered pattern and fixed part.
type TraceDefinition struct {
	Route string `json:"route"`
	Part  string `json:"part"`
}

// TraceEditor is the editor policy state after the command, without values.
type TraceEditor struct {
	Dirty       bool `json:"dirty"`
	Pending     bool `json:"pending"`
	Unknown     bool `json:"unknown"`
	Restored    bool `json:"restored"`
	DraftStored bool `json:"draftStored"`
	CanLeave    bool `json:"canLeave"`
	CanNavigate bool `json:"canNavigate"`
}

// TraceReceipt is an accepted effect receipt's capability and status.
type TraceReceipt struct {
	Capability Capability   `json:"capability"`
	Status     EffectStatus `json:"status"`
}

// Clone returns a deep copy, so a consumer can keep or change it safely.
func (event TraceEvent) Clone() TraceEvent {
	event.FieldKeys = append([]string(nil), event.FieldKeys...)
	event.Effects = append([]string(nil), event.Effects...)
	if len(event.FieldKeys) == 0 {
		event.FieldKeys = nil
	}
	if len(event.Effects) == 0 {
		event.Effects = nil
	}
	if event.Definition != nil {
		definition := *event.Definition
		event.Definition = &definition
	}
	if event.Editor != nil {
		editor := *event.Editor
		event.Editor = &editor
	}
	if event.Receipt != nil {
		receipt := *event.Receipt
		event.Receipt = &receipt
	}
	return event
}

var traceKinds = map[string]bool{"hello": true, "open": true, "close": true, "ack": true, "lock": true, "checkpoint": true, "leave": true, "action": true,
	"respond": true, "file-result": true, "service-result": true, "complete": true, "watch": true}

var traceCodes = map[string]bool{"protocol": true, "busy": true, "expired": true, "unsupported": true, "pending": true, "superseded": true, "stale": true,
	"invalid_request": true, "internal": true, "unknown": true, "conflict": true, "validation": true, "not_found": true, "forbidden": true, "draft": true,
	"cancelled": true, "unsaved_changes": true, "save_pending": true, "save_unknown": true, "stale_completion": true, "invalid_outcome": true}

type traceKey struct{}

// traceScratch collects facts while a traced command runs. It exists only
// when an Observer is configured.
type traceScratch struct {
	mu         sync.Mutex
	failure    error
	captured   bool
	route      string
	keys       map[string]bool
	action     string
	foreground string
	capability Capability
	renderer   string
}

func (scratch *traceScratch) fail(err error) {
	scratch.mu.Lock()
	scratch.failure = err
	scratch.mu.Unlock()
}

// fail records an unexpected cause for the observer, then applies the
// existing safeFailure redaction. With no observer it does not touch ctx.
func (runtime *Runtime) fail(ctx context.Context, err error) *ActionError {
	if runtime.options.Observer != nil {
		if scratch, ok := ctx.Value(traceKey{}).(*traceScratch); ok {
			scratch.fail(err)
		}
	}
	return safeFailure(err)
}

func traceFrom(runtime *Runtime, ctx context.Context) *traceScratch {
	if runtime.options.Observer == nil {
		return nil
	}
	scratch, _ := ctx.Value(traceKey{}).(*traceScratch)
	return scratch
}

// noteRefused records the first declared requirement the contract refuses.
func (runtime *Runtime) noteRefused(ctx context.Context, contract Contract, requirements []Requirement) {
	scratch := traceFrom(runtime, ctx)
	if scratch == nil {
		return
	}
	for _, requirement := range requirements {
		if contract.Check([]Requirement{requirement}) != nil {
			if runtime.app.declaredCapability(requirement.Capability) {
				scratch.mu.Lock()
				scratch.capability = requirement.Capability
				scratch.mu.Unlock()
			}
			return
		}
	}
}

// captureSession runs while the session lock is held, just before Handle
// releases it, and copies only structural facts.
func (runtime *Runtime) captureSession(scratch *traceScratch, session *runtimeSession, action string) {
	route, _, err := runtime.app.resolve(session.screen.Route, nil)
	scratch.mu.Lock()
	defer scratch.mu.Unlock()
	scratch.captured = true
	if err == nil {
		scratch.route = route.Path
		if action != "" {
			scratch.action = "undeclared"
			if route.Actions[action] != nil {
				scratch.action = action
			}
		}
	} else if action != "" {
		scratch.action = "undeclared"
	}
	scratch.keys = declaredKeys(session.screen)
	switch {
	case session.presentation != nil:
		scratch.foreground = "presentation"
	case session.files != nil:
		scratch.foreground = "files"
	case session.service != nil:
		scratch.foreground = "service"
	}
}

func declaredKeys(screen Screen) map[string]bool {
	keys := map[string]bool{"row": true, "action": true}
	if screen.Form != nil {
		for _, field := range screen.Form.Fields {
			keys[field.Key] = true
		}
	}
	if screen.List != nil {
		list := screen.List
		for _, field := range list.Filters {
			keys[field.Key] = true
		}
		if list.SearchKey != "" {
			keys[list.SearchKey] = true
		}
		if list.Selection != nil {
			keys[list.Selection.Key] = true
		}
		if list.Paging != nil {
			keys[list.Paging.Key] = true
		}
	}
	return keys
}

func (runtime *Runtime) traceHash(parts ...string) string {
	digest := sha256.New()
	digest.Write(runtime.traceSalt[:])
	for _, part := range parts {
		digest.Write([]byte(part))
		digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))[:12]
}

func (runtime *Runtime) instanceHash(instance string) string {
	if instance == "" {
		return ""
	}
	return runtime.traceHash("instance", instance)
}

// traced runs one command and reports it. Building and delivering the event
// are both recovered: a diagnostics failure never reaches the renderer.
func (runtime *Runtime) traced(ctx context.Context, observer Observer, command Command) Reply {
	scratch := &traceScratch{}
	started := time.Now()
	reply := runtime.handle(context.WithValue(ctx, traceKey{}, scratch), command)
	elapsed := time.Since(started)
	func() {
		defer func() { _ = recover() }()
		event := runtime.traceEvent(command, reply, scratch)
		event.At, event.Duration = started, elapsed
		observer(event)
	}()
	return reply
}

func (runtime *Runtime) emitInvalid(started time.Time) {
	observer := runtime.options.Observer
	if observer == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		observer(TraceEvent{Seq: runtime.traceSeq.Add(1), At: started, Duration: time.Since(started), Kind: "invalid", Outcome: "invalid_request"})
	}()
}

func (runtime *Runtime) traceEvent(command Command, reply Reply, scratch *traceScratch) TraceEvent {
	scratch.mu.Lock()
	defer scratch.mu.Unlock()
	event := TraceEvent{Seq: runtime.traceSeq.Add(1), Kind: "other", Outcome: "ok", Changed: reply.Changed}
	if traceKinds[command.Kind] {
		event.Kind = command.Kind
	}
	if command.Version != ProtocolVersion {
		event.Kind = "other"
	}
	switch {
	case reply.Contract != nil:
		event.Renderer = reply.Contract.Renderer
	case command.Kind == "hello" && command.Hello != nil && traceRenderer(command.Hello.Renderer):
		event.Renderer = command.Hello.Renderer
	default:
		event.Renderer = scratch.renderer
	}
	keys := scratch.keys
	if command.Kind == "open" {
		destination := command.Route
		if reply.Screen != nil {
			destination = reply.Screen.Route
			keys = declaredKeys(*reply.Screen)
		}
		if pattern, ok := runtime.app.RoutePattern(destination); ok {
			event.Route = pattern
		}
		event.Instance = runtime.instanceHash(reply.Instance)
		event.Capability = scratch.capability
	} else if scratch.captured {
		event.Route, event.Action, event.Foreground = scratch.route, scratch.action, scratch.foreground
		event.Instance = runtime.instanceHash(command.Instance)
	}
	if command.RequestID != "" && command.Kind != "hello" && command.Kind != "open" {
		event.Correlation = runtime.traceHash("request", command.Connection, command.Instance, command.RequestID)
	}
	if reply.Error != nil {
		event.Outcome = "other"
		if traceCodes[reply.Error.Code] {
			event.Outcome = reply.Error.Code
		}
		for key := range reply.Error.Fields {
			if keys[key] {
				event.FieldKeys = append(event.FieldKeys, key)
			} else {
				event.UndeclaredFields++
			}
		}
		sort.Strings(event.FieldKeys)
	}
	if scratch.failure != nil {
		event.Definition, event.Cause = runtime.traceCause(scratch.failure)
	}
	if editor := reply.Editor; editor != nil {
		event.Editor = &TraceEditor{Dirty: editor.Dirty, Pending: editor.Pending, Unknown: editor.Unknown, Restored: editor.Restored, DraftStored: editor.DraftStored, CanLeave: editor.CanLeave, CanNavigate: editor.CanNavigate}
	}
	if command.Kind == "ack" && reply.Error == nil && command.Receipt != nil && knownCapability(command.Receipt.Capability) {
		event.Receipt = &TraceReceipt{Capability: command.Receipt.Capability, Status: command.Receipt.Status}
	}
	event.Effects = traceEffects(reply)
	return event
}

func traceRenderer(renderer string) bool {
	return renderer == "uikit" || renderer == "android" || renderer == "webview"
}

// traceCause keeps only a definition's registered pattern and fixed part and
// the Go type names of the unexpected error. Expected ActionErrors have none.
func (runtime *Runtime) traceCause(err error) (*TraceDefinition, string) {
	var failure *ActionError
	if errors.As(err, &failure) {
		switch failure.Code {
		case "validation", "not_found", "forbidden", "conflict":
			return nil, ""
		}
	}
	var definition *DefinitionError
	if errors.As(err, &definition) {
		trusted := &TraceDefinition{Part: "screen"}
		if definitionParts[definition.Part] {
			trusted.Part = definition.Part
		}
		for _, route := range runtime.app.routes {
			if route.Path == definition.Route {
				trusted.Route = route.Path
			}
		}
		return trusted, errorTypes(definition.Err)
	}
	return nil, errorTypes(err)
}

func errorTypes(err error) string {
	names := make([]string, 0, 4)
	for err != nil && len(names) < 4 {
		names = append(names, reflect.TypeOf(err).String())
		switch wrapped := err.(type) {
		case interface{ Unwrap() []error }:
			errs := wrapped.Unwrap()
			if len(errs) == 0 {
				err = nil
			} else {
				err = errs[0]
			}
		default:
			err = errors.Unwrap(err)
		}
	}
	return strings.Join(names, ">")
}

func traceEffects(reply Reply) []string {
	var effects []string
	if response := reply.Response; response != nil {
		if response.Navigation != nil {
			switch response.Navigation.Kind {
			case "push", "sheet", "pop", "dismiss":
				effects = append(effects, "navigation:"+response.Navigation.Kind)
			default:
				effects = append(effects, "navigation")
			}
		}
		for name, present := range map[string]bool{"screen": response.Screen != nil, "share": response.Share != "", "haptic": response.Haptic != "",
			"announcement": response.Announcement != "", "saved": response.Saved != nil, "clear-draft": response.ClearDraft,
			"presentation": response.Presentation != nil, "files": response.Files != nil, "service": response.Service != nil} {
			if present {
				effects = append(effects, name)
			}
		}
	}
	if reply.Screen != nil && (reply.Response == nil || reply.Response.Screen == nil) {
		effects = append(effects, "screen")
	}
	sort.Strings(effects)
	return effects
}
