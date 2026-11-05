package notesscreens

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func behaviorRoutes() []nativeui.Route {
	cases := []struct {
		name         string
		title        string
		detail       string
		presentation nativeui.Presentation
	}{
		{"alert", "Alert", "Acknowledge a message. Closing it never runs a destructive action.", nativeui.Presentation{
			ID: "result", Kind: nativeui.PresentationAlert, Title: "Your notes stay on this device", Message: "This alert is defined in Go and displayed by the selected renderer.", Dismissible: true,
			Choices: []nativeui.PresentationChoice{{ID: "ok", Label: "Got it"}}}},
		{"confirmation", "Confirmation", "Cancel or explicitly confirm a destructive-looking action. This example does not delete your notes.", confirmation()},
		{"menu", "Action menu", "Choose an action, open a follow-up confirmation, or cancel. Unavailable choices cannot run.", nativeui.Presentation{
			ID: "result", Kind: nativeui.PresentationMenu, Title: "Note actions", Dismissible: true,
			Choices: []nativeui.PresentationChoice{{ID: "share", Label: "Share example"}, {ID: "confirm", Label: "Reset example", Role: "destructive"}, {ID: "unavailable", Label: "Sync · not configured", Disabled: true}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}}},
		{"dialog", "Dialog", "An explicit modal decision. Escape or a swipe cannot approve it.", nativeui.Presentation{
			ID: "result", Kind: nativeui.PresentationDialog, Title: "Ready to focus?", Message: "Choose Continue, or Cancel to leave things unchanged.",
			Choices: []nativeui.PresentationChoice{{ID: "continue", Label: "Continue"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}}},
		{"sheet", "Bottom sheet", "A dismissible task presented above this screen, not a new application or downloaded page.", nativeui.Presentation{
			ID: "result", Kind: nativeui.PresentationSheet, Title: "A little room for an idea", Message: "UIKit and WebView each own the sheet controls, focus, and animation. Go owns the result.", Dismissible: true,
			Choices: []nativeui.PresentationChoice{{ID: "done", Label: "Done"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}}},
		{"selection", "Single selection", "Choose the ordering for the Notes list. The same selection contract works in either renderer.", nativeui.Presentation{
			ID: "result", Kind: nativeui.PresentationDialog, Title: "Sort notes", Dismissible: true, Choices: selectionChoices(),
			Selection: &nativeui.Selection{Min: 1, Selected: []string{"recent"}, Options: []nativeui.SelectionOption{
				{ID: "recent", Label: "Recently updated", Detail: "Newest changes first"}, {ID: "title", Label: "Title", Detail: "Alphabetical order"}, {ID: "shared", Label: "Shared first", Detail: "Sharing is not configured", Disabled: true}}}}},
		{"multiple", "Multiple selection", "Preview one or more tags, including all available tags. Shared is disabled. Preview tags are not saved to notes.", nativeui.Presentation{
			ID: "result", Kind: nativeui.PresentationSheet, Title: "Preview tags", Message: "Choose one or more tags. You can apply all three available tags; Shared is disabled.", Dismissible: true, Choices: selectionChoices(),
			Selection: &nativeui.Selection{Multiple: true, Min: 1, Selected: []string{}, Options: []nativeui.SelectionOption{
				{ID: "ideas", Label: "Ideas"}, {ID: "work", Label: "Work"}, {ID: "personal", Label: "Personal"}, {ID: "shared", Label: "Shared", Disabled: true}}}}},
	}
	rows := make([]nativeui.Row, 0, len(cases))
	routes := make([]nativeui.Route, 0, len(cases)+1)
	for _, example := range cases {
		rows = append(rows, nativeui.Row{ID: example.name, Title: example.title, Subtitle: example.detail, Destination: "/behaviors/" + example.name})
		routes = append(routes, nativeui.Route{Path: "/behaviors/" + example.name, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: example.title, Text: example.detail, Requires: []nativeui.Requirement{{Capability: nativeui.CapabilityPresentation}},
				Actions: []nativeui.Action{{ID: "show", Label: "Show " + strings.ToLower(example.title), Primary: true}}}, nil
		}, Actions: map[string]nativeui.Handle{"show": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Presentation: &example.presentation}, nil
		}}, Presentations: map[string]nativeui.Respond{
			"result": func(_ context.Context, _ nativeui.Request, result nativeui.PresentationResult) (nativeui.Result, error) {
				if example.name == "menu" {
					switch result.Choice {
					case "share":
						return nativeui.Result{Share: "Notes · one Go application, UIKit or WebView"}, nil
					case "confirm":
						definition := confirmation()
						return nativeui.Result{Presentation: &definition}, nil
					}
				}
				if example.name == "selection" {
					return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/notes?order=" + result.Selected[0]}, Announcement: "Notes ordering applied"}, nil
				}
				message := example.title + " acknowledged"
				if example.name == "multiple" {
					message = "Selected tags: " + strings.Join(result.Selected, ", ")
				}
				return nativeui.Result{Announcement: message, Haptic: "success"}, nil
			},
			"reset": func(context.Context, nativeui.Request, nativeui.PresentationResult) (nativeui.Result, error) {
				return nativeui.Result{Announcement: "Example reset confirmed · your notes were not changed", Haptic: "success"}, nil
			},
		}})
	}
	return append([]nativeui.Route{{Path: "/behaviors", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Behaviors", Prompt: "Notes · one Go-only showcase", LargeTitle: true, List: &nativeui.List{
			Header: "Shared Go interactions", Footer: "The Notes editor also demonstrates drafts, validation, saving, sharing, and native feedback. Files supports import; saved notes support files, photo library, and camera attachments when available on this device.", Rows: rows}}, nil
	}}}, routes...)
}

func selectionChoices() []nativeui.PresentationChoice {
	return []nativeui.PresentationChoice{{ID: "apply", Label: "Apply"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}
}

func confirmation() nativeui.Presentation {
	return nativeui.Presentation{ID: "reset", Kind: nativeui.PresentationConfirmation, Title: "Reset this example?", Message: "This demonstrates explicit confirmation. Your saved notes and drafts are not changed.", Dismissible: true,
		Choices: []nativeui.PresentationChoice{{ID: "reset", Label: "Reset example", Role: "destructive"}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}}
}
