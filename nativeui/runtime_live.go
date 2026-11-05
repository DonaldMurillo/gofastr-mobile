package nativeui

import (
	"context"
	"maps"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const WatchTimeout = 10 * time.Second

func validDataVersion(version string) bool {
	return version != "" && len(version) <= 256 && utf8.ValidString(version) && !strings.ContainsFunc(version, unicode.IsControl)
}

func (runtime *Runtime) cancelWatch(session *runtimeSession) {
	if session.watchCancel != nil {
		session.watchCancel()
		session.watchCancel = nil
	}
	session.watchGeneration++
}

func (runtime *Runtime) watch(ctx context.Context, session *runtimeSession, command Command, reply Reply) Reply {
	if ctx.Err() != nil {
		reply.Error = commandFailure("cancelled", "Live read cancelled.")
		return reply
	}
	if session.watchRetired {
		reply.Error = commandFailure("stale", "This live read belongs to an obsolete screen.")
		return reply
	}
	if session.screen.Live == nil || !validDataVersion(command.DataVersion) || session.active || session.service != nil || session.presentation != nil || session.files != nil || session.uncertain {
		reply.Error = commandFailure("invalid_request", "This live read is no longer available.")
		return reply
	}
	query := authorizedQuery(session.screen)
	for key, value := range command.Values {
		if expected, exists := query[key]; !exists || expected != value {
			reply.Error = commandFailure("stale", "Refresh the current query before watching it.")
			return reply
		}
	}
	route, request, err := runtime.app.resolve(session.screen.Route, query)
	if err != nil || route.Watch == nil {
		reply.Error = commandFailure("invalid_request", "This route does not support live reads.")
		return reply
	}
	runtime.cancelWatch(session)
	if session.watchWorker {
		reply.Error = commandFailure("pending", "The preceding live read is stopping.")
		return reply
	}
	select {
	case runtime.watchSlots <- struct{}{}:
	default:
		reply.Error = commandFailure("pending", "Live read capacity is occupied.")
		return reply
	}
	wait, cancel := context.WithTimeout(ctx, WatchTimeout)
	session.watchCancel = cancel
	session.watchWorker = true
	generation, revision := session.watchGeneration, session.revision
	screen := session.screen
	type observed struct {
		version string
		err     error
	}
	finished := make(chan observed, 1)
	session.mu.Unlock()
	go func() {
		outcome := observed{}
		defer func() {
			session.mu.Lock()
			<-runtime.watchSlots
			session.watchWorker = false
			session.mu.Unlock()
			finished <- outcome
		}()
		current, failure := runtime.app.Render(wait, screen.Route, query)
		if failure == nil && current.Live == nil {
			failure = unavailableAction("watch")
		}
		if failure != nil {
			outcome.err = failure
			return
		}
		version, failure := route.Watch(wait, request, command.DataVersion)
		if failure == nil {
			current, failure = runtime.app.Render(wait, screen.Route, query)
			if failure == nil && (current.Live == nil || !maps.Equal(authorizedQuery(current), query)) {
				failure = unavailableAction("watch")
			}
		}
		outcome = observed{version: version, err: failure}
	}()
	var outcome observed
	select {
	case outcome = <-finished:
	case <-wait.Done():
		outcome.err = wait.Err()
	}
	waitErr := wait.Err()
	cancel()
	session.mu.Lock()
	if session.closed || session.watchRetired || session.watchGeneration != generation || session.revision != revision || !maps.Equal(authorizedQuery(session.screen), query) || !runtime.ownsDraft(session) {
		reply.Error = commandFailure("stale", "This live read belongs to an obsolete screen.")
		return reply
	}
	session.watchCancel = nil
	if outcome.err != nil {
		if outcome.err == context.DeadlineExceeded && ctx.Err() == nil {
			reply.DataVersion = command.DataVersion
			return reply
		}
		if waitErr != nil {
			reply.Error = commandFailure("cancelled", "Live read cancelled.")
		} else {
			reply.Error = safeFailure(outcome.err)
		}
		return reply
	}
	if !validDataVersion(outcome.version) {
		reply.Error = commandFailure("internal", "The live source returned an invalid version.")
		return reply
	}
	reply.DataVersion, reply.Changed = outcome.version, outcome.version != command.DataVersion
	return reply
}
