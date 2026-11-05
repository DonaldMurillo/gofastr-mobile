package nativeui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
)

type runtimeFiles struct {
	id         string
	definition FileRequest
	resolved   bool
	action     string
	values     map[string]string
}

func (runtime *Runtime) presentFiles(session *runtimeSession, record *runtimeAction, reply *Reply) {
	definition := reply.Response.Files
	if definition == nil || reply.Error != nil {
		return
	}
	if record.fileID != "" {
		if session.files != nil && session.files.id == record.fileID && !session.files.resolved {
			reply.FileID = record.fileID
		} else {
			reply.Response.Files = nil
		}
		return
	}
	capability := definition.capability()
	if definition.Extension != nil {
		declared, err := runtime.app.extensions.Validate(*definition.Extension)
		if err != nil {
			reply.Response.Files = nil
			reply.Error = commandFailure("validation", err.Error())
			return
		}
		capability = Capability(declared)
	}
	support := session.contract.Capabilities[capability]
	if support != SupportAvailable && support != SupportFallback {
		reply.Response.Files = nil
		message := "File access is unavailable in this renderer."
		if definition.Photo != nil {
			message = "Photo selection is unavailable in this renderer."
			if definition.capability() == CapabilityCamera {
				message = "Camera capture is unavailable in this renderer or device. Choose a photo instead."
			}
		}
		reply.Error = commandFailure("unsupported", message)
		return
	}
	runtime.presentationMu.Lock()
	defer runtime.presentationMu.Unlock()
	if runtime.presentationOwners[session.connection] != "" {
		reply.Response.Files = nil
		reply.Error = commandFailure("pending", "Another task is already presented in this app window.")
		return
	}
	identifier := runtimeID()
	session.files = &runtimeFiles{id: identifier, definition: *definition, action: record.sourceAction, values: maps.Clone(record.inputValues)}
	runtime.presentationOwners[session.connection] = identifier
	record.fileID, reply.FileID = identifier, identifier
}

func (runtime *Runtime) releaseFiles(session *runtimeSession) {
	if session.files == nil {
		return
	}
	runtime.presentationMu.Lock()
	if runtime.presentationOwners[session.connection] == session.files.id {
		delete(runtime.presentationOwners, session.connection)
	}
	runtime.presentationMu.Unlock()
	session.files = nil
}

func (runtime *Runtime) fileResult(ctx context.Context, session *runtimeSession, command Command, reply Reply) Reply {
	if command.RequestID == "" || len(command.RequestID) > 128 || command.FileResult == nil {
		reply.Error = commandFailure("invalid_request", "A file result identity is required.")
		return reply
	}
	outcome := *command.FileResult
	if outcome.File != nil {
		if err := outcome.File.Validate(); err != nil {
			reply.Error = commandFailure("validation", err.Error())
			return reply
		}
	}
	encoded, _ := json.Marshal(outcome)
	digest := sha256.Sum256(encoded)
	fingerprint := "file-result:" + command.FileID + ":" + hex.EncodeToString(digest[:])
	if previous := session.actions[command.RequestID]; previous != nil {
		if previous.action != fingerprint || !maps.Equal(previous.values, command.Values) {
			reply.Error = commandFailure("conflict", "A file result identity cannot be reused for different input.")
		} else if !previous.finished {
			reply.Error = commandFailure("pending", "This file result is already running.")
		} else {
			reply.Response, reply.Error = &previous.response, previous.failure
		}
		return reply
	}
	pending := session.files
	if pending == nil || pending.id != command.FileID || pending.resolved || session.active || session.uncertain || len(session.actions) >= 64 {
		reply.Error = commandFailure("invalid_request", "This file task is no longer available.")
		return reply
	}
	switch outcome.Status {
	case EffectCompleted:
		if pending.definition.acquires() {
			if outcome.File == nil {
				reply.Error = commandFailure("validation", "The picker did not return a file.")
				return reply
			}
			var err error
			if pending.definition.Extension != nil {
				err = runtime.app.extensions.Accept(*pending.definition.Extension, *outcome.File)
			} else if pending.definition.Photo != nil {
				err = pending.definition.Photo.Accept(*outcome.File)
			} else {
				err = pending.definition.Pick.Accept(*outcome.File)
			}
			if err != nil {
				reply.Error = commandFailure("validation", err.Error())
				return reply
			}
		} else if outcome.File != nil {
			reply.Error = commandFailure("invalid_request", "An export cannot upload a file.")
			return reply
		}
	case EffectCancelled, EffectDenied, EffectUnsupported, EffectFailed:
		if outcome.File != nil {
			reply.Error = commandFailure("invalid_request", "An unsuccessful file task cannot return content.")
			return reply
		}
	default:
		reply.Error = commandFailure("invalid_request", "Unknown file task outcome.")
		return reply
	}
	values := maps.Clone(pending.values)
	if outcome.Status == EffectCompleted && pending.definition.acquires() {
		var validationErr error
		values, validationErr = continuationValues(session, pending.action, pending.values, command.Values)
		if validationErr != nil {
			reply.Error, reply.Editor = safeFailure(validationErr), runtime.editorState(session)
			return reply
		}
	}
	record := &runtimeAction{action: fingerprint, values: maps.Clone(command.Values), sourceAction: pending.action, inputValues: maps.Clone(values), effectIDs: make(map[string]string), resolves: pending.id}
	session.actions[command.RequestID], session.active, pending.resolved = record, true, true
	definition, routeName := pending.definition, session.screen.Route
	screen := session.screen
	session.mu.Unlock()
	result := Response{Version: 1}
	var err error
	if outcome.Status == EffectCompleted && definition.acquires() {
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
			continuation := route.Files[definition.ID]
			if continuation == nil {
				err = errors.New("nativeui: file continuation is no longer registered")
			} else {
				var effects Result
				effects, err = continuation(ctx, request, outcome)
				if err == nil {
					result, err = runtime.app.result(ctx, route, effects)
				}
			}
		}
		if definition.Commit {
			runtime.mutations.Unlock()
		}
	} else {
		result.Announcement = map[EffectStatus]string{
			EffectCancelled: "File task cancelled", EffectDenied: "File access was denied", EffectUnsupported: "File access is unavailable", EffectFailed: "File access failed",
		}[outcome.Status]
	}
	session.mu.Lock()
	session.active, record.finished, record.response = false, true, result
	if err != nil {
		record.failure = safeFailure(err)
		if definition.Commit && record.failure.Code == "internal" {
			session.uncertain, record.unknown = true, true
			record.failure = commandFailure("unknown", "The file operation outcome is unknown. Inspect the data before trying again; it was not retried.")
		}
	}
	reply.Response, reply.Error, reply.Editor = &record.response, record.failure, runtime.editorState(session)
	return reply
}
