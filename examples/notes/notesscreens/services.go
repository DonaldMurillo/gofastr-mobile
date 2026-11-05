package notesscreens

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-mobile/device/audio"
	"github.com/DonaldMurillo/gofastr-mobile/device/files"
	"github.com/DonaldMurillo/gofastr-mobile/device/notifications"
	"github.com/DonaldMurillo/gofastr-mobile/device/sharing"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/nativeui"
)

const recordingMediaType = "application/vnd.gofastr.notes.audio+json"
const recordingMaxBytes = 512 << 10

type savedRecording struct {
	Version int            `json:"version"`
	Audio   audio.Document `json:"audio"`
}

func recordingRequest() audio.Request {
	return audio.Request{Title: "Record note audio", MaxSeconds: 60, MaxBytes: recordingMaxBytes}
}

func authorizeNote(ctx context.Context, service *notes.Service, request nativeui.Request) error {
	if request.Params["id"] == "" {
		return nil
	}
	_, err := service.Get(ctx, request.Params["id"])
	return presentationError(err)
}

func requestNoteShare(service *notes.Service, clipboard bool) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		if err := authorizeNote(ctx, service, request); err != nil {
			return nativeui.Result{}, err
		}
		text := request.Values["title"] + "\n\n" + request.Values["body"]
		operation := &nativeui.ServiceRequest{ID: "share"}
		if clipboard {
			operation.ID, operation.Clipboard = "copy", &sharing.Clipboard{Text: text}
		} else {
			document, err := noteDocument(request)
			if err != nil {
				return nativeui.Result{}, err
			}
			operation.Share = &sharing.Request{Title: "Share note", Text: text, Files: []files.Document{document}}
		}
		if err := operation.Validate(); err != nil {
			return nativeui.Result{}, fileError("This note is too large or invalid for sharing/copy. Text is limited to 64 KiB; use Export for larger notes.")
		}
		return nativeui.Result{Service: operation}, nil
	}
}

func requestReminder(service *notes.Service, operation string) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		note, err := service.Get(ctx, request.Params["id"])
		if err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		identifier, err := service.ReminderID(ctx, note.ID)
		if err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		reminder := &notifications.Request{Operation: operation, Identifier: identifier}
		if operation == notifications.Schedule {
			reminder.Title, reminder.Body = boundedServiceTitle(note.Title), "Open your saved note in Notes."
			reminder.Route = "/notes/" + url.PathEscape(note.ID)
			reminder.Date, reminder.Time = note.ReminderDate, note.ReminderTime
			if value, exists := request.Values["reminder_date"]; exists {
				reminder.Date = value
			}
			if value, exists := request.Values["reminder_time"]; exists {
				reminder.Time = value
			}
		}
		if err := reminder.Validate(); err != nil {
			return nativeui.Result{}, fileError("Choose a valid reminder date and time. Scheduling uses this device's local calendar; past or nonexistent times are rejected by the device.")
		}
		return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "reminder-" + operation, Notification: reminder}}, nil
	}
}

func boundedServiceTitle(title string) string {
	clean := []rune{}
	for _, character := range title {
		if !unicode.IsControl(character) {
			clean = append(clean, character)
		}
	}
	title = string(clean)
	for len(title) > 200 {
		_, size := utf8.DecodeLastRuneInString(title)
		title = title[:len(title)-size]
	}
	if title == "" {
		return "Notes reminder"
	}
	return title
}

func serviceOutcome(kind string, outcome nativeui.ServiceResult) nativeui.Result {
	message := "No device success was confirmed."
	switch outcome.Status {
	case nativeui.EffectCompleted:
		switch kind {
		case "share":
			message = "Share sheet finished. Recipient delivery is not confirmed."
		case "copy":
			message = "Device reports text copied."
		case "reminder-schedule":
			message = "Device reports reminder scheduled. Reminder fields are saved separately with Done."
		case "reminder-cancel":
			message = "Device reports reminder cancellation completed. Saved reminder fields were not changed."
		case "reminder-status":
			message = "Reminder status checked. Scheduling is not confirmed by this action."
		}
	case nativeui.EffectCancelled:
		message = "Device action cancelled. No success was confirmed."
	case nativeui.EffectDenied:
		message = "Device access denied. No success was confirmed."
	case nativeui.EffectUnsupported:
		message = "This device action is unsupported in this host. No success was confirmed."
	case nativeui.EffectFailed:
		message = "Device action failed. No success was confirmed."
	}
	if outcome.Message != "" {
		message += " Device report: " + outcome.Message
	}
	return nativeui.Result{Announcement: message}
}

func editorServices(service *notes.Service, store *files.Store) map[string]nativeui.ServiceRespond {
	responders := make(map[string]nativeui.ServiceRespond)
	for _, identifier := range []string{"share", "copy", "reminder-schedule", "reminder-cancel", "reminder-status"} {
		responders[identifier] = func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
			if err := authorizeNote(ctx, service, request); err != nil {
				return nativeui.Result{}, err
			}
			return serviceOutcome(identifier, outcome), nil
		}
	}
	responders["record"] = func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
		if _, err := service.Get(ctx, request.Params["id"]); err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		if outcome.Status != nativeui.EffectCompleted {
			return serviceOutcome("record", outcome), nil
		}
		if store == nil || outcome.File == nil {
			return nativeui.Result{}, fileError("Recording storage is unavailable.")
		}
		if err := recordingRequest().Accept(*outcome.File, outcome.DurationMillis); err != nil {
			return nativeui.Result{}, fileError("The recording is invalid or exceeds 60 seconds / 512 KiB.")
		}
		recording := savedRecording{Version: 1, Audio: audio.Document{File: *outcome.File, DurationMillis: outcome.DurationMillis}}
		encoded, err := json.Marshal(recording)
		if err != nil {
			return nativeui.Result{}, err
		}
		item, err := store.Put(ctx, request.Params["id"], files.Document{Name: outcome.File.Name, MediaType: recordingMediaType, Data: encoded})
		if err != nil {
			return nativeui.Result{}, fileError("Could not save the recording. Storage may be full.")
		}
		service.AttachmentsChanged(ctx)
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: attachmentRoute(request.Params["id"], item.Handle)}, Announcement: "Recording attached. Unsaved note text is still a draft."}, nil
	}
	return responders
}

func requestRecording(service *notes.Service, store *files.Store) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		if _, err := service.Get(ctx, request.Params["id"]); err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		if store == nil {
			return nativeui.Result{}, fileError("Recording storage is unavailable.")
		}
		recording := recordingRequest()
		return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "record", Audio: &recording, Commit: true}}, nil
	}
}

func playbackDocument(document files.Document) (files.Document, *audio.Document, error) {
	if document.MediaType != recordingMediaType {
		return document, nil, nil
	}
	var recording savedRecording
	if json.Unmarshal(document.Data, &recording) != nil || recording.Version != 1 || recordingRequest().Accept(recording.Audio.File, recording.Audio.DurationMillis) != nil {
		return files.Document{}, nil, fileError("The stored recording is invalid or unavailable.")
	}
	return recording.Audio.File, &recording.Audio, nil
}

func recordingMetadata(ctx context.Context, store *files.Store, noteID string, item files.File) (files.File, error) {
	if item.MediaType != recordingMediaType {
		return item, nil
	}
	document, err := store.Read(ctx, noteID, item.Handle)
	if err != nil {
		return files.File{}, err
	}
	document, _, err = playbackDocument(document)
	if err != nil {
		return files.File{}, err
	}
	item.Name, item.MediaType, item.Size = document.Name, document.MediaType, len(document.Data)
	return item, nil
}

func requestAttachmentShare(load func(context.Context, nativeui.Request) (files.Document, error)) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		document, err := load(ctx, request)
		if err != nil {
			return nativeui.Result{}, err
		}
		operation := &nativeui.ServiceRequest{ID: "share", Share: &sharing.Request{Title: "Share attachment", Files: []files.Document{document}}}
		if err := operation.Validate(); err != nil {
			return nativeui.Result{}, fileError(fmt.Sprintf("This attachment cannot be shared: %s", err))
		}
		return nativeui.Result{Service: operation}, nil
	}
}
