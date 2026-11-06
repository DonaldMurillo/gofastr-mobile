package notesscreens

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/widgets"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notedevices"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func withDeviceFeatures(routes []nativeui.Route, service *notes.Service, attachments *files.Store, devices *notedevices.Store) []nativeui.Route {
	for index := range routes {
		route := &routes[index]
		switch route.Path {
		case Root:
			base := route.Screen
			route.Screen = func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
				screen, err := base(ctx, request)
				screen.Actions = append(screen.Actions, nativeui.Action{ID: "inbox", Label: "Shared items"}, nativeui.Action{ID: "protected", Label: "Protected notes"})
				return screen, err
			}
			route.Actions["inbox"], route.Actions["protected"] = navigate("push", "/inbox"), navigate("push", "/protected")
			route.Services = map[string]nativeui.ServiceRespond{}
			for _, action := range []string{"archive", "trash"} {
				baseAction := route.Actions[action]
				directID, confirmedID := "widget-before-"+action, "widget-confirmed-"+action
				route.Actions[action] = func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
					if action == "archive" && request.Values["row"] != "" && request.Values["view"] != "archived" {
						result, needed, err := beforeWidgetChange(ctx, devices, request, directID)
						if err != nil || needed {
							return result, err
						}
					}
					return baseAction(ctx, request)
				}
				baseResponse := route.Presentations[action]
				route.Presentations[action] = func(ctx context.Context, request nativeui.Request, choice nativeui.PresentationResult) (nativeui.Result, error) {
					if !choice.Cancelled && choice.Choice == "apply" && !(action == "archive" && request.Values["view"] == "archived") {
						result, needed, err := beforeWidgetChange(ctx, devices, request, confirmedID)
						if err != nil || needed {
							return result, err
						}
					}
					return baseResponse(ctx, request, choice)
				}
				route.Services[directID] = afterWidgetClear(devices, baseAction)
				route.Services[confirmedID] = afterWidgetClear(devices, func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
					return baseResponse(ctx, request, nativeui.PresentationResult{Choice: "apply"})
				})
			}
		case "/settings":
			base := route.Screen
			route.Screen = func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
				screen, err := base(ctx, request)
				if err != nil {
					return screen, err
				}
				screen.List.Rows = append([]nativeui.Row{
					{ID: "inbox", Title: "Shared items", Subtitle: "Review text and files sent to Notes", Destination: "/inbox"},
					{ID: "protected", Title: "Protected notes", Subtitle: "Encrypted text opened with biometrics", Destination: "/protected"},
					{ID: "widget", Title: "Home screen widget", Subtitle: "Manage the note shown outside the app", Destination: "/settings/widget"},
				}, screen.List.Rows...)
				return screen, nil
			}
		case "/notes/new", "/notes/{id}":
			base := route.Screen
			route.Screen = func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
				screen, err := base(ctx, request)
				if err == nil && request.Params["id"] != "" {
					screen.Actions = append(screen.Actions, nativeui.Action{ID: "widget", Label: "Show on widget"})
				}
				return screen, err
			}
			baseSave := route.Actions["save"]
			route.Actions["save"] = func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				result, err := baseSave(ctx, request)
				if err != nil {
					return result, err
				}
				state, err := devices.Snapshot(ctx)
				if err != nil {
					return nativeui.Result{}, err
				}
				if request.Params["id"] == "" || state.WidgetNote != request.Params["id"] || state.WidgetClearPending {
					return result, nil
				}
				note, err := service.Get(ctx, state.WidgetNote)
				if err != nil {
					return nativeui.Result{}, err
				}
				result.Navigation = nil
				result.Service = &nativeui.ServiceRequest{ID: "widget-save", Widget: &widgets.Request{Operation: widgets.Publish, Snapshot: noteWidget(note)}}
				return result, nil
			}
			route.Actions["widget"] = func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
				note, err := service.Get(ctx, request.Params["id"])
				if err != nil {
					return nativeui.Result{}, presentationError(err)
				}
				if note.Archived || note.Trashed {
					return nativeui.Result{}, fileError("Choose an active saved note for the widget.")
				}
				return nativeui.Result{Presentation: &nativeui.Presentation{ID: "widget", Kind: nativeui.PresentationConfirmation, Title: "Show this note on your widget?", Message: "The saved title and a text preview will be visible outside Notes. Future saves update this widget.", Dismissible: true, Choices: []nativeui.PresentationChoice{{ID: "cancel", Label: "Cancel", Role: "cancel"}, {ID: "publish", Label: "Show note"}}}}, nil
			}
			route.Presentations = map[string]nativeui.Respond{"widget": func(ctx context.Context, request nativeui.Request, choice nativeui.PresentationResult) (nativeui.Result, error) {
				if choice.Cancelled || choice.Choice != "publish" {
					return nativeui.Result{}, nil
				}
				note, err := service.Get(ctx, request.Params["id"])
				if err != nil {
					return nativeui.Result{}, presentationError(err)
				}
				if note.Archived || note.Trashed {
					return nativeui.Result{}, fileError("Choose an active saved note for the widget.")
				}
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "widget", Commit: true, Widget: &widgets.Request{Operation: widgets.Publish, Snapshot: noteWidget(note)}}}, nil
			}}
			route.Services["widget"] = func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
				if outcome.Status != nativeui.EffectCompleted {
					return featureOutcome("The widget change was not confirmed.", outcome), nil
				}
				if err := devices.Update(ctx, func(state *notedevices.State) error {
					state.WidgetNote = request.Params["id"]
					state.WidgetClearPending = false
					return nil
				}); err != nil {
					return nativeui.Result{}, err
				}
				return nativeui.Result{Announcement: "Widget content saved. Add the Notes widget from your home screen."}, nil
			}
			route.Services["widget-save"] = func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
				message := "Note and widget content saved."
				if outcome.Status != nativeui.EffectCompleted {
					message = "Note saved. The widget could not be updated; publish it again from this note."
				}
				return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "pop"}, Announcement: message}, nil
			}
		}
	}
	routes = append(routes, inboxRoutes(service, attachments, devices)...)
	routes = append(routes, protectedRoutes(devices)...)
	return append(routes, widgetSettings(service, devices))
}

func noteWidget(note notes.Note) *widgets.Snapshot {
	title := boundedText(strings.Join(strings.Fields(note.Title), " "), 200)
	if title == "" {
		title = "Note"
	}
	return &widgets.Snapshot{Title: title, Text: boundedText(strings.Join(strings.Fields(note.Body), " "), 2048), Route: "/notes/" + url.PathEscape(note.ID)}
}

func clearWidget(ctx context.Context, devices *notedevices.Store) (nativeui.Result, error) {
	if err := devices.Update(ctx, func(state *notedevices.State) error {
		state.WidgetClearPending = true
		return nil
	}); err != nil {
		return nativeui.Result{}, err
	}
	return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "widget-cleared", Commit: true, Widget: &widgets.Request{Operation: widgets.Clear}}}, nil
}

func beforeWidgetChange(ctx context.Context, devices *notedevices.Store, request nativeui.Request, serviceID string) (nativeui.Result, bool, error) {
	state, err := devices.Snapshot(ctx)
	if err != nil {
		return nativeui.Result{}, false, err
	}
	if state.WidgetNote == "" {
		return nativeui.Result{}, false, nil
	}
	ids := []string{request.Values["row"]}
	if ids[0] == "" {
		if json.Unmarshal([]byte(request.Values["selected"]), &ids) != nil {
			return nativeui.Result{}, false, nil
		}
	}
	for _, id := range ids {
		if id != state.WidgetNote {
			continue
		}
		result, err := clearWidget(ctx, devices)
		if err == nil {
			result.Service.ID = serviceID
		}
		return result, true, err
	}
	return nativeui.Result{}, false, nil
}

func afterWidgetClear(devices *notedevices.Store, next nativeui.Handle) nativeui.ServiceRespond {
	return func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
		result, err := widgetCleared(devices)(ctx, request, outcome)
		if err != nil || outcome.Status != nativeui.EffectCompleted {
			return result, err
		}
		return next(ctx, request)
	}
}

func widgetCleared(devices *notedevices.Store) nativeui.ServiceRespond {
	return func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
		if outcome.Status != nativeui.EffectCompleted {
			return featureOutcome("Widget content may still be visible. Clear it again from Widget settings.", outcome), nil
		}
		if err := devices.Update(ctx, func(state *notedevices.State) error {
			state.WidgetNote = ""
			state.WidgetClearPending = false
			return nil
		}); err != nil {
			return nativeui.Result{}, err
		}
		return nativeui.Result{Announcement: "Widget content cleared."}, nil
	}
}

func widgetSettings(service *notes.Service, devices *notedevices.Store) nativeui.Route {
	return nativeui.Route{Path: "/settings/widget", Screen: func(ctx context.Context, request nativeui.Request) (nativeui.Screen, error) {
		state, err := devices.Snapshot(ctx)
		if err != nil {
			return nativeui.Screen{}, err
		}
		text := "Open a saved note and choose Show on widget to publish its title and a text preview. Add Notes from your home screen's widget picker."
		if state.WidgetNote != "" {
			if note, err := service.Get(ctx, state.WidgetNote); err == nil {
				text = "Selected note\n" + note.Title + "\n\nSaving this note updates the widget. Archiving or moving it to Trash clears the widget."
			}
		}
		if state.WidgetClearPending {
			text = "Widget cleanup was not confirmed. Old content may still be visible. Tap Clear widget to finish."
		}
		return nativeui.Screen{Title: "Home screen widget", Text: text, Actions: []nativeui.Action{{ID: "status", Label: "Check widget", Primary: true}, {ID: "clear", Label: "Clear widget"}}}, nil
	}, Actions: map[string]nativeui.Handle{
		"status": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "status", Widget: &widgets.Request{Operation: widgets.Status}}}, nil
		},
		"clear": func(ctx context.Context, request nativeui.Request) (nativeui.Result, error) {
			return clearWidget(ctx, devices)
		},
	}, Services: map[string]nativeui.ServiceRespond{
		"widget-cleared": widgetCleared(devices),
		"status": func(ctx context.Context, request nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
			if outcome.Status != nativeui.EffectCompleted {
				return featureOutcome("Widget status could not be checked.", outcome), nil
			}
			if outcome.Widget.Published {
				return nativeui.Result{Announcement: "Widget content is stored. Home screen placement and refresh are controlled by the device."}, nil
			}
			return nativeui.Result{Announcement: "No widget content is stored."}, nil
		},
	}}
}
