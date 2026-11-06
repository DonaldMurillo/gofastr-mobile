package nativeui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
)

type runtimeService struct {
	id         string
	definition ServiceRequest
	resolved   bool
	action     string
	values     map[string]string
}

func (runtime *Runtime) presentService(ctx context.Context, session *runtimeSession, record *runtimeAction, command Command, reply *Reply) {
	definition := record.response.Service
	if definition == nil {
		return
	}
	reply.Response.Service = nil
	if reply.Error != nil {
		return
	}
	if record.serviceID != "" {
		if session.service != nil && session.service.id == record.serviceID && !session.service.resolved {
			reply.ServiceID, reply.Response.Service = record.serviceID, cloneService(&session.service.definition)
		}
		return
	}
	if session.active || session.uncertain || len(session.actions) >= 64 || !runtime.ownsDraft(session) {
		reply.Error = commandFailure("pending", "This service source is no longer available.")
		return
	}
	values, err := continuationValues(session, record.sourceAction, record.inputValues, command.Values)
	if err != nil {
		reply.Error = safeFailure(err)
		return
	}
	if !maps.Equal(values, record.inputValues) {
		reply.Error = commandFailure("conflict", "Service inputs changed before admission. Choose the action again.")
		return
	}
	screen := runtime.actionScreen(session)
	session.active = true
	session.mu.Unlock()
	_, _, err = runtime.app.actionRequest(ctx, screen.Route, record.sourceAction, values, &screen)
	session.mu.Lock()
	session.active = false
	if err != nil {
		reply.Error = safeFailure(err)
		return
	}
	if command.Revision != session.revision || session.closed || !runtime.ownsDraft(session) {
		reply.Error = commandFailure("stale", "The service source changed before admission.")
		return
	}
	runtime.presentationMu.Lock()
	defer runtime.presentationMu.Unlock()
	if runtime.presentationOwners[session.connection] != "" {
		reply.Error = commandFailure("pending", "Another task is already presented in this app window.")
		return
	}
	identifier := runtimeID()
	session.service = &runtimeService{id: identifier, definition: *cloneService(definition), action: record.sourceAction, values: maps.Clone(values)}
	runtime.presentationOwners[session.connection] = identifier
	record.serviceID, reply.ServiceID, reply.Response.Service = identifier, identifier, cloneService(definition)
}

func (runtime *Runtime) releaseService(session *runtimeSession) {
	if session.service == nil {
		return
	}
	runtime.presentationMu.Lock()
	if runtime.presentationOwners[session.connection] == session.service.id {
		delete(runtime.presentationOwners, session.connection)
	}
	runtime.presentationMu.Unlock()
	session.service = nil
}

func (runtime *Runtime) serviceResult(ctx context.Context, session *runtimeSession, command Command, reply Reply) Reply {
	if command.RequestID == "" || len(command.RequestID) > 128 || command.ServiceID == "" || command.ServiceResult == nil {
		reply.Error = commandFailure("invalid_request", "A service result identity is required.")
		return reply
	}
	encoded, _ := json.Marshal(struct {
		ServiceID string
		Revision  uint64
		Result    *ServiceResult
	}{command.ServiceID, command.Revision, command.ServiceResult})
	digest := sha256.Sum256(encoded)
	fingerprint := "service-result:" + hex.EncodeToString(digest[:])
	if previous := session.actions[command.RequestID]; previous != nil {
		if previous.action != fingerprint || !maps.Equal(previous.values, command.Values) {
			reply.Error = commandFailure("conflict", "A service result identity cannot be reused for different input.")
		} else if !previous.finished {
			reply.Error = commandFailure("pending", "This service result is already running.")
		} else {
			response := previous.response
			response.Service = cloneService(response.Service)
			reply.Response, reply.Error = &response, previous.failure
		}
		reply.Editor = runtime.editorState(session)
		return reply
	}
	pending := session.service
	if pending == nil || pending.id != command.ServiceID || pending.resolved || session.active || session.uncertain || len(session.actions) >= 64 {
		reply.Error = commandFailure("invalid_request", "This service task is no longer available.")
		return reply
	}
	var outcome ServiceResult
	encoded, _ = json.Marshal(command.ServiceResult)
	_ = json.Unmarshal(encoded, &outcome)
	if err := pending.definition.accept(outcome); err != nil {
		reply.Error = commandFailure("validation", err.Error())
		return reply
	}
	values, err := continuationValues(session, pending.action, pending.values, command.Values)
	if err != nil {
		reply.Error, reply.Editor = safeFailure(err), runtime.editorState(session)
		return reply
	}
	if !sameServiceTarget(session.screen, pending.values, values) {
		reply.Error = commandFailure("conflict", "The admitted service target cannot change.")
		return reply
	}
	record := &runtimeAction{action: fingerprint, values: maps.Clone(command.Values), sourceAction: pending.action, inputValues: maps.Clone(values), effectIDs: make(map[string]string), resolves: pending.id}
	session.actions[command.RequestID], session.active, pending.resolved = record, true, true
	definition, screen := pending.definition, session.screen
	session.mu.Unlock()
	if definition.Commit {
		runtime.mutations.Lock()
	}
	route, _, err := runtime.app.resolve(screen.Route, values)
	var request Request
	if err == nil {
		_, request, err = runtime.app.actionRequest(ctx, screen.Route, pending.action, values, &screen)
	}
	response := Response{Version: 1}
	if err == nil {
		continuation := route.Services[definition.ID]
		if continuation == nil {
			err = errors.New("nativeui: service continuation is no longer registered")
		} else {
			var result Result
			request.Service, request.ServiceValues = cloneService(&definition), maps.Clone(pending.values)
			result, err = continuation(ctx, request, outcome)
			if err == nil && outcome.Status != EffectCompleted && (result.Saved != nil || result.ClearDraft) {
				err = errors.New("nativeui: an unsuccessful service cannot commit editor state")
			}
			if err == nil && result.Saved != nil && !definition.Commit {
				err = errors.New("nativeui: a read-only service cannot commit an editor")
			}
			if err == nil {
				response, err = runtime.app.result(ctx, route, result)
			}
		}
	}
	if definition.Commit {
		runtime.mutations.Unlock()
	}
	session.mu.Lock()
	session.active, record.finished, record.response = false, true, response
	if err == nil && response.Saved != nil && session.editor != nil {
		begin := session.editor.Begin(editorValues(screen, pending.values))
		if !begin.Admitted {
			err = errors.New("nativeui: saved service editor could not settle")
		} else {
			record.submitted = begin.Submission
			session.editor.Checkpoint(editorValues(screen, values))
		}
	}
	if err != nil {
		record.failure = safeFailure(err)
		if definition.Commit && record.failure.Code == "internal" {
			session.uncertain, record.unknown = true, true
			record.failure = commandFailure("unknown", "The service mutation outcome is unknown. Inspect the data before trying again; it was not retried.")
		}
	}
	if runtime.finishRevoked(session, &reply) {
		return reply
	}
	result := record.response
	result.Service = cloneService(result.Service)
	reply.Response, reply.Error, reply.Editor = &result, record.failure, runtime.editorState(session)
	return reply
}

func sameServiceTarget(screen Screen, original, current map[string]string) bool {
	original, current = maps.Clone(original), maps.Clone(current)
	if screen.Form != nil {
		for _, field := range screen.Form.Fields {
			delete(original, field.Key)
			delete(current, field.Key)
		}
	}
	return maps.Equal(original, current)
}
