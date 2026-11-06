package notesscreens

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func tabShell(routes []nativeui.Route) []nativeui.Route {
	for index := range routes {
		render := routes[index].Screen
		path := routes[index].Path
		routes[index].Screen = func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
			screen, err := render(ctx, request)
			if err != nil {
				return nativeui.Screen{}, err
			}
			screen.Tabs = []nativeui.Tab{
				{ID: "notes", Label: "Notes", Route: Root, Icon: nativeui.IconNotes},
				{ID: "files", Label: "Files", Route: "/files", Icon: nativeui.IconFile},
				{ID: "settings", Label: "Settings", Route: "/settings", Icon: nativeui.IconSettings},
			}
			screen.ActiveTab = "notes"
			if path == "/files" || strings.HasPrefix(path, "/files/") {
				screen.ActiveTab = "files"
			} else if path == "/settings" || strings.HasPrefix(path, "/settings/") || path == "/about" || strings.HasPrefix(path, "/behaviors") {
				screen.ActiveTab = "settings"
			}
			return screen, nil
		}
	}
	return routes
}

func settingsRoute() nativeui.Route {
	return nativeui.Route{Path: "/settings", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Settings", LargeTitle: true, List: &nativeui.List{
			Header: "On this device", Footer: "Saved notes and attachments belong to this installation. Drafts are separate from saved notes. Opening Settings does not request device permissions.",
			Rows: []nativeui.Row{
				{ID: "behaviors", Title: "Behaviors", Subtitle: "Shared Go interactions", Destination: "/behaviors"},
				{ID: "about", Title: "About Notes", Subtitle: "One application, shared Go screens", Destination: "/settings/about"},
			},
		}}, nil
	}}
}

func aboutRoute(path, closeKind string) nativeui.Route {
	return nativeui.Route{Path: path, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "About Notes", Text: "Your notes stay on this device.\n\nSaved notes, attachments, and unsaved drafts belong to this installation. There is no cloud sync. A draft is separate from a saved note.\n\nChoose Schedule reminder on a saved note to request a notification. Saving reminder fields alone does not schedule one.\n\nUndo is available for 30 seconds and ends when Notes restarts. Moving a note to Trash keeps its attachments so you can restore them together.",
			Actions: []nativeui.Action{{ID: "close", Label: "Done", Primary: true}}}, nil
	}, Actions: map[string]nativeui.Handle{"close": navigate(closeKind, "")}}
}
