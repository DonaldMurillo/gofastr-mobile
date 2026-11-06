package notesscreens

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/sharein"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notedevices"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func inboxRoutes(service *notes.Service, attachments *files.Store, devices *notedevices.Store) []nativeui.Route {
	load := func(ctx context.Context, id string) (sharein.Item, error) {
		state, err := devices.Snapshot(ctx)
		if err != nil {
			return sharein.Item{}, err
		}
		item, found := state.Shares[id]
		if !found {
			return item, &nativeui.ActionError{Code: "not_found", Message: "This shared item is no longer in the review inbox."}
		}
		return item, nil
	}
	acknowledge := func(id, operation string) nativeui.Result {
		return nativeui.Result{Service: &nativeui.ServiceRequest{ID: operation,
			IncomingShare: &sharein.Request{Operation: sharein.Acknowledge, Identifier: id}}}
	}
	acknowledged := func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
		if outcome.Status != nativeui.EffectCompleted {
			return nativeui.Result{Announcement: "The shared item remains in the inbox. If you saved it, check Notes before trying again."}, nil
		}
		id := request.Service.IncomingShare.Identifier
		if err := devices.Update(ctx, func(state *notedevices.State) error { delete(state.Shares, id); return nil }); err != nil {
			return nativeui.Result{}, err
		}
		result := nativeui.Result{Announcement: "Shared item removed from the inbox."}
		if request.Service.ID == "accepted" {
			result.Navigation = &nativeui.Navigation{Kind: "pop"}
		}
		return result, nil
	}
	return []nativeui.Route{
		{Path: "/inbox", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
			state, err := devices.Snapshot(ctx)
			if err != nil {
				return nativeui.Screen{}, err
			}
			list := &nativeui.List{Header: "Ready to review", Empty: "No shared items to review.\nShare text or files to Notes, then tap Check inbox.", Footer: "Items stay in the inbox until you save or discard them."}
			ids := make([]string, 0, len(state.Shares))
			for id := range state.Shares {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				item := state.Shares[id]
				list.Rows = append(list.Rows, nativeui.Row{ID: id, Title: incomingTitle(item), Subtitle: fmt.Sprintf("%d attachments", len(item.Files)),
					Destination: "/inbox/" + id, TrailingActions: []nativeui.RowAction{{ID: "discard", Label: "Discard", Role: "destructive"}}})
			}
			return nativeui.Screen{Title: "Shared items", LargeTitle: true, List: list, Actions: []nativeui.Action{{ID: "read", Label: "Check inbox", Primary: true}}}, nil
		}, Actions: map[string]nativeui.Handle{
			"read": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "read", IncomingShare: &sharein.Request{Operation: sharein.Next}}}, nil
			},
			"discard": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				if _, err := load(ctx, request.Values["row"]); err != nil {
					return nativeui.Result{}, err
				}
				return nativeui.Result{Presentation: &nativeui.Presentation{ID: "discard", Kind: nativeui.PresentationConfirmation, Title: "Discard shared item?", Message: "This removes the inbox copy. The original stays in the sending app.", Dismissible: true, Choices: []nativeui.PresentationChoice{
					{ID: "cancel", Label: "Cancel", Role: "cancel"}, {ID: "discard", Label: "Discard", Role: "destructive"},
				}}}, nil
			},
		}, Presentations: map[string]nativeui.Respond{"discard": func(ctx context.Context, request nativeui.Request, choice nativeui.PresentationResult) (nativeui.Result, error) {
			if choice.Cancelled || choice.Choice != "discard" {
				return nativeui.Result{}, nil
			}
			if _, err := load(ctx, request.Values["row"]); err != nil {
				return nativeui.Result{}, err
			}
			return acknowledge(request.Values["row"], "discard"), nil
		}}, Services: map[string]nativeui.ServiceRespond{
			"discard": acknowledged,
			"read": func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
				if outcome.Status != nativeui.EffectCompleted {
					return featureOutcome("The inbox could not be checked.", outcome), nil
				}
				if outcome.IncomingShare == nil {
					return nativeui.Result{Announcement: "No new shared items."}, nil
				}
				if err := devices.Stage(ctx, *outcome.IncomingShare); err != nil {
					return nativeui.Result{}, err
				}
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/inbox/" + outcome.IncomingShare.Identifier}}, nil
			},
		}},
		{Path: "/inbox/{share}", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
			item, err := load(ctx, request.Params["share"])
			if err != nil {
				return nativeui.Screen{}, err
			}
			var cards []nativeui.Attachment
			for i, file := range item.Files {
				cards = append(cards, nativeui.Attachment{ID: strconv.Itoa(i), Name: file.Name, MediaType: file.MediaType, Size: len(file.Data), Destination: "/inbox/" + item.Identifier + "/files/" + strconv.Itoa(i)})
			}
			components, err := mediaComponents(nil, cards, nil)
			if err != nil {
				return nativeui.Screen{}, err
			}
			return nativeui.Screen{Title: "Review shared item", Components: components, Actions: []nativeui.Action{{ID: "save", Label: "Save note", Primary: true, Commit: true}},
				Form: &nativeui.Form{Style: "document", Status: "Review the text and attachments before saving.", Fields: []nativeui.Field{
					{Key: "title", Label: "Title", Value: incomingTitle(item), Kind: "text", Style: "title", Required: true},
					{Key: "body", Label: "Note", Value: item.Text, Kind: "multiline", Placeholder: "Add a note about these files"},
				}, Draft: &nativeui.Draft{Key: "notes.inbox.draft." + item.Identifier, Changed: "Review changes kept on this device", Restored: "Review draft restored", BackLabel: "Shared items", Title: "Keep your review changes?", Message: "The original shared item remains in the inbox.", KeepEditing: "Keep Editing", KeepDraft: "Keep Draft", Discard: "Discard Changes"}}}, nil
		}, Actions: map[string]nativeui.Handle{"save": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
			item, err := load(ctx, request.Params["share"])
			if err != nil {
				return nativeui.Result{}, err
			}
			if attachments == nil && len(item.Files) > 0 {
				return nativeui.Result{}, fileError("Attachment storage is unavailable. The shared item remains in the inbox.")
			}
			note, complete, err := service.ImportShared(ctx, item.Identifier, request.Values["title"], request.Values["body"])
			if err != nil {
				return nativeui.Result{}, importFailure(err)
			}
			if !complete {
				for i, file := range item.Files {
					if _, err := attachments.PutOnce(ctx, note.ID, item.Identifier+"."+strconv.Itoa(i), file); err != nil {
						return nativeui.Result{}, importFailure(err)
					}
				}
				if err := service.FinishImport(ctx, note.ID); err != nil {
					return nativeui.Result{}, importFailure(err)
				}
				service.AttachmentsChanged(ctx)
			}
			result := acknowledge(item.Identifier, "accepted")
			result.Saved = &nativeui.SavedState{Route: request.Route, Values: map[string]string{"title": note.Title, "body": note.Body}}
			result.ClearDraft, result.Announcement = true, "Note saved. Finishing inbox cleanup."
			return result, nil
		}}, Services: map[string]nativeui.ServiceRespond{"accepted": acknowledged}},
		{Path: "/inbox/{share}/files/{index}", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
			item, err := load(ctx, request.Params["share"])
			if err != nil {
				return nativeui.Screen{}, err
			}
			index, err := strconv.Atoi(request.Params["index"])
			if err != nil || index < 0 || index >= len(item.Files) {
				return nativeui.Screen{}, fileError("This shared file is unavailable.")
			}
			file := item.Files[index]
			preview := &nativeui.FilePreview{File: nativeui.Attachment{ID: strconv.Itoa(index), Name: file.Name, MediaType: file.MediaType, Size: len(file.Data), Destination: request.Route}, Caption: "Shared file awaiting your review"}
			if strings.HasPrefix(file.MediaType, "text/") && utf8.Valid(file.Data) && !strings.ContainsRune(string(file.Data), '\x00') {
				preview.PreviewTitle, preview.Text = "Text preview", boundedText(string(file.Data), 16384)
			}
			var images []nativeui.Image
			if image, err := nativeui.NewImage(strconv.Itoa(index), file.Name, file.Data, 1024); err == nil {
				images = append(images, image)
			}
			components, err := mediaComponents(images, nil, preview)
			return nativeui.Screen{Title: "Shared attachment", Components: components}, err
		}},
	}
}

func importFailure(err error) error {
	var validation *notes.ValidationError
	if errors.As(err, &validation) {
		return presentationError(err)
	}
	return &nativeui.ActionError{Code: "conflict", Message: "The import is incomplete. Check the saved note and available storage, then choose Save note again to finish this same import. The shared item remains in the inbox.", Err: err}
}

func incomingTitle(item sharein.Item) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.SplitN(strings.TrimSpace(item.Text), "\n", 2)[0]
	}
	if title == "" && len(item.Files) > 0 {
		title = item.Files[0].Name
	}
	if title == "" {
		title = "Shared note"
	}
	runes := []rune(title)
	if len(runes) > 200 {
		title = string(runes[:200])
	}
	return title
}

func boundedText(text string, maximum int) string {
	if len(text) <= maximum {
		return text
	}
	text = text[:maximum]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text
}

func featureOutcome(message string, outcome nativeui.ServiceResult) nativeui.Result {
	return nativeui.Result{Announcement: message + " " + map[nativeui.EffectStatus]string{
		nativeui.EffectCancelled: "The device action was cancelled.", nativeui.EffectDenied: "Device access was denied.",
		nativeui.EffectUnsupported: "This host does not support the feature.", nativeui.EffectFailed: "The device action failed.",
	}[outcome.Status]}
}
