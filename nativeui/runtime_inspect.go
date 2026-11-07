package nativeui

import (
	"maps"
	"sort"

	"github.com/DonaldMurillo/gofastr-native/behavior"
)

// RuntimeSnapshot is a redacted, point-in-time view of a Runtime for
// diagnostics. It contains no values, titles, concrete routes or raw IDs.
type RuntimeSnapshot struct {
	Connections []ConnectionSnapshot `json:"connections"`
	Sessions    []SessionSnapshot    `json:"sessions"`
}

// ConnectionSnapshot is one renderer connection's negotiated contract.
type ConnectionSnapshot struct {
	Renderer     string                 `json:"renderer"`
	Version      int                    `json:"version"`
	Capabilities map[Capability]Support `json:"capabilities"`
}

// SessionSnapshot describes one retained screen instance. When Busy is true
// another goroutine held the session lock and only Instance is filled.
type SessionSnapshot struct {
	Instance       string        `json:"instance"`
	Route          string        `json:"route,omitempty"`
	Renderer       string        `json:"renderer,omitempty"`
	Busy           bool          `json:"busy"`
	Active         bool          `json:"active"`
	Closed         bool          `json:"closed"`
	Uncertain      bool          `json:"uncertain"`
	Protected      bool          `json:"protected"`
	Live           bool          `json:"live"`
	PrivacyRevoked bool          `json:"privacyRevoked"`
	Foreground     string        `json:"foreground,omitempty"`
	Editor         *TraceEditor  `json:"editor,omitempty"`
	Actions        ActionSummary `json:"actions"`
}

// ActionSummary counts retained action records by settlement state.
type ActionSummary struct {
	Total      int `json:"total"`
	Unfinished int `json:"unfinished"`
	Unsettled  int `json:"unsettled"`
	Unknown    int `json:"unknown"`
}

// Inspect returns a snapshot without blocking on busy sessions and without
// changing runtime state: it does not expire sessions, touch idle clocks,
// call the configured clock or run application code. It never holds the
// runtime lock and a session lock at the same time, so an Observer may call
// it. Instance matches TraceEvent.Instance for the same Runtime.
func (runtime *Runtime) Inspect() RuntimeSnapshot {
	type entry struct {
		id      string
		session *runtimeSession
	}
	runtime.mu.Lock()
	snapshot := RuntimeSnapshot{Connections: make([]ConnectionSnapshot, 0, len(runtime.connections)), Sessions: make([]SessionSnapshot, 0, len(runtime.sessions))}
	for _, connection := range runtime.connections {
		snapshot.Connections = append(snapshot.Connections, ConnectionSnapshot{Renderer: connection.contract.Renderer, Version: connection.contract.Version, Capabilities: maps.Clone(connection.contract.Capabilities)})
	}
	entries := make([]entry, 0, len(runtime.sessions))
	for id, session := range runtime.sessions {
		entries = append(entries, entry{id, session})
	}
	runtime.mu.Unlock()
	for _, item := range entries {
		state := SessionSnapshot{Instance: runtime.instanceHash(item.id)}
		if !item.session.mu.TryLock() {
			state.Busy = true
			snapshot.Sessions = append(snapshot.Sessions, state)
			continue
		}
		runtime.describeSession(item.session, &state)
		item.session.mu.Unlock()
		snapshot.Sessions = append(snapshot.Sessions, state)
	}
	sort.Slice(snapshot.Connections, func(i, j int) bool { return snapshot.Connections[i].Renderer < snapshot.Connections[j].Renderer })
	sort.Slice(snapshot.Sessions, func(i, j int) bool { return snapshot.Sessions[i].Instance < snapshot.Sessions[j].Instance })
	return snapshot
}

// describeSession runs under the session lock and only reads.
func (runtime *Runtime) describeSession(session *runtimeSession, state *SessionSnapshot) {
	if pattern, ok := runtime.app.RoutePattern(session.screen.Route); ok {
		state.Route = pattern
	}
	state.Renderer = session.contract.Renderer
	state.Active, state.Closed, state.Uncertain, state.PrivacyRevoked = session.active, session.closed, session.uncertain, session.privacyRevoked
	state.Protected, state.Live = session.screen.Protection != nil, session.screen.Live != nil
	switch {
	case session.presentation != nil:
		state.Foreground = "presentation"
	case session.files != nil:
		state.Foreground = "files"
	case session.service != nil:
		state.Foreground = "service"
	}
	if session.editor != nil {
		// Snapshot only: Editor.Leave records the current buffer, which would
		// be a mutation.
		snapshot := session.editor.Snapshot()
		unknown := snapshot.Outcome == behavior.SaveUnknown
		state.Editor = &TraceEditor{Dirty: snapshot.Dirty, Pending: snapshot.Pending, Unknown: unknown || session.uncertain, Restored: session.restored, DraftStored: session.draftStored,
			CanLeave:    !snapshot.Pending && !unknown && !snapshot.Dirty && !session.uncertain && session.service == nil && !session.draftFailed,
			CanNavigate: !snapshot.Dirty && !snapshot.Pending && !unknown && !session.uncertain && !session.draftFailed}
	}
	for _, record := range session.actions {
		state.Actions.Total++
		if !record.finished {
			state.Actions.Unfinished++
		} else if !record.settled {
			state.Actions.Unsettled++
		}
		if record.unknown {
			state.Actions.Unknown++
		}
	}
}
