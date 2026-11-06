// Package notesscreens authors native Notes screens entirely in Go.
package notesscreens

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notedevices"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesbackup"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extensions/media"
	"github.com/DonaldMurillo/gofastr-native/extensions/photos"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

// Root is the initial mobile Notes screen for either renderer.
const Root = "/notes"

// New composes mobile definitions over the same service as the web and terminal.
func New(service *notes.Service, stores ...*files.Store) (*nativeui.App, error) {
	if len(stores) > 1 {
		return nil, errors.New("notesscreens: at most one attachment store is supported")
	}
	var attachments *files.Store
	if len(stores) == 1 {
		attachments = stores[0]
	}
	return newApp(service, attachments, nil, nil)
}

// NewWithDevices adds installation-owned reviewed shares and protected-note
// metadata. Platform prompts and encrypted records stay in the device adapters.
func NewWithDevices(service *notes.Service, attachments *files.Store, devices *notedevices.Store) (*nativeui.App, error) {
	return newApp(service, attachments, devices, nil)
}

type Features struct {
	Devices *notedevices.Store
	Backups *notesbackup.Manager
}

func NewWithFeatures(service *notes.Service, attachments *files.Store, features Features) (*nativeui.App, error) {
	return newApp(service, attachments, features.Devices, features.Backups)
}

func newApp(service *notes.Service, attachments *files.Store, devices *notedevices.Store, backups *notesbackup.Manager) (*nativeui.App, error) {
	routes := []nativeui.Route{
		collectionRoute(service),
		nativeui.Route{Path: "/notes/new", Screen: editor(service, attachments), Actions: editorActions(service, attachments), Services: editorServices(service, attachments)},
		nativeui.Route{Path: "/notes/{id}", Screen: editor(service, attachments), Actions: editorActions(service, attachments), Files: map[string]nativeui.FileRespond{"attach": attachDocument(service, attachments)}, Services: editorServices(service, attachments)},
		aboutRoute("/about", "dismiss"),
		aboutRoute("/settings/about", "pop"),
	}
	routes = append(append(append(routes, fileRoutes(service, attachments)...), behaviorRoutes()...), settingsRoute())
	if devices != nil {
		routes = withDeviceFeatures(routes, service, attachments, devices)
	}
	if backups != nil {
		routes = withBackups(routes, service, backups)
	}
	return nativeui.NewWithExtensions(Root, []extension.Definition{photos.Definition(), media.Definition()}, tabShell(routes)...)
}

func editor(service *notes.Service, store *files.Store) nativeui.Render {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
		note := notes.Note{}
		if id := request.Params["id"]; id != "" {
			var err error
			note, err = service.Get(ctx, id)
			if err != nil {
				return nativeui.Screen{}, presentationError(err)
			}
		}
		title, status, key := "Note", "Saved on this device", note.ID
		if note.ID == "" {
			title, status, key = "New note", "Not saved yet · tap Done to save", "new"
		}
		actions := []nativeui.Action{{ID: "save", Label: "Done", Primary: true, Commit: true}, {ID: "share", Label: "Share note", Icon: nativeui.IconShare}, {ID: "copy", Label: "Copy note"}, {ID: "export", Label: "Export"}}
		if note.ID != "" {
			actions = append(actions, nativeui.Action{ID: "save-copy", Label: "Save text as new note", Commit: true})
			actions = append(actions, nativeui.Action{ID: "attach", Label: "Add file"}, nativeui.Action{ID: "attachments", Label: "Attachments"},
				nativeui.Action{ID: "record", Label: "Record audio"},
				nativeui.Action{ID: "reminder-schedule", Label: "Schedule reminder"}, nativeui.Action{ID: "reminder-cancel", Label: "Cancel reminder"}, nativeui.Action{ID: "reminder-status", Label: "Check reminder status"})
		}
		images, attachments, err := noteMedia(ctx, store, note.ID)
		if err != nil {
			return nativeui.Screen{}, err
		}
		components, err := mediaComponents(images, attachments, nil)
		if err != nil {
			return nativeui.Screen{}, err
		}
		return nativeui.Screen{Title: title, Actions: actions, Components: components,
			Form: &nativeui.Form{Style: "document", DetailsTitle: "Note details", Status: status, Fields: []nativeui.Field{
				{Key: "title", Label: "Title", Placeholder: "Title", Value: note.Title, Kind: "text", Style: "title", Required: true},
				{Key: "body", Label: "Note", Placeholder: "Start writing…", Value: note.Body, Kind: "multiline"},
				{Key: "pinned", Label: "Pinned", Value: strconv.FormatBool(note.Pinned), Kind: "toggle", Style: "detail"},
				{Key: "tag", Label: "Tag", Value: note.Tag, Kind: "select", Style: "detail", Options: tagOptions("No tag")},
				{Key: "reminder_date", Label: "Reminder date", Value: note.ReminderDate, Kind: "date", Style: "detail"},
				{Key: "reminder_time", Label: "Reminder time", Value: note.ReminderTime, Kind: "time", Style: "detail"},
			}, Draft: &nativeui.Draft{Key: "notes.native.draft." + key, Changed: "Unsaved changes · draft kept on this device", Restored: "Unsaved draft restored · tap Done to save", BackLabel: "Notes",
				Title: "Keep your draft?", Message: "Your changes haven’t been saved as a note yet.", KeepEditing: "Keep Editing", KeepDraft: "Keep Draft", Discard: "Discard Changes"}}}, nil
	}
}

func editorActions(service *notes.Service, store *files.Store) map[string]nativeui.Handle {
	return map[string]nativeui.Handle{
		"save":              saveEditor(service, false),
		"save-copy":         saveEditor(service, true),
		"share":             requestNoteShare(service, false),
		"copy":              requestNoteShare(service, true),
		"reminder-schedule": requestReminder(service, "schedule"),
		"reminder-cancel":   requestReminder(service, "cancel"),
		"reminder-status":   requestReminder(service, "status"),
		"record":            requestRecording(service, store),
		"export":            exportNote,
		"attach":            requestAttachment(service, store),
		"attachments": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
			if _, err := service.Get(ctx, request.Params["id"]); err != nil {
				return nativeui.Result{}, presentationError(err)
			}
			return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/notes/" + url.PathEscape(request.Params["id"]) + "/files"}}, nil
		},
	}
}

func saveEditor(service *notes.Service, copyText bool) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		details := notes.Details{}
		if request.Params["id"] != "" {
			existing, err := service.Get(ctx, request.Params["id"])
			if err != nil {
				return nativeui.Result{}, presentationError(err)
			}
			details = existing.Details
		}
		if value, exists := request.Values["pinned"]; exists {
			if value != "true" && value != "false" {
				return nativeui.Result{}, fileError("Choose true or false for Pinned.")
			}
			details.Pinned = value == "true"
		}
		for key, target := range map[string]*string{"tag": &details.Tag, "reminder_date": &details.ReminderDate, "reminder_time": &details.ReminderTime} {
			if value, exists := request.Values[key]; exists {
				*target = value
			}
		}
		id := request.Params["id"]
		if copyText {
			id = ""
			details.Archived = false
			details.Trashed = false
		}
		note, err := service.SaveDetailsIfUnchanged(ctx, id, request.Values["title"], request.Values["body"], details, request.Baseline)
		if err != nil {
			return nativeui.Result{}, presentationError(err)
		}
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, ClearDraft: true, Haptic: "success", Announcement: "Note saved",
			Saved: &nativeui.SavedState{Route: "/notes/" + url.PathEscape(note.ID), Values: map[string]string{
				"title": note.Title, "body": note.Body, "pinned": strconv.FormatBool(note.Pinned), "tag": note.Tag,
				"reminder_date": note.ReminderDate, "reminder_time": note.ReminderTime,
			}, DraftKey: "notes.native.draft." + note.ID}}, nil
	}
}

func navigate(kind, route string) nativeui.Handle {
	return func(context.Context, nativeui.Request) (nativeui.Result, error) {
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: kind, Route: route}}, nil
	}
}

func presentationError(err error) error {
	if errors.Is(err, notes.ErrChanged) {
		return &nativeui.ActionError{Code: "conflict", Message: "This note was saved in another editor. Your draft is still here. Export it or choose Save text as new note, then reopen the original. Attachments stay with the original note.", Err: err}
	}
	var validation *notes.ValidationError
	if errors.As(err, &validation) {
		return &nativeui.ActionError{Code: "validation", Message: validation.Message, Err: err}
	}
	if errors.Is(err, notes.ErrNotFound) {
		return &nativeui.ActionError{Code: "not_found", Message: "This note is no longer available.", Err: err}
	}
	if errors.Is(err, notes.ErrUndoUnavailable) {
		return &nativeui.ActionError{Code: "conflict", Message: "Undo is no longer available. Notes were not changed.", Err: err}
	}
	return err
}
