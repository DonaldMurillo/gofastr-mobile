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
			Header: "On this device", Footer: "Notes and attachments stay in this installation. Reminder fields are saved independently: use Schedule, Cancel or Check status explicitly on a saved note. Device support and permission vary; no prompt appears just by opening Notes. Audio recording is explicit, up to 60 seconds / 512 KiB. Undo lasts 30 seconds and does not survive a restart. Live changes are process-local, not cloud sync.",
			Rows: []nativeui.Row{
				{ID: "behaviors", Title: "Behaviors", Subtitle: "Shared Go interactions", Destination: "/behaviors"},
				{ID: "about", Title: "About Notes", Subtitle: "One application, shared Go screens", Destination: "/settings/about"},
			},
		}}, nil
	}}
}

func aboutRoute(path, closeKind string) nativeui.Route {
	return nativeui.Route{Path: path, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "About Notes", Text: "Your notes stay on this device.\n\nNotes, attachments, and unsaved drafts belong to this installation. A draft is separate from a saved note.\n\nReminder fields do not schedule a notification. Choose Schedule reminder on a saved note, then check the device outcome.\n\nThe same Go screens and actions run through native iOS, native Android, or the mobile WebView renderer.",
			Actions: []nativeui.Action{{ID: "close", Label: "Done", Primary: true}}}, nil
	}, Actions: map[string]nativeui.Handle{"close": navigate(closeKind, "")}}
}
