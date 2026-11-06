package nativeui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/extension"
)

var (
	ErrInvalidRoute   = errors.New("nativeui: invalid local route")
	ErrScreenNotFound = errors.New("nativeui: screen not found")
	ErrUnknownAction  = errors.New("nativeui: unknown action")
	ErrInvalidValues  = errors.New("nativeui: invalid field values")
)

// App is an immutable route registry shared by native renderers.
type App struct {
	root       string
	routes     []Route
	extensions *extension.Registry
}

// New validates a screen registry. Register literal routes before parameterized
// routes when their patterns overlap. No presentation code is downloaded.
func New(root string, routes ...Route) (*App, error) {
	return NewWithExtensions(root, nil, routes...)
}

// NewWithExtensions explicitly enables compiled acquisition and rendering adapters.
func NewWithExtensions(root string, definitions []extension.Definition, routes ...Route) (*App, error) {
	extensions, err := extension.New(definitions...)
	if err != nil {
		return nil, err
	}
	for _, definition := range extensions.Definitions() {
		for _, capability := range definition.Manifest.Capabilities {
			if knownCapability(Capability(capability)) && (definition.Manifest.Kind == extension.ComponentKind || capability != string(CapabilityPhotos) && capability != string(CapabilityCamera)) {
				return nil, fmt.Errorf("nativeui: extension cannot replace core capability %q", capability)
			}
		}
	}
	app := &App{root: root, routes: append([]Route(nil), routes...), extensions: extensions}
	seen := make(map[string]bool)
	for index, route := range app.routes {
		if !validRoute(route.Path) || route.Screen == nil || seen[route.Path] {
			return nil, fmt.Errorf("nativeui: invalid or duplicate screen %q", route.Path)
		}
		seen[route.Path] = true
		actions := make(map[string]Handle, len(route.Actions))
		for name, handle := range route.Actions {
			if !behavior.SafeToken(name) || handle == nil {
				return nil, fmt.Errorf("nativeui: invalid action in %q", route.Path)
			}
			actions[name] = handle
		}
		app.routes[index].Actions = actions
		presentations := make(map[string]Respond, len(route.Presentations))
		for name, respond := range route.Presentations {
			if name == "" || len(name) > 128 || respond == nil {
				return nil, fmt.Errorf("nativeui: invalid presentation continuation in %q", route.Path)
			}
			presentations[name] = respond
		}
		app.routes[index].Presentations = presentations
		fileHandlers := make(map[string]FileRespond, len(route.Files))
		for name, respond := range route.Files {
			if name == "" || len(name) > 128 || respond == nil {
				return nil, fmt.Errorf("nativeui: invalid file continuation in %q", route.Path)
			}
			fileHandlers[name] = respond
		}
		app.routes[index].Files = fileHandlers
		services := make(map[string]ServiceRespond, len(route.Services))
		for name, respond := range route.Services {
			if !behavior.SafeToken(name) || respond == nil {
				return nil, fmt.Errorf("nativeui: invalid service continuation in %q", route.Path)
			}
			services[name] = respond
		}
		app.routes[index].Services = services
	}
	if _, _, err := app.resolve(root, nil); err != nil {
		return nil, err
	}
	return app, nil
}

// Root returns the initial route for a native host.
func (app *App) Root() string { return app.root }

// Extensions returns detached definitions for compiled renderer construction.
func (app *App) Extensions() []extension.Definition { return app.extensions.Definitions() }

// Routes returns registration-ordered metadata, with sorted action IDs.
// Mutating the returned slices does not change the immutable registry.
func (app *App) Routes() []RouteInfo {
	routes := make([]RouteInfo, 0, len(app.routes))
	for _, route := range app.routes {
		routes = append(routes, routeInfo(route))
	}
	return routes
}

// Resolve validates a local destination without invoking application code.
func (app *App) Resolve(destination string) (RouteInfo, error) {
	route, _, err := app.resolve(destination, nil)
	if err != nil {
		return RouteInfo{}, err
	}
	return routeInfo(route), nil
}

func routeInfo(route Route) RouteInfo {
	info := RouteInfo{Path: route.Path, Actions: make([]string, 0, len(route.Actions))}
	for action := range route.Actions {
		info.Actions = append(info.Actions, action)
	}
	sort.Strings(info.Actions)
	return info
}

// RenderJSON resolves a route and encodes its Go-authored screen for gomobile.
func (app *App) RenderJSON(ctx context.Context, destination, valuesJSON string) (string, error) {
	values, err := decodeValues(valuesJSON)
	if err != nil {
		return "", err
	}
	screen, err := app.Render(ctx, destination, values)
	if err != nil {
		return "", err
	}
	return encode(Response{Version: 1, Screen: &screen})
}

// DispatchJSON invokes only registered Go actions and encodes their effects.
// Business validation and authorization remain inside the application service.
func (app *App) DispatchJSON(ctx context.Context, destination, action, valuesJSON string) (string, error) {
	values, err := decodeValues(valuesJSON)
	if err != nil {
		return "", err
	}
	response, err := app.Dispatch(ctx, destination, action, values)
	if err != nil {
		return "", err
	}
	return encode(response)
}

// Dispatch invokes a registered action directly and validates its effects.
// Errors preserve the application's original wrapping chain.
func (app *App) Dispatch(ctx context.Context, destination, action string, values map[string]string) (Response, error) {
	return app.dispatch(ctx, destination, action, values, nil)
}

func (app *App) dispatch(ctx context.Context, destination, action string, values map[string]string, previous *Screen) (Response, error) {
	route, request, err := app.resolve(destination, values)
	if err != nil {
		return Response{}, err
	}
	handle := route.Actions[action]
	if handle == nil {
		if previous != nil {
			return Response{}, unavailableAction(action)
		}
		return Response{}, fmt.Errorf("%w %q", ErrUnknownAction, action)
	}
	screen, request, err := app.actionRequest(ctx, destination, action, request.Values, previous)
	if err != nil {
		return Response{}, err
	}
	result, err := handle(ctx, request)
	if err != nil {
		return Response{}, err
	}
	if result.Saved != nil && screen.Form != nil {
		saved := *result.Saved
		saved.Values = map[string]string(editorValues(screen, request.Values))
		for key, value := range result.Saved.Values {
			saved.Values[key] = value
		}
		result.Saved = &saved
	}
	return app.result(ctx, route, result)
}

func (app *App) actionRequest(ctx context.Context, destination, action string, values map[string]string, previous *Screen) (Screen, Request, error) {
	_, request, err := app.resolve(destination, values)
	if err != nil {
		return Screen{}, Request{}, err
	}
	var query map[string]string
	if previous != nil {
		query = authorizedQuery(*previous)
	}
	screen, err := app.Render(ctx, destination, query)
	if err != nil {
		return Screen{}, Request{}, err
	}
	request.Values, _, err = submittedComponents(screen, action, request.Values)
	if previous != nil && errors.Is(err, ErrUnknownAction) {
		err = unavailableAction(action)
	}
	if err == nil {
		_, request, err = app.resolve(destination, request.Values)
	}
	return screen, request, err
}

func unavailableAction(action string) *ActionError {
	return &ActionError{Code: "forbidden", Message: "This action is no longer available.", Fields: map[string]string{"action": "Choose an action on the current screen."}, Err: fmt.Errorf("%w %q", ErrUnknownAction, action)}
}

func (app *App) result(ctx context.Context, route Route, result Result) (Response, error) {
	response := Response{Version: 1, Result: result}
	if result.Service != nil {
		if err := result.Service.Validate(); err != nil {
			return Response{}, err
		}
		if route.Services[result.Service.ID] == nil {
			return Response{}, errors.New("nativeui: service continuation is not registered")
		}
		if result.Files != nil || result.Presentation != nil || result.Navigation != nil || result.Share != "" {
			return Response{}, errors.New("nativeui: a service cannot compete with another foreground effect")
		}
		if result.Service.Notification != nil && result.Service.Notification.Route != "" {
			if _, err := app.Resolve(result.Service.Notification.Route); err != nil {
				return Response{}, err
			}
		}
		response.Service = cloneService(result.Service)
	}
	if result.Files != nil {
		if err := validateFiles(*result.Files, route.Files, app.extensions); err != nil {
			return Response{}, err
		}
		if result.Presentation != nil || result.Navigation != nil || result.Share != "" {
			return Response{}, errors.New("nativeui: file operations cannot compete with other presentations")
		}
		if result.Files.Extension != nil {
			frozen, operation := *result.Files, *result.Files.Extension
			frozen.Extension = &operation
			response.Files = &frozen
		}
	}
	if result.Presentation != nil {
		if err := validatePresentation(*result.Presentation, route.Presentations); err != nil {
			return Response{}, err
		}
		if result.Navigation != nil || result.Share != "" {
			return Response{}, errors.New("nativeui: a presentation cannot compete with navigation or sharing")
		}
	}
	if result.Saved != nil {
		if _, err := app.Resolve(result.Saved.Route); err != nil {
			return Response{}, err
		}
		if _, _, err := app.resolve(result.Saved.Route, result.Saved.Values); err != nil {
			return Response{}, err
		}
	}
	if result.Navigation != nil {
		switch result.Navigation.Kind {
		case "push", "sheet":
			screen, err := app.Render(ctx, result.Navigation.Route, nil)
			if err != nil {
				return Response{}, err
			}
			response.Screen = &screen
		case "pop", "dismiss":
			if result.Navigation.Route != "" {
				if _, err := app.Resolve(result.Navigation.Route); err != nil {
					return Response{}, err
				}
			}
		default:
			return Response{}, errors.New("nativeui: unsupported navigation")
		}
	}
	return response, nil
}

// Render resolves and validates a screen without a JSON encode/decode boundary.
func (app *App) Render(ctx context.Context, destination string, values map[string]string) (Screen, error) {
	route, request, err := app.resolve(destination, values)
	if err != nil {
		return Screen{}, err
	}
	screen, err := route.Screen(ctx, request)
	if err != nil {
		return Screen{}, err
	}
	screen.Route, screen.Values = request.Route, request.Values
	if screen.Protection != nil {
		if screen.Protection.LockRoute == screen.Route {
			return Screen{}, errors.New("nativeui: a protected screen needs a separate lock route")
		}
		if _, err := app.Resolve(screen.Protection.LockRoute); err != nil {
			return Screen{}, err
		}
		if screen.Form != nil && screen.Form.Draft != nil {
			return Screen{}, errors.New("nativeui: protected forms cannot persist drafts")
		}
		screen.Requires = append(append([]Requirement(nil), screen.Requires...), Requirement{Capability: CapabilityPrivacy})
	}
	if screen.Live != nil && (route.Watch == nil || !validDataVersion(screen.Live.Version)) {
		return Screen{}, errors.New("nativeui: a live screen requires a bounded version and watch handler")
	}
	if _, exists := request.Values["row"]; exists {
		return Screen{}, invalidComponents(map[string]string{"row": "Row IDs may only be submitted to a declared row action."})
	}
	if screen.List != nil && screen.List.Rows == nil {
		list := *screen.List
		list.Rows = []Row{}
		screen.List = &list
	}
	if err := validateScreen(screen, route.Actions); err != nil {
		return Screen{}, err
	}
	if screen.List != nil {
		for _, row := range screen.List.Rows {
			if row.Destination == "" && (row.Disabled || screen.List.Selection != nil || len(row.LeadingActions)+len(row.TrailingActions) != 0) {
				continue
			}
			if _, err := app.Resolve(row.Destination); err != nil {
				return Screen{}, fmt.Errorf("nativeui: invalid row destination: %w", err)
			}
		}
	}
	if err := app.validateRenderingComponents(&screen); err != nil {
		return Screen{}, err
	}
	if err := app.validateTabs(screen); err != nil {
		return Screen{}, err
	}
	for _, preview := range screen.Images {
		if preview.Destination != "" {
			if _, err := app.Resolve(preview.Destination); err != nil {
				return Screen{}, fmt.Errorf("nativeui: invalid image destination: %w", err)
			}
		}
	}
	for _, attachment := range screen.Attachments {
		if _, err := app.Resolve(attachment.Destination); err != nil {
			return Screen{}, fmt.Errorf("nativeui: invalid attachment destination: %w", err)
		}
	}
	if screen.FilePreview != nil {
		if _, err := app.Resolve(screen.FilePreview.File.Destination); err != nil {
			return Screen{}, fmt.Errorf("nativeui: invalid file preview destination: %w", err)
		}
	}
	if err := componentValues(&screen); err != nil {
		return Screen{}, err
	}
	if _, _, err := app.resolve(destination, screen.Values); err != nil {
		return Screen{}, err
	}
	return screen, nil
}

func (app *App) resolve(destination string, values map[string]string) (Route, Request, error) {
	parsed, err := url.ParseRequestURI(destination)
	if err != nil || len(destination) > 4096 || parsed.IsAbs() || parsed.Host != "" || !validRoute(parsed.Path) || strings.Contains(destination, "#") {
		return Route{}, Request{}, ErrInvalidRoute
	}
	merged := make(map[string]string)
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return Route{}, Request{}, ErrInvalidRoute
	}
	for key, items := range query {
		merged[key] = items[len(items)-1]
	}
	for key, value := range values {
		merged[key] = value
	}
	size := 0
	for key, value := range merged {
		if !behavior.SafeToken(key) {
			return Route{}, Request{}, ErrInvalidValues
		}
		size += len(key) + len(value)
	}
	if len(merged) > 128 || size > 8<<20 {
		return Route{}, Request{}, ErrInvalidValues
	}
	for _, route := range app.routes {
		params, matches := match(route.Path, parsed.Path)
		if matches {
			return route, Request{Route: destination, Params: params, Values: merged}, nil
		}
	}
	return Route{}, Request{}, ErrScreenNotFound
}

func validRoute(route string) bool {
	if !strings.HasPrefix(route, "/") || strings.ContainsAny(route, "?#\\") || strings.Contains(route, "//") || strings.IndexFunc(route, unicode.IsControl) >= 0 {
		return false
	}
	for _, segment := range strings.Split(route, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func match(pattern, destination string) (map[string]string, bool) {
	expected, actual := strings.Split(pattern, "/"), strings.Split(destination, "/")
	if len(expected) != len(actual) {
		return nil, false
	}
	params := make(map[string]string)
	for index, segment := range expected {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") && len(segment) > 2 {
			if actual[index] == "" {
				return nil, false
			}
			params[segment[1:len(segment)-1]] = actual[index]
		} else if segment != actual[index] {
			return nil, false
		}
	}
	return params, true
}

func decodeValues(payload string) (map[string]string, error) {
	if payload == "" {
		return nil, nil
	}
	if len(payload) > 8<<20 || !strings.HasPrefix(strings.TrimSpace(payload), "{") {
		return nil, ErrInvalidValues
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(payload), &values); err != nil || len(values) > 128 {
		return nil, ErrInvalidValues
	}
	return values, nil
}

func validateScreen(screen Screen, actions map[string]Handle) error {
	if screen.List != nil && len(screen.Attachments) != 0 {
		return errors.New("nativeui: file attachments require form or text content")
	}
	if err := validateAttachments(screen.Attachments); err != nil {
		return err
	}
	if screen.List != nil && len(screen.Images) != 0 {
		return errors.New("nativeui: image previews require form or text content")
	}
	if err := validateImages(screen.Images); err != nil {
		return err
	}
	content := 0
	if screen.List != nil {
		content++
		if err := validateList(*screen.List, actions); err != nil {
			return err
		}
	}
	if screen.Form != nil {
		content++
		if screen.Form.Style != "" && screen.Form.Style != "document" {
			return errors.New("nativeui: unsupported form style")
		}
		keys := make(map[string]bool)
		if err := validateFields(screen.Form.Fields, keys); err != nil {
			return err
		}
		if len(keys) == 0 {
			return errors.New("nativeui: empty form")
		}
	}
	if screen.Text != "" {
		content++
	}
	if screen.FilePreview != nil {
		content++
		if err := validateFilePreview(*screen.FilePreview); err != nil {
			return err
		}
	}
	if content == 0 && len(screen.Components) != 0 {
		content++
	}
	if content != 1 || screen.Title == "" {
		return errors.New("nativeui: screen requires a title and one content kind")
	}
	seen := make(map[string]bool)
	for _, action := range screen.Actions {
		if !behavior.SafeToken(action.ID) || action.Label == "" || actions[action.ID] == nil || seen[action.ID] {
			return errors.New("nativeui: undeclared or duplicate screen action")
		}
		if action.Icon != "" && action.Icon != IconCompose && action.Icon != IconInfo && action.Icon != IconShare {
			return errors.New("nativeui: unsupported action icon")
		}
		seen[action.ID] = true
	}
	return nil
}

func encode(response Response) (string, error) {
	encoded, err := json.Marshal(response)
	return string(encoded), err
}
