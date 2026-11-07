package nativeui

import (
	"context"
	"maps"
	"sort"
)

// RouteDescription is static registry metadata. Building it never calls
// application code and never includes handlers.
type RouteDescription struct {
	Path          string   `json:"path"`
	Actions       []string `json:"actions"`
	Presentations []string `json:"presentations,omitempty"`
	Files         []string `json:"files,omitempty"`
	Services      []string `json:"services,omitempty"`
	Live          bool     `json:"live"`
}

// Describe returns registration-ordered route metadata with sorted names.
// The result is a copy; changing it does not change the registry.
func (app *App) Describe() []RouteDescription {
	routes := make([]RouteDescription, 0, len(app.routes))
	for _, route := range app.routes {
		routes = append(routes, RouteDescription{Path: route.Path, Actions: sortedKeys(route.Actions), Presentations: optionalKeys(route.Presentations),
			Files: optionalKeys(route.Files), Services: optionalKeys(route.Services), Live: route.Watch != nil})
	}
	return routes
}

// RoutePattern maps a concrete local destination, which may carry IDs and a
// query, to the registered pattern that serves it. It runs no application code.
func (app *App) RoutePattern(destination string) (string, bool) {
	route, _, err := app.resolve(destination, nil)
	if err != nil {
		return "", false
	}
	return route.Path, true
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func optionalKeys[V any](values map[string]V) []string {
	if len(values) == 0 {
		return nil
	}
	return sortedKeys(values)
}

// ScreenOutline is the structure of a rendered screen: registered patterns,
// author tokens, kinds, counts and booleans. It never includes titles, labels,
// prompts, text, field values, row titles or IDs, image bytes, file names,
// component payloads or concrete destinations.
type ScreenOutline struct {
	Route        string             `json:"route"`
	Content      string             `json:"content"`
	Protected    bool               `json:"protected"`
	Requires     []Requirement      `json:"requires,omitempty"`
	Actions      []OutlineAction    `json:"actions,omitempty"`
	FormStyle    string             `json:"formStyle,omitempty"`
	Fields       []OutlineField     `json:"fields,omitempty"`
	List         *OutlineList       `json:"list,omitempty"`
	Tabs         []OutlineTab       `json:"tabs,omitempty"`
	ActiveTab    string             `json:"activeTab,omitempty"`
	Components   []OutlineComponent `json:"components,omitempty"`
	Images       int                `json:"images,omitempty"`
	Attachments  int                `json:"attachments,omitempty"`
	Destinations []string           `json:"destinations,omitempty"`
	Live         bool               `json:"live"`
	Draft        bool               `json:"draft"`
}

// OutlineAction describes a declared navigation-bar action without its label.
type OutlineAction struct {
	ID      string `json:"id"`
	Icon    Icon   `json:"icon,omitempty"`
	Leading bool   `json:"leading,omitempty"`
	Primary bool   `json:"primary,omitempty"`
	Commit  bool   `json:"commit,omitempty"`
}

// OutlineField describes a control's contract without label or value.
type OutlineField struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Style    string `json:"style,omitempty"`
	Required bool   `json:"required,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Options  int    `json:"options,omitempty"`
}

// OutlineList counts rows and lists declared list controls.
type OutlineList struct {
	Rows         int            `json:"rows"`
	DisabledRows int            `json:"disabledRows,omitempty"`
	RowActions   []string       `json:"rowActions,omitempty"`
	SearchKey    string         `json:"searchKey,omitempty"`
	Filters      []OutlineField `json:"filters,omitempty"`
	Selection    string         `json:"selection,omitempty"`
	Maximum      int            `json:"maximum,omitempty"`
	Paging       bool           `json:"paging,omitempty"`
	Refreshable  bool           `json:"refreshable,omitempty"`
	Empty        bool           `json:"empty,omitempty"`
}

// OutlineTab is a tab's ID, registered literal root and icon, without label.
type OutlineTab struct {
	ID    string `json:"id"`
	Route string `json:"route"`
	Icon  Icon   `json:"icon,omitempty"`
}

// OutlineComponent counts compiled components by registered extension name.
type OutlineComponent struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Outline reduces a screen to its structure. Pass a screen returned by
// App.Render or a Runtime reply. Action IDs, extension names and
// destinations that the app did not register are dropped, so a malformed
// screen cannot smuggle values through them. Field and list keys are author
// tokens and are reported as written.
func Outline(app *App, screen Screen) ScreenOutline {
	outline := ScreenOutline{Protected: screen.Protection != nil, Live: screen.Live != nil}
	var registered Route
	if app != nil {
		if route, _, err := app.resolve(screen.Route, nil); err == nil {
			registered, outline.Route = route, route.Path
		}
	}
	switch {
	case screen.List != nil:
		outline.Content = "list"
	case screen.Form != nil:
		outline.Content = "form"
	case screen.FilePreview != nil:
		outline.Content = "file-preview"
	case screen.Text != "":
		outline.Content = "text"
	case len(screen.Components) != 0:
		outline.Content = "components"
	default:
		outline.Content = "none"
	}
	for _, requirement := range screen.Requires {
		if app.declaredCapability(requirement.Capability) {
			outline.Requires = append(outline.Requires, requirement)
		}
	}
	for _, action := range screen.Actions {
		if registered.Actions[action.ID] != nil {
			outline.Actions = append(outline.Actions, OutlineAction{ID: action.ID, Icon: knownIcon(action.Icon), Leading: action.Leading, Primary: action.Primary, Commit: action.Commit})
		}
	}
	destinations := map[string]bool{}
	addDestination := func(destination string) {
		if app == nil || destination == "" {
			return
		}
		if pattern, ok := app.RoutePattern(destination); ok {
			destinations[pattern] = true
		}
	}
	if screen.Form != nil {
		outline.FormStyle = screen.Form.Style
		outline.Draft = screen.Form.Draft != nil
		outline.Fields = outlineFields(screen.Form.Fields)
	}
	if screen.List != nil {
		list := &OutlineList{Rows: len(screen.List.Rows), SearchKey: screen.List.SearchKey, Filters: outlineFields(screen.List.Filters),
			Paging: screen.List.Paging != nil, Refreshable: screen.List.Refreshable, Empty: screen.List.Empty != ""}
		if screen.List.Selection != nil {
			list.Selection, list.Maximum = screen.List.Selection.Key, screen.List.Selection.Maximum
		}
		rowActions := map[string]bool{}
		for _, row := range screen.List.Rows {
			if row.Disabled {
				list.DisabledRows++
			}
			for _, action := range append(append([]RowAction(nil), row.LeadingActions...), row.TrailingActions...) {
				if registered.Actions[action.ID] != nil {
					rowActions[action.ID] = true
				}
			}
			addDestination(row.Destination)
		}
		list.RowActions = optionalKeys(rowActions)
		outline.List = list
	}
	for _, tab := range screen.Tabs {
		if app == nil {
			continue
		}
		if pattern, ok := app.RoutePattern(tab.Route); ok && pattern == tab.Route {
			outline.Tabs = append(outline.Tabs, OutlineTab{ID: tab.ID, Route: tab.Route, Icon: knownIcon(tab.Icon)})
			if tab.ID == screen.ActiveTab {
				outline.ActiveTab = tab.ID
			}
		}
	}
	counts := map[string]int{}
	for _, component := range screen.Components {
		if app.declaredCapability(Capability(component.Name)) {
			counts[component.Name]++
		}
		for _, destination := range component.Destinations {
			addDestination(destination)
		}
	}
	for _, name := range sortedKeys(counts) {
		outline.Components = append(outline.Components, OutlineComponent{Name: name, Count: counts[name]})
	}
	outline.Images, outline.Attachments = len(screen.Images), len(screen.Attachments)
	for _, image := range screen.Images {
		addDestination(image.Destination)
	}
	for _, attachment := range screen.Attachments {
		addDestination(attachment.Destination)
	}
	if screen.FilePreview != nil {
		addDestination(screen.FilePreview.File.Destination)
	}
	outline.Destinations = optionalKeys(destinations)
	return outline
}

func outlineFields(fields []Field) []OutlineField {
	if len(fields) == 0 {
		return nil
	}
	outlined := make([]OutlineField, 0, len(fields))
	for _, field := range fields {
		outlined = append(outlined, OutlineField{Key: field.Key, Kind: field.Kind, Style: field.Style, Required: field.Required, Disabled: field.Disabled, Options: len(field.Options)})
	}
	return outlined
}

func knownIcon(icon Icon) Icon {
	switch icon {
	case IconCompose, IconInfo, IconShare, IconFile, IconPhoto, IconAudio, IconNotes, IconSettings:
		return icon
	}
	return ""
}

// declaredCapability accepts the built-in names and this app's compiled
// extension capabilities, the only names diagnostics may echo.
func (app *App) declaredCapability(capability Capability) bool {
	if knownCapability(capability) {
		return true
	}
	return app != nil && app.extensions.Allows(string(capability))
}

type contractKey struct{}

// RendererContract reports the contract negotiated for the renderer
// connection that is rendering or dispatching. ok is false outside Runtime,
// for example in a direct App.Render or the legacy Render/Dispatch exports.
// "available" means the renderer build advertised the capability; it is not
// proof that the device or OS will complete the operation.
func RendererContract(ctx context.Context) (Contract, bool) {
	contract, ok := ctx.Value(contractKey{}).(*Contract)
	if !ok || contract == nil {
		return Contract{}, false
	}
	return Contract{Version: contract.Version, Renderer: contract.Renderer, Capabilities: maps.Clone(contract.Capabilities)}, true
}

func withRendererContract(ctx context.Context, contract *Contract) context.Context {
	return context.WithValue(ctx, contractKey{}, contract)
}
