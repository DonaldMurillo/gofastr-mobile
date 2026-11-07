// Package gallery is a runnable catalog of the screen contracts nativeui
// supports today. Each demo states what it needs and declares Requires, so a
// renderer without that capability refuses to open it instead of the demo
// faking a result. Demo state lives in memory; there is no repository.
package gallery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

// Root is the first screen.
const Root = "/gallery"

// LegacyRenderPath is the matrix copy shown when a screen renders outside a
// negotiated Runtime connection, for example through the legacy Render export.
const LegacyRenderPath = "unknown (legacy render path)"

// MatrixFooter qualifies every capability value the gallery prints.
const MatrixFooter = "Advertised by this renderer build. Not proof of device support."

// New returns the gallery screens with fresh in-memory demo state.
func New() (*nativeui.App, error) { return newApp(nil) }

type state struct {
	mu      sync.Mutex
	saved   map[string]string
	version int
	changed chan struct{}
}

// newApp registers every route. onCall, when set, observes each application
// handler invocation as "<route pattern> <handler>", so tests can prove a
// refused screen ran nothing.
func newApp(onCall func(string)) (*nativeui.App, error) {
	demo := &state{saved: map[string]string{}, changed: make(chan struct{})}
	called := func(name string) {
		if onCall != nil {
			onCall(name)
		}
	}
	images, err := demoImages()
	if err != nil {
		return nil, err
	}
	routes := []nativeui.Route{
		tabRoot(Root, "controls", "Controls", "Each row opens one demo. A demo that needs a capability says so and refuses to open where the renderer lacks it.", []nativeui.Row{
			{ID: "fields", Title: "Form fields", Subtitle: "Six field kinds, required, disabled, validation", Destination: "/gallery/fields"},
			{ID: "document", Title: "Document form", Subtitle: "Document style with a details section", Icon: nativeui.IconFile, Destination: "/gallery/document"},
			{ID: "draft", Title: "Drafts", Subtitle: "Local recovery and the leave prompt", Destination: "/gallery/draft"},
			{ID: "unknown", Title: "Simulated unknown save", Subtitle: "What happens when a save's outcome is lost", Destination: "/gallery/unknown"},
		}, called),
		fieldsRoute(demo, called),
		documentRoute(demo, called),
		draftRoute(called),
		unknownRoute(called),
		tabRoot("/lists", "lists", "Lists", "List contracts: search, filters, paging, row actions, selection, empty and live lists.", []nativeui.Row{
			{ID: "browse", Title: "Search, filter and page", Subtitle: "60 generated rows", Destination: "/lists/browse"},
			{ID: "actions", Title: "Row actions", Subtitle: "Leading and trailing swipe actions", Destination: "/lists/actions"},
			{ID: "select", Title: "Selection", Subtitle: "Pick up to three rows", Destination: "/lists/select"},
			{ID: "empty", Title: "Empty list", Destination: "/lists/empty"},
			{ID: "live", Title: "Live list", Subtitle: "Refreshes when Go data changes", Destination: "/lists/live"},
		}, called),
		browseRoute(called),
		{Path: "/lists/rows/{id}", Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
			called("/lists/rows/{id} screen")
			number, err := strconv.Atoi(request.Params["id"])
			if err != nil || number < 1 || number > browseRows {
				return nativeui.Screen{}, &nativeui.ActionError{Code: "not_found", Message: "That row does not exist."}
			}
			return nativeui.Screen{Title: rowTitle(number), Text: "Row details come from the route parameter. Nothing is stored."}, nil
		}},
		rowActionsRoute(called),
		selectRoute(called),
		{Path: "/lists/empty", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			called("/lists/empty screen")
			return nativeui.Screen{Title: "Empty list", List: &nativeui.List{Empty: "Nothing here yet.\nThis list is empty on purpose.", Rows: []nativeui.Row{}}}, nil
		}},
		liveRoute(demo, called),
		tabRoot("/feedback", "feedback", "Feedback", "Presentations, effects, text and images.", feedbackRows(), called),
		presentRoute(called),
		{Path: "/feedback/effects", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			called("/feedback/effects screen")
			return nativeui.Screen{Title: "Effects", Prompt: "Each effect has its own screen, so a fallback or refusal shows per effect.", List: &nativeui.List{Rows: []nativeui.Row{
				{ID: "haptic", Title: "Haptic", Destination: "/feedback/effects/haptic"},
				{ID: "announcement", Title: "Announcement", Destination: "/feedback/effects/announcement"},
				{ID: "share", Title: "Share text", Destination: "/feedback/effects/share"},
				{ID: "navigation", Title: "Push and pop", Destination: "/feedback/navigation"},
				{ID: "sheet", Title: "Sheet and dismiss", Destination: "/feedback/sheet"},
			}}}, nil
		}},
		effectRoute(called),
		{Path: "/feedback/text", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			called("/feedback/text screen")
			return nativeui.Screen{Title: "Text", Text: sampleText}, nil
		}},
		{Path: "/feedback/images", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			called("/feedback/images screen")
			return nativeui.Screen{Title: "Images", Text: "Go draws both previews at startup with image/png. nativeui.NewImage re-encodes them without metadata.", Images: images}, nil
		}},
		tabRoot("/device", "device", "Device", "Device contracts. The capability matrix shows what this renderer advertised.", []nativeui.Row{
			{ID: "capabilities", Title: "Capability matrix", Destination: "/device/capabilities"},
			{ID: "files", Title: "Files", Subtitle: "Export and import", Icon: nativeui.IconFile, Destination: "/device/files"},
			{ID: "clipboard", Title: "Clipboard", Destination: "/device/clipboard"},
			{ID: "share-text", Title: "System share", Destination: "/device/share-text"},
			{ID: "protected", Title: "Protected screen", Destination: "/device/protected"},
			{ID: "more", Title: "More device features", Destination: "/device/more"},
		}, called),
		{Path: "/device/capabilities", Screen: func(ctx context.Context, _ nativeui.Request) (nativeui.Screen, error) {
			called("/device/capabilities screen")
			return nativeui.Screen{Title: "Capability matrix", Text: Matrix(ctx)}, nil
		}},
		filesRoute(called),
		serviceRoute("/device/clipboard", "Clipboard", "Copies a fixed sentence to the clipboard. The result shows the status the OS reported.", "copy", "Copy text",
			nativeui.CapabilityClipboard, nativeui.ServiceRequest{ID: "clipboard", Clipboard: &sharing.Clipboard{Text: "Copied from the GoFastr gallery."}}, called),
		serviceRoute("/device/share-text", "System share", "Opens the system share sheet with a fixed sentence.", "share", "Share text",
			nativeui.CapabilitySharing, nativeui.ServiceRequest{ID: "share", Share: &sharing.Request{Title: "Gallery", Text: "Shared from the GoFastr gallery."}}, called),
		{Path: "/device/protected", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			called("/device/protected screen")
			return nativeui.Screen{Title: "Protected screen", Text: "Native hosts hide this screen in app snapshots and replace it with the lock screen when the app goes to the background. Renderers without privacy/v1, such as the browser, refuse to open it.",
				Protection: &nativeui.ScreenProtection{LockRoute: "/device/locked"}}, nil
		}},
		{Path: "/device/locked", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			called("/device/locked screen")
			return nativeui.Screen{Title: "Locked", Text: "The protected screen was retired when the app left the foreground. Open it again from Device."}, nil
		}},
		{Path: "/device/more", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			called("/device/more screen")
			return nativeui.Screen{Title: "More device features", Text: moreText}, nil
		}},
	}
	routes = append(routes, navigationRoutes(called)...)
	return nativeui.New(Root, routes...)
}

var tabs = []nativeui.Tab{
	{ID: "controls", Label: "Controls", Route: Root, Icon: nativeui.IconCompose},
	{ID: "lists", Label: "Lists", Route: "/lists", Icon: nativeui.IconNotes},
	{ID: "feedback", Label: "Feedback", Route: "/feedback", Icon: nativeui.IconInfo},
	{ID: "device", Label: "Device", Route: "/device", Icon: nativeui.IconSettings},
}

func tabRoot(path, id, title, prompt string, rows []nativeui.Row, called func(string)) nativeui.Route {
	return nativeui.Route{Path: path, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called(path + " screen")
		return nativeui.Screen{Title: title, LargeTitle: true, Prompt: prompt, Tabs: tabs, ActiveTab: id, List: &nativeui.List{Rows: rows}}, nil
	}}
}

func editor() []nativeui.Requirement {
	return []nativeui.Requirement{{Capability: nativeui.CapabilityEditor}}
}

func fieldsRoute(demo *state, called func(string)) nativeui.Route {
	return nativeui.Route{Path: "/gallery/fields", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called("/gallery/fields screen")
		demo.mu.Lock()
		saved := demo.saved
		demo.mu.Unlock()
		return nativeui.Screen{Title: "Form fields", Prompt: "Needs editor/v1. Validate checks the required name. Save commits to memory only.", Requires: editor(),
			Actions: []nativeui.Action{{ID: "validate", Label: "Validate"}, {ID: "save", Label: "Save", Primary: true, Commit: true}},
			Form: &nativeui.Form{Status: "Saved values last until the gallery stops.", Fields: []nativeui.Field{
				{Key: "name", Label: "Name", Placeholder: "Required", Kind: "text", Style: "title", Required: true, Value: saved["name"]},
				{Key: "notes", Label: "Notes", Placeholder: "Several lines", Kind: "multiline", Style: "body", Value: saved["notes"]},
				{Key: "subscribed", Label: "Subscribe", Kind: "toggle", Value: saved["subscribed"]},
				{Key: "day", Label: "Day", Kind: "date", Value: saved["day"]},
				{Key: "at", Label: "Time", Kind: "time", Value: saved["at"]},
				{Key: "size", Label: "Size", Kind: "select", Value: saved["size"], Options: []nativeui.Option{
					{Value: "", Label: "Choose a size"}, {Value: "small", Label: "Small"}, {Value: "medium", Label: "Medium"}, {Value: "huge", Label: "Huge (unavailable)", Disabled: true}}},
				{Key: "fixed", Label: "Fixed", Kind: "text", Style: "detail", Disabled: true, Value: "Disabled fields cannot change"},
			}}}, nil
	}, Actions: map[string]nativeui.Handle{
		"validate": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
			called("/gallery/fields validate")
			if strings.TrimSpace(request.Values["name"]) == "" {
				return nativeui.Result{}, &nativeui.ActionError{Code: "validation", Message: "Fix the highlighted field.", Fields: map[string]string{"name": "Enter a name."}}
			}
			return nativeui.Result{Announcement: "All fields are valid."}, nil
		},
		"save": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
			called("/gallery/fields save")
			if strings.TrimSpace(request.Values["name"]) == "" {
				return nativeui.Result{}, &nativeui.ActionError{Code: "validation", Message: "Fix the highlighted field.", Fields: map[string]string{"name": "Enter a name."}}
			}
			values := map[string]string{}
			for _, key := range []string{"name", "notes", "subscribed", "day", "at", "size"} {
				values[key] = request.Values[key]
			}
			values["fixed"] = "Disabled fields cannot change"
			demo.mu.Lock()
			demo.saved = values
			demo.mu.Unlock()
			return nativeui.Result{Saved: &nativeui.SavedState{Route: "/gallery/fields", Values: values}, Announcement: "Saved in memory."}, nil
		},
	}}
}

func documentRoute(demo *state, called func(string)) nativeui.Route {
	return nativeui.Route{Path: "/gallery/document", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called("/gallery/document screen")
		demo.mu.Lock()
		title, body := demo.saved["doc-title"], demo.saved["doc-body"]
		demo.mu.Unlock()
		return nativeui.Screen{Title: "Document form", Prompt: "Needs editor/v1. Document style puts the title and body first and the rest under a details section.", Requires: editor(),
			Actions: []nativeui.Action{{ID: "save", Label: "Save", Primary: true, Commit: true}},
			Form: &nativeui.Form{Style: "document", DetailsTitle: "Details", Fields: []nativeui.Field{
				{Key: "doc-title", Label: "Title", Placeholder: "Title", Kind: "text", Style: "title", Value: title},
				{Key: "doc-body", Label: "Body", Placeholder: "Start writing", Kind: "multiline", Style: "body", Value: body},
				{Key: "doc-pinned", Label: "Pinned", Kind: "toggle"},
			}}}, nil
	}, Actions: map[string]nativeui.Handle{"save": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		called("/gallery/document save")
		values := map[string]string{"doc-title": request.Values["doc-title"], "doc-body": request.Values["doc-body"], "doc-pinned": request.Values["doc-pinned"]}
		demo.mu.Lock()
		demo.saved["doc-title"], demo.saved["doc-body"] = values["doc-title"], values["doc-body"]
		demo.mu.Unlock()
		return nativeui.Result{Saved: &nativeui.SavedState{Route: "/gallery/document", Values: values}, Announcement: "Saved in memory."}, nil
	}}}
}

func draftRoute(called func(string)) nativeui.Route {
	return nativeui.Route{Path: "/gallery/draft", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called("/gallery/draft screen")
		return nativeui.Screen{Title: "Drafts", Prompt: "Needs editor/v1 and drafts/v1 (a fallback is allowed). Type, then go back to see the leave prompt. Discard clears the local draft.",
			Requires: []nativeui.Requirement{{Capability: nativeui.CapabilityEditor}, {Capability: nativeui.CapabilityDrafts, AllowFallback: true}},
			Actions:  []nativeui.Action{{ID: "finish", Label: "Finish", Primary: true, Commit: true}},
			Form: &nativeui.Form{Draft: &nativeui.Draft{Key: "gallery-draft", Changed: "Draft saved on this device.", Restored: "Restored your local draft.", BackLabel: "Back",
				Title: "Keep this draft?", Message: "Your text is stored only on this device.", KeepEditing: "Keep editing", KeepDraft: "Keep draft", Discard: "Discard"},
				Fields: []nativeui.Field{{Key: "draft-text", Label: "Draft", Placeholder: "Type something, then go back", Kind: "multiline", Style: "body"}}}}, nil
	}, Actions: map[string]nativeui.Handle{"finish": func(context.Context, nativeui.Request) (nativeui.Result, error) {
		called("/gallery/draft finish")
		return nativeui.Result{ClearDraft: true, Announcement: "Draft cleared. Nothing else was stored."}, nil
	}}}
}

// ErrSimulatedLoss is the plain error the unknown-save demo returns.
var ErrSimulatedLoss = errors.New("gallery: simulated lost response")

func unknownRoute(called func(string)) nativeui.Route {
	return nativeui.Route{Path: "/gallery/unknown", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called("/gallery/unknown screen")
		return nativeui.Screen{Title: "Simulated unknown save", Prompt: "Needs editor/v1. This demo writes nothing. Save returns a plain Go error, so the runtime cannot tell whether a write happened. It reports the outcome as unknown and refuses a retry until you reopen the screen.",
			Requires: editor(), Actions: []nativeui.Action{{ID: "save", Label: "Save", Primary: true, Commit: true}},
			Form: &nativeui.Form{Fields: []nativeui.Field{{Key: "text", Label: "Text", Placeholder: "Anything", Kind: "text", Value: "Example"}}}}, nil
	}, Actions: map[string]nativeui.Handle{"save": func(context.Context, nativeui.Request) (nativeui.Result, error) {
		called("/gallery/unknown save")
		return nativeui.Result{}, ErrSimulatedLoss
	}}}
}

const browseRows, pageSize = 60, 20

func rowTitle(number int) string { return fmt.Sprintf("Row %02d", number) }

func browseRoute(called func(string)) nativeui.Route {
	return nativeui.Route{Path: "/lists/browse", Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
		called("/lists/browse screen")
		query := strings.ToLower(strings.TrimSpace(request.Values["q"]))
		parity := request.Values["parity"]
		shown := pageSize
		if page := request.Values["page"]; strings.HasPrefix(page, "p") {
			if count, err := strconv.Atoi(page[1:]); err == nil && count > 0 && count <= browseRows {
				shown = count
			}
		}
		var matching []nativeui.Row
		for number := 1; number <= browseRows; number++ {
			if parity == "even" && number%2 != 0 || parity == "odd" && number%2 == 0 {
				continue
			}
			title := rowTitle(number)
			row := nativeui.Row{ID: "r" + strconv.Itoa(number), Title: title, Subtitle: "Generated in Go", Metadata: "#" + strconv.Itoa(number), Destination: "/lists/rows/" + strconv.Itoa(number), Disabled: number == 7}
			if row.Disabled {
				row.Subtitle = "Disabled rows cannot be opened"
			}
			if query != "" {
				index := strings.Index(strings.ToLower(title), query)
				if index < 0 {
					continue
				}
				start := len([]rune(title[:index]))
				row.TitleMatches = []nativeui.TextRange{{Start: start, End: start + len([]rune(query))}}
			}
			matching = append(matching, row)
		}
		list := &nativeui.List{Header: "Generated rows", SearchKey: "q", SearchPlaceholder: "Search rows", Refreshable: true,
			Empty:   "No rows match.\nTry another search or filter.",
			Filters: []nativeui.Field{{Key: "parity", Label: "Show", Kind: "select", Value: parity, Options: []nativeui.Option{{Value: "", Label: "All rows"}, {Value: "even", Label: "Even rows"}, {Value: "odd", Label: "Odd rows"}}}},
			Paging:  &nativeui.Paging{Key: "page", Label: "Load more"}, Rows: []nativeui.Row{}}
		if len(matching) > shown {
			list.Paging.Next = "p" + strconv.Itoa(shown+pageSize)
			matching = matching[:shown]
		}
		list.Rows = append(list.Rows, matching...)
		list.Footer = fmt.Sprintf("%d rows shown", len(list.Rows))
		return nativeui.Screen{Title: "Search, filter and page", List: list}, nil
	}}
}

func rowActionsRoute(called func(string)) nativeui.Route {
	announce := func(name, verb string) nativeui.Handle {
		return func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
			called("/lists/actions " + name)
			return nativeui.Result{Announcement: verb + " " + strings.TrimPrefix(request.Values["row"], "a") + ". Nothing was stored.", Haptic: "selection"}, nil
		}
	}
	return nativeui.Route{Path: "/lists/actions", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called("/lists/actions screen")
		rows := make([]nativeui.Row, 0, 5)
		for number := 1; number <= 5; number++ {
			rows = append(rows, nativeui.Row{ID: "a" + strconv.Itoa(number), Title: "Swipe row " + strconv.Itoa(number), Destination: "/lists/rows/" + strconv.Itoa(number),
				LeadingActions:  []nativeui.RowAction{{ID: "flag", Label: "Flag", FullSwipe: true}},
				TrailingActions: []nativeui.RowAction{{ID: "remove", Label: "Remove", Role: "destructive"}}})
		}
		return nativeui.Screen{Title: "Row actions", Prompt: "Swipe right for Flag (full swipe allowed). Swipe left for Remove, which needs a tap because it is destructive. Both only announce.", List: &nativeui.List{Rows: rows}}, nil
	}, Actions: map[string]nativeui.Handle{"flag": announce("flag", "Flagged row"), "remove": announce("remove", "Would remove row")}}
}

func selectRoute(called func(string)) nativeui.Route {
	available := map[string]bool{}
	rows := make([]nativeui.Row, 0, 8)
	for number := 1; number <= 8; number++ {
		id := "s" + strconv.Itoa(number)
		available[id] = true
		rows = append(rows, nativeui.Row{ID: id, Title: "Choice " + strconv.Itoa(number), Destination: "/lists/rows/" + strconv.Itoa(number)})
	}
	return nativeui.Route{Path: "/lists/select", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called("/lists/select screen")
		return nativeui.Screen{Title: "Selection", Prompt: "Select up to three rows, then Count.", Actions: []nativeui.Action{{ID: "count", Label: "Count", Primary: true}},
			List: &nativeui.List{Selection: &nativeui.Selection{Key: "selected", Selected: []string{}, Maximum: 3}, Rows: rows}}, nil
	}, Actions: map[string]nativeui.Handle{"count": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		called("/lists/select count")
		selected, err := behavior.DecodeSelection(request.Values["selected"], available, 3)
		if err != nil {
			return nativeui.Result{}, &nativeui.ActionError{Code: "validation", Message: "Choose at most three rows.", Fields: map[string]string{"selected": "Choose at most three rows."}}
		}
		return nativeui.Result{Announcement: fmt.Sprintf("%d of 3 selected.", len(selected))}, nil
	}}}
}

func liveRoute(demo *state, called func(string)) nativeui.Route {
	version := func() string {
		demo.mu.Lock()
		defer demo.mu.Unlock()
		return "v" + strconv.Itoa(demo.version)
	}
	return nativeui.Route{Path: "/lists/live", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called("/lists/live screen")
		current := version()
		return nativeui.Screen{Title: "Live list", Prompt: "Needs live/v1. Add bumps an in-memory version; open screens refresh through Watch.", Requires: []nativeui.Requirement{{Capability: nativeui.CapabilityLive}},
			Live: &nativeui.Live{Version: current}, Actions: []nativeui.Action{{ID: "bump", Label: "Add", Primary: true}},
			List: &nativeui.List{Footer: "Data version " + current, Rows: liveRows(current)}}, nil
	}, Actions: map[string]nativeui.Handle{"bump": func(context.Context, nativeui.Request) (nativeui.Result, error) {
		called("/lists/live bump")
		demo.mu.Lock()
		demo.version++
		close(demo.changed)
		demo.changed = make(chan struct{})
		demo.mu.Unlock()
		return nativeui.Result{Announcement: "Data changed in Go."}, nil
	}}, Watch: func(ctx context.Context, _ nativeui.Request, seen string) (string, error) {
		called("/lists/live watch")
		for {
			demo.mu.Lock()
			current, changed := "v"+strconv.Itoa(demo.version), demo.changed
			demo.mu.Unlock()
			if current != seen {
				return current, nil
			}
			select {
			case <-ctx.Done():
				return seen, ctx.Err()
			case <-changed:
			}
		}
	}}
}

func liveRows(version string) []nativeui.Row {
	count, _ := strconv.Atoi(strings.TrimPrefix(version, "v"))
	rows := []nativeui.Row{}
	for number := count; number >= 1 && len(rows) < 20; number-- {
		rows = append(rows, nativeui.Row{ID: "l" + strconv.Itoa(number), Title: "Update " + strconv.Itoa(number), Destination: "/lists/rows/" + strconv.Itoa(min(number, browseRows))})
	}
	if len(rows) == 0 {
		rows = append(rows, nativeui.Row{ID: "l0", Title: "No updates yet", Disabled: true})
	}
	return rows
}

var presentations = map[string]nativeui.Presentation{
	"alert": {Kind: nativeui.PresentationAlert, Title: "Alert", Message: "An alert has one acknowledgement.", Choices: []nativeui.PresentationChoice{{ID: "ok", Label: "OK"}}},
	"confirmation": {Kind: nativeui.PresentationConfirmation, Title: "Delete the sample?", Message: "Nothing is deleted. This only reports your choice.", Choices: []nativeui.PresentationChoice{
		{ID: "cancel", Label: "Cancel", Role: "cancel"}, {ID: "delete", Label: "Delete", Role: "destructive"}}, Dismissible: true},
	"menu": {Kind: nativeui.PresentationMenu, Title: "Choose an option", Choices: []nativeui.PresentationChoice{
		{ID: "first", Label: "First option"}, {ID: "second", Label: "Second option"}, {ID: "unavailable", Label: "Unavailable option", Disabled: true}, {ID: "cancel", Label: "Cancel", Role: "cancel"}}, Dismissible: true},
	"dialog": {Kind: nativeui.PresentationDialog, Title: "Dialog", Message: "A dialog asks one question.", Choices: []nativeui.PresentationChoice{
		{ID: "cancel", Label: "Not now", Role: "cancel"}, {ID: "continue", Label: "Continue"}}, Dismissible: true},
	"sheet": {Kind: nativeui.PresentationSheet, Title: "Sheet", Message: "A sheet holds more content than a dialog.", Choices: []nativeui.PresentationChoice{
		{ID: "cancel", Label: "Close", Role: "cancel"}, {ID: "done", Label: "Done"}}, Dismissible: true},
	"single": {Kind: nativeui.PresentationSheet, Title: "Pick one color", Choices: []nativeui.PresentationChoice{{ID: "cancel", Label: "Cancel", Role: "cancel"}, {ID: "choose", Label: "Choose"}}, Dismissible: true,
		Selection: &nativeui.Selection{Options: colorOptions(), Selected: []string{"blue"}, Min: 1, Max: 1}},
	"multiple": {Kind: nativeui.PresentationDialog, Title: "Pick up to two colors", Choices: []nativeui.PresentationChoice{{ID: "cancel", Label: "Cancel", Role: "cancel"}, {ID: "choose", Label: "Choose"}}, Dismissible: true,
		Selection: &nativeui.Selection{Options: colorOptions(), Selected: []string{}, Multiple: true, Min: 0, Max: 2}},
}

// PresentationKinds lists the presentation demos in display order.
var PresentationKinds = []string{"alert", "confirmation", "menu", "dialog", "sheet", "single", "multiple"}

func colorOptions() []nativeui.SelectionOption {
	return []nativeui.SelectionOption{{ID: "red", Label: "Red"}, {ID: "green", Label: "Green"}, {ID: "blue", Label: "Blue", Detail: "Selected by default in the single-choice demo"}, {ID: "gray", Label: "Gray", Disabled: true}}
}

func feedbackRows() []nativeui.Row {
	labels := map[string]string{"alert": "Alert", "confirmation": "Confirmation", "menu": "Menu with a disabled choice", "dialog": "Dialog", "sheet": "Sheet", "single": "Single selection", "multiple": "Multiple selection"}
	rows := make([]nativeui.Row, 0, len(PresentationKinds)+3)
	for _, kind := range PresentationKinds {
		rows = append(rows, nativeui.Row{ID: kind, Title: labels[kind], Destination: "/feedback/present/" + kind})
	}
	return append(rows, nativeui.Row{ID: "effects", Title: "Effects", Subtitle: "Haptic, announcement, share, navigation", Destination: "/feedback/effects"},
		nativeui.Row{ID: "text", Title: "Text", Destination: "/feedback/text"},
		nativeui.Row{ID: "images", Title: "Images", Icon: nativeui.IconPhoto, Destination: "/feedback/images"})
}

func presentRoute(called func(string)) nativeui.Route {
	return nativeui.Route{Path: "/feedback/present/{kind}", Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
		called("/feedback/present/{kind} screen")
		presentation, ok := presentations[request.Params["kind"]]
		if !ok {
			return nativeui.Screen{}, &nativeui.ActionError{Code: "not_found", Message: "That presentation demo does not exist."}
		}
		return nativeui.Screen{Title: presentation.Title, Prompt: "Needs presentation/v1. Show opens it; the result is announced. Cancelling runs nothing.", Requires: []nativeui.Requirement{{Capability: nativeui.CapabilityPresentation}},
			Actions: []nativeui.Action{{ID: "show", Label: "Show", Primary: true}}, Text: "Kind: " + string(presentation.Kind) + "."}, nil
	}, Actions: map[string]nativeui.Handle{"show": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		called("/feedback/present/{kind} show")
		presentation, ok := presentations[request.Params["kind"]]
		if !ok {
			return nativeui.Result{}, &nativeui.ActionError{Code: "not_found", Message: "That presentation demo does not exist."}
		}
		presentation.ID = "demo"
		if presentation.Selection != nil {
			selection := *presentation.Selection
			selection.Options = append([]nativeui.SelectionOption(nil), selection.Options...)
			selection.Selected = append([]string{}, selection.Selected...)
			presentation.Selection = &selection
		}
		presentation.Choices = append([]nativeui.PresentationChoice(nil), presentation.Choices...)
		return nativeui.Result{Presentation: &presentation}, nil
	}}, Presentations: map[string]nativeui.Respond{"demo": func(_ context.Context, _ nativeui.Request, result nativeui.PresentationResult) (nativeui.Result, error) {
		called("/feedback/present/{kind} demo")
		message := "You chose " + result.Choice + "."
		if len(result.Selected) != 0 {
			selected := append([]string(nil), result.Selected...)
			sort.Strings(selected)
			message += " Selected: " + strings.Join(selected, ", ") + "."
		}
		return nativeui.Result{Announcement: message}, nil
	}}}
}

// effects maps each effect demo to its capability and result.
var effects = map[string]struct {
	title, prompt string
	requirement   nativeui.Requirement
	result        nativeui.Result
}{
	"haptic":       {"Haptic", "Needs haptics/v1 (a fallback is allowed). Run plays a success haptic, or the renderer's fallback.", nativeui.Requirement{Capability: nativeui.CapabilityHaptics, AllowFallback: true}, nativeui.Result{Haptic: "success"}},
	"announcement": {"Announcement", "Needs announcement/v1. Run asks the screen reader to read a sentence.", nativeui.Requirement{Capability: nativeui.CapabilityAnnouncement}, nativeui.Result{Announcement: "This sentence came from Go."}},
	"share":        {"Share text", "Needs share/v1 (a fallback is allowed). Run offers a fixed sentence to share or copy.", nativeui.Requirement{Capability: nativeui.CapabilityShare, AllowFallback: true}, nativeui.Result{Share: "Shared from the GoFastr gallery."}},
}

func effectRoute(called func(string)) nativeui.Route {
	return nativeui.Route{Path: "/feedback/effects/{effect}", Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
		called("/feedback/effects/{effect} screen")
		effect, ok := effects[request.Params["effect"]]
		if !ok {
			return nativeui.Screen{}, &nativeui.ActionError{Code: "not_found", Message: "That effect demo does not exist."}
		}
		return nativeui.Screen{Title: effect.title, Text: effect.prompt, Requires: []nativeui.Requirement{effect.requirement}, Actions: []nativeui.Action{{ID: "run", Label: "Run", Primary: true}}}, nil
	}, Actions: map[string]nativeui.Handle{"run": func(_ context.Context, request nativeui.Request) (nativeui.Result, error) {
		called("/feedback/effects/{effect} run")
		effect, ok := effects[request.Params["effect"]]
		if !ok {
			return nativeui.Result{}, &nativeui.ActionError{Code: "not_found", Message: "That effect demo does not exist."}
		}
		return effect.result, nil
	}}}
}

func navigationRoutes(called func(string)) []nativeui.Route {
	navigate := func(route, name string, navigation nativeui.Navigation) map[string]nativeui.Handle {
		return map[string]nativeui.Handle{name: func(context.Context, nativeui.Request) (nativeui.Result, error) {
			called(route + " " + name)
			return nativeui.Result{Navigation: &navigation}, nil
		}}
	}
	text := func(route, title, body string, requires []nativeui.Requirement, action nativeui.Action, navigation nativeui.Navigation) nativeui.Route {
		return nativeui.Route{Path: route, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			called(route + " screen")
			return nativeui.Screen{Title: title, Text: body, Requires: requires, Actions: []nativeui.Action{action}}, nil
		}, Actions: navigate(route, action.ID, navigation)}
	}
	navigation := []nativeui.Requirement{{Capability: nativeui.CapabilityNavigation}}
	sheets := []nativeui.Requirement{{Capability: nativeui.CapabilitySheets, AllowFallback: true}}
	return []nativeui.Route{
		text("/feedback/navigation", "Push and pop", "Needs navigation/v1. Push renders the next screen in Go before the renderer shows it.", navigation,
			nativeui.Action{ID: "push", Label: "Push", Primary: true}, nativeui.Navigation{Kind: "push", Route: "/feedback/navigation/pushed"}),
		text("/feedback/navigation/pushed", "Pushed screen", "Pop returns to the previous screen.", navigation,
			nativeui.Action{ID: "pop", Label: "Pop", Primary: true}, nativeui.Navigation{Kind: "pop"}),
		text("/feedback/sheet", "Sheet and dismiss", "Needs sheets/v1 (a fallback is allowed). Browser renderers show the sheet as a full page.", sheets,
			nativeui.Action{ID: "open", Label: "Open sheet", Primary: true}, nativeui.Navigation{Kind: "sheet", Route: "/feedback/sheet/open"}),
		text("/feedback/sheet/open", "Sheet", "Dismiss closes the sheet.", sheets,
			nativeui.Action{ID: "dismiss", Label: "Dismiss", Primary: true}, nativeui.Navigation{Kind: "dismiss"}),
	}
}

// ExportText is the fixed document the files demo exports.
const ExportText = "This file was generated by the GoFastr gallery.\n"

func filesRoute(called func(string)) nativeui.Route {
	return nativeui.Route{Path: "/device/files", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called("/device/files screen")
		return nativeui.Screen{Title: "Files", Requires: []nativeui.Requirement{{Capability: nativeui.CapabilityFiles}},
			Text:    "Needs files/v1. Export saves a fixed text file you choose where to put. Import reports the media type and size only; the gallery never shows a file's name or content.",
			Actions: []nativeui.Action{{ID: "export", Label: "Export"}, {ID: "import", Label: "Import", Primary: true}}}, nil
	}, Actions: map[string]nativeui.Handle{
		"export": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			called("/device/files export")
			return nativeui.Result{Files: &nativeui.FileRequest{ID: "export", Export: &files.Document{Name: "gallery-sample.txt", MediaType: "text/plain", Data: []byte(ExportText)}}}, nil
		},
		"import": func(context.Context, nativeui.Request) (nativeui.Result, error) {
			called("/device/files import")
			return nativeui.Result{Files: &nativeui.FileRequest{ID: "import", Pick: &files.Pick{Title: "Choose a file", MaxBytes: 1 << 20}}}, nil
		},
	}, Files: map[string]nativeui.FileRespond{"import": func(_ context.Context, _ nativeui.Request, result nativeui.FileResult) (nativeui.Result, error) {
		called("/device/files import-result")
		if result.Status != nativeui.EffectCompleted || result.File == nil {
			return nativeui.Result{Announcement: "Import " + string(result.Status) + ". Nothing was read."}, nil
		}
		return nativeui.Result{Announcement: fmt.Sprintf("Import completed: %d bytes of %s.", len(result.File.Data), mediaType(result.File.MediaType))}, nil
	}}}
}

// mediaType keeps only a well-formed type/subtype so a picker cannot inject text.
func mediaType(value string) string {
	kind, subtype, ok := strings.Cut(value, "/")
	if !ok || !behavior.SafeToken(kind) || !behavior.SafeToken(strings.ReplaceAll(subtype, "+", "-")) {
		return "an unrecognized type"
	}
	return kind + "/" + subtype
}

func serviceRoute(path, title, text, action, label string, capability nativeui.Capability, request nativeui.ServiceRequest, called func(string)) nativeui.Route {
	return nativeui.Route{Path: path, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		called(path + " screen")
		return nativeui.Screen{Title: title, Text: "Needs " + string(capability) + ". " + text, Requires: []nativeui.Requirement{{Capability: capability}}, Actions: []nativeui.Action{{ID: action, Label: label, Primary: true}}}, nil
	}, Actions: map[string]nativeui.Handle{action: func(context.Context, nativeui.Request) (nativeui.Result, error) {
		called(path + " " + action)
		copied := request
		return nativeui.Result{Service: &copied}, nil
	}}, Services: map[string]nativeui.ServiceRespond{request.ID: func(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
		called(path + " " + request.ID + "-result")
		return nativeui.Result{Announcement: title + ": " + string(result.Status) + "."}, nil
	}}}
}

// Matrix prints one line per known capability with the value the current
// renderer connection advertised, or LegacyRenderPath outside a Runtime.
func Matrix(ctx context.Context) string {
	contract, ok := nativeui.RendererContract(ctx)
	var lines strings.Builder
	if ok {
		fmt.Fprintf(&lines, "Renderer: %s, protocol %d\n\n", contract.Renderer, contract.Version)
	} else {
		lines.WriteString("Renderer: " + LegacyRenderPath + "\n\n")
	}
	for _, capability := range KnownCapabilities {
		value := LegacyRenderPath
		if ok {
			value = string(contract.Capabilities[capability])
			if value == "" {
				value = "not advertised"
			}
		}
		lines.WriteString(string(capability) + ": " + value + "\n")
	}
	lines.WriteString("\n" + MatrixFooter)
	return lines.String()
}

// KnownCapabilities is every built-in capability in protocol order.
var KnownCapabilities = []nativeui.Capability{nativeui.CapabilityNavigation, nativeui.CapabilityEditor, nativeui.CapabilityDrafts, nativeui.CapabilityShare,
	nativeui.CapabilityHaptics, nativeui.CapabilitySheets, nativeui.CapabilityAnnouncement, nativeui.CapabilityPresentation, nativeui.CapabilityFiles,
	nativeui.CapabilityPhotos, nativeui.CapabilityCamera, nativeui.CapabilityNotifications, nativeui.CapabilitySharing, nativeui.CapabilityClipboard,
	nativeui.CapabilityAudio, nativeui.CapabilityLive, nativeui.CapabilityIncomingShares, nativeui.CapabilitySecureStorage, nativeui.CapabilityWidgets, nativeui.CapabilityPrivacy}

func demoImages() ([]nativeui.Image, error) {
	draw := func(pixel func(x, y int) color.Color) ([]byte, error) {
		canvas := image.NewRGBA(image.Rect(0, 0, 240, 160))
		for y := 0; y < 160; y++ {
			for x := 0; x < 240; x++ {
				canvas.Set(x, y, pixel(x, y))
			}
		}
		var encoded bytes.Buffer
		err := png.Encode(&encoded, canvas)
		return encoded.Bytes(), err
	}
	gradient, err := draw(func(x, y int) color.Color { return color.RGBA{uint8(40 + x/2), uint8(90 + y/2), 200, 255} })
	if err != nil {
		return nil, err
	}
	checks, err := draw(func(x, y int) color.Color {
		if (x/20+y/20)%2 == 0 {
			return color.RGBA{230, 230, 230, 255}
		}
		return color.RGBA{60, 60, 70, 255}
	})
	if err != nil {
		return nil, err
	}
	first, err := nativeui.NewImage("gradient", "A blue and green gradient", gradient, 240)
	if err != nil {
		return nil, err
	}
	second, err := nativeui.NewImage("checks", "A gray checkerboard", checks, 240)
	if err != nil {
		return nil, err
	}
	return []nativeui.Image{first, second}, nil
}

const sampleText = "Text screens show plain text from Go. Renderers never interpret it as HTML or Markdown, so <b>tags</b> and *asterisks* stay literal.\n\n" +
	"Long paragraphs wrap to the screen width and follow the system text size. Blank lines separate paragraphs.\n\n" +
	"This screen needs no capability, so every renderer opens it."

const moreText = "Audio, photos, camera, notifications, secure storage, home-screen widgets and the incoming share inbox are covered elsewhere so this gallery does not duplicate them.\n\n" +
	"examples/devicefeatures: secure storage, widgets and the share inbox on both native platforms.\n\n" +
	"examples/notes: audio recording and playback, photos, camera, notifications, attachments and backups inside a full app."
