package nativeui

import (
	"context"
	"errors"
	"maps"
	"slices"

	"github.com/DonaldMurillo/gofastr-native/behavior"
)

type runtimePresentation struct {
	id         string
	definition Presentation
	behavior   *behavior.Presentation
	resolved   bool
	action     string
	values     map[string]string
}

func (runtime *Runtime) present(session *runtimeSession, record *runtimeAction, reply *Reply) {
	definition := reply.Response.Presentation
	if definition == nil || reply.Error != nil {
		return
	}
	if record.presentationID != "" {
		if session.presentation != nil && session.presentation.id == record.presentationID && !session.presentation.resolved {
			reply.PresentationID = record.presentationID
		} else {
			reply.Response.Presentation = nil
		}
		return
	}
	if session.contract.Capabilities[CapabilityPresentation] != SupportAvailable {
		reply.Response.Presentation = nil
		reply.Error = commandFailure("unsupported", "This renderer does not support this presentation.")
		return
	}
	runtime.presentationMu.Lock()
	defer runtime.presentationMu.Unlock()
	if runtime.presentationOwners[session.connection] != "" {
		reply.Response.Presentation = nil
		reply.Error = commandFailure("pending", "Another task is already presented in this app window.")
		return
	}
	identifier := runtimeID()
	policy, err := behavior.NewPresentation(identifier, presentationSpec(*definition))
	if err != nil {
		reply.Response.Presentation = nil
		reply.Error = commandFailure("invalid_request", "The presentation definition is invalid.")
		return
	}
	session.presentation = &runtimePresentation{id: identifier, definition: *definition, behavior: policy, action: record.sourceAction, values: maps.Clone(record.inputValues)}
	runtime.presentationOwners[session.connection] = identifier
	record.presentationID, reply.PresentationID = identifier, identifier
}

func (runtime *Runtime) releasePresentation(session *runtimeSession) {
	if session.presentation == nil {
		return
	}
	runtime.presentationMu.Lock()
	if runtime.presentationOwners[session.connection] == session.presentation.id {
		delete(runtime.presentationOwners, session.connection)
	}
	runtime.presentationMu.Unlock()
	session.presentation = nil
}

func (runtime *Runtime) respond(ctx context.Context, session *runtimeSession, command Command, reply Reply) Reply {
	if command.RequestID == "" || len(command.RequestID) > 128 || len(command.Selected) > 256 {
		reply.Error = commandFailure("invalid_request", "A presentation response identity is required.")
		return reply
	}
	fingerprint := "respond:" + command.PresentationID + ":" + command.Choice
	if previous := session.actions[command.RequestID]; previous != nil {
		if previous.action != fingerprint || !maps.Equal(previous.values, command.Values) || !slices.Equal(previous.selected, command.Selected) {
			reply.Error = commandFailure("conflict", "A response identity cannot be reused for different input.")
		} else if !previous.finished {
			reply.Error = commandFailure("pending", "This presentation response is already running.")
		} else {
			reply.Response, reply.Error = &previous.response, previous.failure
		}
		return reply
	}
	pending := session.presentation
	if pending == nil || pending.id != command.PresentationID || session.active || len(session.actions) >= 64 {
		reply.Error = commandFailure("invalid_request", "This presentation is no longer available.")
		return reply
	}
	cancelling := command.Choice == "" && pending.definition.Dismissible
	for _, choice := range pending.definition.Choices {
		if choice.ID == command.Choice && choice.Role == "cancel" && !choice.Disabled {
			cancelling = true
		}
	}
	values := maps.Clone(pending.values)
	if !cancelling {
		var err error
		values, err = continuationValues(session, pending.action, pending.values, command.Values)
		if err != nil {
			reply.Error, reply.Editor = safeFailure(err), runtime.editorState(session)
			return reply
		}
	}
	outcome, err := pending.behavior.Resolve(command.PresentationID, command.Choice, command.Selected)
	if err != nil {
		reply.Error = commandFailure("validation", err.Error())
		return reply
	}
	response := PresentationResult{Choice: outcome.Choice, Selected: outcome.Selected, Cancelled: outcome.Cancelled}
	record := &runtimeAction{action: fingerprint, values: maps.Clone(command.Values), sourceAction: pending.action, inputValues: maps.Clone(values), selected: slices.Clone(command.Selected), effectIDs: make(map[string]string), resolves: pending.id}
	session.actions[command.RequestID], session.active, pending.resolved = record, true, true
	definition, routeName := pending.definition, session.screen.Route
	screen := runtime.actionScreen(session)
	session.mu.Unlock()
	var result Response
	if outcome.Cancelled {
		result.Version = 1
	} else {
		if definition.Commit {
			runtime.mutations.Lock()
		}
		var route Route
		var request Request
		route, _, err = runtime.app.resolve(routeName, values)
		if err == nil {
			_, request, err = runtime.app.actionRequest(ctx, routeName, pending.action, values, &screen)
		}
		if err == nil {
			var effects Result
			continuation := route.Presentations[definition.ID]
			if continuation == nil {
				err = errors.New("nativeui: presentation continuation is no longer registered")
			} else {
				effects, err = continuation(ctx, request, response)
			}
			if err == nil {
				result, err = runtime.app.result(ctx, route, effects)
			}
		}
		if definition.Commit {
			runtime.mutations.Unlock()
		}
	}
	session.mu.Lock()
	session.active, record.finished, record.response = false, true, result
	if runtime.finishRevoked(session, &reply) {
		return reply
	}
	if err != nil {
		record.failure = safeFailure(err)
		if definition.Commit && record.failure.Code == "internal" {
			session.uncertain, record.unknown = true, true
			record.failure = commandFailure("unknown", "The operation outcome is unknown. Inspect the data before trying again; it was not retried.")
		}
	}
	reply.Response, reply.Error, reply.Editor = &record.response, record.failure, runtime.editorState(session)
	return reply
}
