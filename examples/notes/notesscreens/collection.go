package notesscreens

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func tagOptions(emptyLabel string) []nativeui.Option {
	return []nativeui.Option{
		{Value: "", Label: emptyLabel}, {Value: "ideas", Label: "Ideas"},
		{Value: "work", Label: "Work"}, {Value: "personal", Label: "Personal"},
		{Value: "shared", Label: "Shared (not configured)", Disabled: true},
	}
}

func collectionRoute(service *notes.Service) nativeui.Route {
	return nativeui.Route{Path: Root, Screen: collectionScreen(service), Watch: func(ctx context.Context, request nativeui.Request, version string) (string, error) {
		if _, err := collectionQuery(request.Values); err != nil {
			return "", err
		}
		return service.Watch(ctx, version)
	}, Actions: map[string]nativeui.Handle{
		"new": navigate("push", "/notes/new"), "about": navigate("sheet", "/about"),
		"behaviors": navigate("push", "/behaviors"), "files": navigate("push", "/files"),
		"pin": collectionAction(service, "pin"), "archive": collectionAction(service, "archive"),
		"trash": collectionAction(service, "trash"), "restore": collectionAction(service, "restore"),
		"tag": collectionAction(service, "tag"),
		"undo": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
			result, err := service.Undo(ctx, request.Values["undo"])
			return mutationResult(result, err, "Undo applied")
		},
	}, Presentations: map[string]nativeui.Respond{
		"tag": collectionResponse(service, "tag"), "archive": collectionResponse(service, "archive"),
		"trash": collectionResponse(service, "trash"), "restore": collectionResponse(service, "restore"),
	}}
}

func collectionQuery(values map[string]string) (notes.Query, error) {
	query := notes.Query{Search: strings.TrimSpace(values["search"]), Tag: values["tag"], View: values["view"], Order: values["order"], Page: 1}
	if query.View == "" {
		query.View = "active"
	}
	if query.Order == "" {
		query.Order = "recent"
		if query.Search != "" {
			query.Order = "relevance"
		}
	}
	page := values["page"]
	if previous := values["page_scope"]; previous != "" && previous != queryScope(query, false) {
		page = "1"
	}
	if err := query.SetPage(page); err != nil {
		return notes.Query{}, presentationError(err)
	}
	return query, nil
}

func queryScope(query notes.Query, withPage bool) string {
	values := url.Values{"search": {query.Search}, "tag": {query.Tag}, "view": {query.View}, "order": {query.Order}}
	if withPage {
		values.Set("page", query.PageValue())
	}
	return values.Encode()
}

func collectionScreen(service *notes.Service) nativeui.Render {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
		query, err := collectionQuery(request.Values)
		if err != nil {
			return nativeui.Screen{}, err
		}
		pageScope := queryScope(query, false)
		if previous := request.Values["page_scope"]; previous != "" && previous != pageScope {
			query.Page = 1
			query.After = nil
			request.Values["page"] = "1"
			request.Values["selected"] = "[]"
		}
		scope := queryScope(query, true)
		version, err := service.Version(ctx)
		if err != nil {
			return nativeui.Screen{}, err
		}
		items, err := service.Collection(ctx, query)
		if err != nil {
			return nativeui.Screen{}, presentationError(err)
		}
		request.Values["page_scope"], request.Values["collection_scope"] = pageScope, scope
		request.Values["view"], request.Values["order"] = query.View, query.Order
		request.Values["page"] = query.PageValue()
		delete(request.Values, "row")
		count := fmt.Sprintf("%d notes", len(items.Notes))
		if len(items.Notes) == 1 {
			count = "1 note"
		}
		list := &nativeui.List{
			Header: "All notes", Footer: count + " shown · Stored on this device",
			Empty:     "No notes yet.\nTap New note to start writing.",
			SearchKey: "search", SearchPlaceholder: "Search your notes", Refreshable: true,
			Selection: &nativeui.Selection{Key: "selected", Selected: []string{}, Maximum: notes.MaximumSelection},
			Filters: []nativeui.Field{
				{Key: "view", Label: "View", Kind: "select", Value: query.View, Required: true, Options: []nativeui.Option{
					{Value: "active", Label: "Active"}, {Value: "archived", Label: "Archived"}, {Value: "trash", Label: "Trash"},
				}},
				{Key: "tag", Label: "Filter by tag", Kind: "select", Value: query.Tag, Options: tagOptions("All tags")},
				{Key: "order", Label: "Sort", Kind: "select", Value: query.Order, Options: []nativeui.Option{{Value: "relevance", Label: "Best match"}, {Value: "recent", Label: "Recently updated"}, {Value: "title", Label: "Title"}}},
			},
			Rows:   make([]nativeui.Row, 0, len(items.Notes)),
			Paging: &nativeui.Paging{Key: "page", Label: "Load more"},
		}
		if query.View == "archived" {
			list.Header, list.Empty = "Archived notes", "No archived notes."
		} else if query.View == "trash" {
			list.Header, list.Empty = "Trash", "Trash is empty. Files remain stored when a note is moved here."
		}
		if query.Search != "" {
			list.Header, list.Empty = "Search results", "No matching notes in this view.\nTry another word or change the filters."
			if query.Order == "relevance" {
				list.Footer += " · Titles ranked first within each 100-match group"
			}
		} else if query.Tag != "" {
			list.Header = "Tagged " + query.Tag
			list.Empty = "No notes with this tag in the current view.\nChange the tag or view to see other notes."
		}
		visible := make(map[string]bool, len(items.Notes))
		for _, item := range items.Notes {
			visible[item.ID] = true
			subtitle := notes.SearchSnippet(item.Body, query.Search)
			metadata := ""
			if subtitle == "" {
				subtitle = "Open note to read"
			}
			pinLabel := "Pin"
			if item.Pinned {
				pinLabel = "Unpin"
				metadata = "Pinned"
			}
			if item.Tag != "" {
				if metadata != "" {
					metadata += " · "
				}
				metadata += item.Tag
			}
			archiveLabel := "Archive"
			if item.Archived {
				archiveLabel = "Unarchive"
			}
			trailing := []nativeui.RowAction{{ID: "archive", Label: archiveLabel}, {ID: "trash", Label: "Move to Trash", Role: "destructive"}}
			if item.Trashed {
				trailing = []nativeui.RowAction{{ID: "restore", Label: "Restore"}}
			}
			list.Rows = append(list.Rows, nativeui.Row{ID: item.ID, Title: item.Title, Subtitle: subtitle, Metadata: metadata,
				TitleMatches: rowMatches(item.Title, query.Search), SubtitleMatches: rowMatches(subtitle, query.Search),
				Destination:    "/notes/" + url.PathEscape(item.ID),
				LeadingActions: []nativeui.RowAction{{ID: "pin", Label: pinLabel, FullSwipe: true}}, TrailingActions: trailing,
			})
		}
		var selected []string
		if json.Unmarshal([]byte(request.Values["selected"]), &selected) == nil && len(selected) <= notes.MaximumSelection {
			seen := make(map[string]bool)
			for _, id := range selected {
				if visible[id] && !seen[id] {
					list.Selection.Selected = append(list.Selection.Selected, id)
					seen[id] = true
				}
			}
		}
		encodedSelection, _ := json.Marshal(list.Selection.Selected)
		if len(list.Selection.Selected) > 0 {
			list.Header = fmt.Sprintf("%d selected · %s", len(list.Selection.Selected), list.Header)
		}
		request.Values["selected"] = string(encodedSelection)
		if items.Next != "" {
			list.Paging.Next = items.Next
			list.Footer += " · More available"
		}
		actions := []nativeui.Action{{ID: "new", Label: "New note", Icon: nativeui.IconCompose},
			{ID: "about", Label: "About Notes", Icon: nativeui.IconInfo, Leading: true},
			{ID: "behaviors", Label: "Behaviors"}, {ID: "files", Label: "Files"}, {ID: "tag", Label: "Tag selected", Commit: true}}
		if query.View == "trash" {
			actions = append(actions, nativeui.Action{ID: "restore", Label: "Restore selected", Commit: true})
		} else {
			archiveLabel := "Archive selected"
			if query.View == "archived" {
				archiveLabel = "Unarchive selected"
			}
			actions = append(actions, nativeui.Action{ID: "archive", Label: archiveLabel, Commit: true}, nativeui.Action{ID: "trash", Label: "Trash selected", Commit: true})
		}
		if token := service.LatestUndo(ctx); token != "" {
			request.Values["undo"] = token
			actions = append(actions, nativeui.Action{ID: "undo", Label: "Undo last change (30 seconds)", Commit: true})
		} else {
			delete(request.Values, "undo")
		}
		return nativeui.Screen{Title: "Notes", Prompt: "On this device", LargeTitle: true, List: list, Actions: actions, Live: &nativeui.Live{Version: version}}, nil
	}
}

func selectedIDs(ctx context.Context, service *notes.Service, request nativeui.Request) ([]string, error) {
	query, err := collectionQuery(request.Values)
	if err != nil {
		return nil, err
	}
	if scope := request.Values["collection_scope"]; scope != "" && scope != queryScope(query, true) {
		return nil, fileError("The notes query changed. Select notes again.")
	}
	var ids []string
	if row := request.Values["row"]; row != "" {
		ids = []string{row}
	} else if err := json.Unmarshal([]byte(request.Values["selected"]), &ids); err != nil {
		return nil, fileError("Select notes before choosing a collection action.")
	}
	if len(ids) == 0 || len(ids) > notes.MaximumSelection {
		return nil, fileError("Select between 1 and 20 notes.")
	}
	items, err := service.Collection(ctx, query)
	if err != nil {
		return nil, presentationError(err)
	}
	visible := make(map[string]bool, len(items.Notes))
	for _, item := range items.Notes {
		visible[item.ID] = true
	}
	seen := make(map[string]bool)
	for _, id := range ids {
		if !visible[id] || seen[id] {
			return nil, fileError("Select available notes in the current list, once each.")
		}
		seen[id] = true
	}
	return ids, nil
}

func collectionAction(service *notes.Service, action string) nativeui.Handle {
	return func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
		ids, err := selectedIDs(ctx, service, request)
		if err != nil {
			return nativeui.Result{}, err
		}
		if action == "pin" || (action == "archive" && request.Values["row"] != "") || action == "restore" {
			return applyCollection(ctx, service, request, ids, action, "")
		}
		presentation := nativeui.Presentation{ID: action, Kind: nativeui.PresentationConfirmation,
			Title: "Archive selected notes?", Message: fmt.Sprintf("Archive %d selected notes? You can undo this change for 30 seconds or unarchive them later.", len(ids)),
			Dismissible: true, Commit: true, Choices: []nativeui.PresentationChoice{{ID: "apply", Label: "Apply"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}},
		}
		if action == "trash" {
			presentation.Title = "Move notes to Trash?"
			presentation.Message = fmt.Sprintf("Move %d selected notes to Trash? Their attachments are kept. Undo is available for 30 seconds, and you can restore the notes from Trash later.", len(ids))
			presentation.Choices[0].Label, presentation.Choices[0].Role = "Move to Trash", "destructive"
		} else if action == "tag" {
			presentation.Kind, presentation.Title = nativeui.PresentationDialog, "Tag selected notes"
			presentation.Message = fmt.Sprintf("Choose one tag for %d selected notes. No tag removes the current tag. You can undo this change for 30 seconds.", len(ids))
			presentation.Selection = &nativeui.Selection{Min: 1, Max: 1, Selected: []string{}, Options: []nativeui.SelectionOption{
				{ID: "none", Label: "No tag"}, {ID: "ideas", Label: "Ideas"}, {ID: "work", Label: "Work"}, {ID: "personal", Label: "Personal"}, {ID: "shared", Label: "Shared (not configured)", Disabled: true},
			}}
		} else if request.Values["view"] == "archived" {
			presentation.Title = "Unarchive selected notes?"
			presentation.Message = fmt.Sprintf("Move %d selected notes back to Active?", len(ids))
		}
		return nativeui.Result{Presentation: &presentation}, nil
	}
}

func collectionResponse(service *notes.Service, action string) nativeui.Respond {
	return func(ctx context.Context, request nativeui.Request, result nativeui.PresentationResult) (nativeui.Result, error) {
		if result.Cancelled || result.Choice != "apply" {
			return nativeui.Result{}, nil
		}
		ids, err := selectedIDs(ctx, service, request)
		if err != nil {
			return nativeui.Result{}, err
		}
		tag := ""
		if action == "tag" {
			if len(result.Selected) != 1 {
				return nativeui.Result{}, fileError("Choose one available tag.")
			}
			tag = result.Selected[0]
			if tag == "none" {
				tag = ""
			}
		}
		return applyCollection(ctx, service, request, ids, action, tag)
	}
}

func applyCollection(ctx context.Context, service *notes.Service, request nativeui.Request, ids []string, action, tag string) (nativeui.Result, error) {
	if action == "archive" && request.Values["view"] == "archived" {
		action = "unarchive"
	}
	result, err := service.Change(ctx, ids, notes.Change{Kind: action, Tag: tag})
	return mutationResult(result, err, "Notes updated")
}

func mutationResult(result notes.Mutation, err error, message string) (nativeui.Result, error) {
	if err != nil && result.Count == 0 {
		return nativeui.Result{}, presentationError(err)
	}
	announcement := fmt.Sprintf("%s · %d changed", message, result.Count)
	if err != nil {
		announcement += " · Some notes could not be changed. Refresh and review before retrying."
	}
	if err != nil {
		return nativeui.Result{Announcement: announcement}, nil
	}
	return nativeui.Result{Announcement: announcement, Haptic: "success"}, nil
}

func rowMatches(text, term string) []nativeui.TextRange {
	var result []nativeui.TextRange
	for _, match := range notes.Matches(text, term) {
		result = append(result, nativeui.TextRange{Start: match.Start, End: match.End})
	}
	return result
}
