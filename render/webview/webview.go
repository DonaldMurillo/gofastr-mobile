// Package webview renders shared nativeui definitions through GoFastr's SSR,
// client navigator, and mobileui design system. Applications own no web assets.
package webview

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/webassets"

	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// ActionPath is the fixed, same-origin POST endpoint for shared-screen actions.
// Mount Renderer.Actions() here before mounting Renderer.UIHost().
const ActionPath = "/_mobile/actions"

//go:embed webview.css
var stylesheet string

//go:embed webview.js
var behavior string

//go:embed presentation.js
var presentationAdapter string

//go:embed files.js
var filesAdapter string

//go:embed components.js
var componentsAdapter string

//go:embed rendering.js
var renderingAdapter string

//go:embed foreground.js
var foregroundAdapter string

//go:embed services.js
var servicesAdapter string

//go:embed live.js
var liveAdapter string

//go:embed lifecycle.js
var lifecycleAdapter string

var rendererStyle = registry.RegisterStyle("gfm-webview", func(style.Theme) string { return stylesheet })
var rendererForeground = registry.RegisterBehavior("gfm-webview-foreground", foregroundAdapter, registry.Markers("[data-gfw-screen]"))
var rendererServices = registry.RegisterBehavior("gfm-webview-services", servicesAdapter, registry.Markers("[data-gfw-screen]"), registry.Requires("gfm-webview-foreground"))
var rendererLive = registry.RegisterBehavior("gfm-webview-live", liveAdapter, registry.Markers("[data-gfw-screen]"), registry.Requires("gfm-webview-components"))
var rendererLifecycle = registry.RegisterBehavior("gfm-webview-lifecycle", lifecycleAdapter, registry.Markers("[data-gfw-screen]"), registry.Requires(headless.LeaveGuardBehaviorName))
var rendererComponents = registry.RegisterBehavior("gfm-webview-components", componentsAdapter, registry.Markers("[data-gfw-screen]"))
var rendererRendering = registry.RegisterBehavior("gfm-webview-rendering", renderingAdapter, registry.Markers("[data-gfw-screen]"))
var rendererFiles = registry.RegisterBehavior("gfm-webview-files", filesAdapter, registry.Markers("[data-gfw-screen]"),
	registry.Requires("gfm-webview-foreground"))
var rendererPresentation = registry.RegisterBehavior("gfm-webview-presentation", presentationAdapter, registry.Markers("[data-gfw-screen]"), registry.Requires("gfm-webview-foreground"))
var rendererBehavior = registry.RegisterBehavior("gfm-webview", behavior, registry.Markers("[data-gfw-screen]"),
	registry.Requires(headless.LeaveGuardBehaviorName, "gfm-webview-presentation", "gfm-webview-files", "gfm-webview-components", "gfm-webview-rendering", "gfm-webview-services", "gfm-webview-live", "gfm-webview-lifecycle"),
	registry.Interactions(registry.Interaction{Event: "click", Selector: "[data-gfw-screen] [form=\"gfw-form\"]"}))

// Renderer adapts an immutable registry to the existing GoFastr web host.
type Renderer struct {
	screens             *nativeui.App
	platform            mobileui.Platform
	host                *uihost.UIHost
	runtime             *nativeui.Runtime
	extensions          []string
	componentExtensions []string
}

// New creates an SSR host without mounting routes or invoking screen handlers.
// The root document '/' renders screens.Root(), including its initial query.
// Navigation sheets use ordinary pages for compatibility; shared presentations
// use modal overlays. Navigation dismiss/pop use GoFastr's existing history.
func New(screens *nativeui.App, platform mobileui.Platform, options ...nativeui.RuntimeOptions) (*Renderer, error) {
	if screens == nil {
		return nil, errors.New("webview: screens are required")
	}
	if err := platform.Validate(); err != nil {
		return nil, err
	}
	if len(options) > 1 {
		return nil, errors.New("webview: provide at most one runtime configuration")
	}
	configuration := nativeui.RuntimeOptions{}
	if len(options) == 1 {
		configuration = options[0]
	}
	runtime, err := nativeui.NewRuntime(screens, configuration)
	if err != nil {
		return nil, err
	}
	renderer := &Renderer{screens: screens, platform: platform, runtime: runtime}
	for _, definition := range screens.Extensions() {
		name, err := webassets.Module(definition)
		if err != nil {
			return nil, err
		}
		renderer.extensions = append(renderer.extensions, name)
		if definition.Manifest.Kind == extension.ComponentKind {
			renderer.componentExtensions = append(renderer.componentExtensions, definition.Manifest.Name)
		}
	}
	site := appui.NewApp("Mobile")
	site.WithTheme(mobileui.Theme(platform))
	for _, route := range screens.Routes() {
		if route.Path == ActionPath || strings.HasPrefix(route.Path, "/__gofastr/") {
			return nil, errors.New("webview: screen conflicts with a library endpoint")
		}
		if route.Path != "/" {
			site.Register(route.Path, &pageScreen{renderer: renderer}, nil)
		}
	}
	site.Register("/", &pageScreen{renderer: renderer}, nil)
	renderer.host = uihost.New(site)
	return renderer, nil
}

// UIHost returns the ordinary GoFastr host to pass to framework.App.Mount.
func (renderer *Renderer) UIHost() *uihost.UIHost { return renderer.host }

// Actions returns the bounded POST handler to mount at ActionPath.
// Authentication and CSRF middleware remain the application's responsibility.
func (renderer *Renderer) Actions() http.Handler { return http.HandlerFunc(renderer.serveAction) }

type pageScreen struct {
	component.ContextOnly
	renderer   *Renderer
	definition *nativeui.Screen
	failure    *nativeui.ActionError
	effects    *nativeui.Result
	err        error
}

func (*pageScreen) SetParams(map[string]string) {}

func (screen *pageScreen) Load(ctx context.Context) error {
	if screen.definition != nil {
		return nil
	}
	destination := screen.renderer.screens.Root()
	if request := appui.RequestFromContext(ctx); request != nil {
		destination = request.URL.RequestURI()
		if request.URL.Path == "/" && screen.renderer.screens.Root() != "/" {
			root, _ := url.ParseRequestURI(screen.renderer.screens.Root())
			query := root.Query()
			for key, values := range request.URL.Query() {
				query[key] = values
			}
			root.RawQuery = query.Encode()
			destination = root.RequestURI()
		}
	}
	definition, err := screen.renderer.screens.Render(ctx, destination, nil)
	screen.definition, screen.err = &definition, err
	return nil
}

func (screen *pageScreen) ScreenTitle(context.Context) string {
	if screen.definition != nil && screen.definition.Title != "" {
		return screen.definition.Title
	}
	return "Mobile"
}

func (screen *pageScreen) RenderCtx(ctx context.Context) render.HTML {
	if screen.err != nil || screen.definition == nil {
		status, failure := actionFailure(screen.err)
		appui.SetStatus(ctx, status)
		return mobileui.Screen(mobileui.Page{Platform: screen.renderer.platform, Title: "Screen unavailable"},
			mobileui.Empty("Could not open this screen", failure.Message, mobileui.Action{Label: "Back", Href: screen.renderer.screens.Root()}))
	}
	return screen.renderer.markup(ctx, *screen.definition, screen.failure, screen.effects)
}

type clientConfig struct {
	Extensions          []string              `json:"extensions,omitempty"`
	ComponentExtensions []string              `json:"componentExtensions,omitempty"`
	Components          []extension.Component `json:"components,omitempty"`
	DefaultAction       string                `json:"defaultAction,omitempty"`
	Committed           bool                  `json:"committed,omitempty"`
	Root                string                `json:"root"`
	Route               string                `json:"route"`
	Routes              []nativeui.RouteInfo  `json:"routes"`
	Draft               *nativeui.Draft       `json:"draft,omitempty"`
	List                *nativeui.List        `json:"list,omitempty"`
	Tabs                []nativeui.Tab        `json:"tabs,omitempty"`
	ActiveTab           string                `json:"activeTab,omitempty"`
	Live                *nativeui.Live        `json:"live,omitempty"`
}

func (renderer *Renderer) markup(ctx context.Context, screen nativeui.Screen, failure *nativeui.ActionError, effects *nativeui.Result) render.HTML {
	page := mobileui.Page{Platform: renderer.platform, Title: screen.Title, Subtitle: screen.Prompt, Editor: !screen.LargeTitle}
	var images, attachments render.HTML
	if len(screen.Components) == 0 || len(screen.Images) != 0 {
		images = imageGallery(screen.Images)
	}
	if len(screen.Components) == 0 || len(screen.Attachments) != 0 {
		attachments = attachmentList(screen.Attachments)
	}
	tabRoot := false
	for _, tab := range screen.Tabs {
		if strings.Split(screen.Route, "?")[0] == strings.Split(tab.Route, "?")[0] {
			tabRoot = true
			break
		}
	}
	if screen.Route != renderer.screens.Root() && !tabRoot {
		page.BackHref, page.BackLabel = renderer.screens.Root(), "Back"
		if screen.Form != nil && screen.Form.Draft != nil && screen.Form.Draft.BackLabel != "" {
			page.BackLabel = screen.Form.Draft.BackLabel
		}
	}
	primary := -1
	for index, action := range screen.Actions {
		if action.Primary {
			primary = index
			break
		}
	}
	if primary < 0 {
		for index, action := range screen.Actions {
			if !action.Leading && !collectionAction(screen, action) {
				primary = index
				break
			}
		}
	}
	var overflow []nativeui.Action
	for index, action := range screen.Actions {
		if collectionAction(screen, action) {
			continue
		}
		endpoint := ActionPath + "?" + url.Values{"route": {screen.Route}, "action": {action.ID}}.Encode()
		button := mobileui.Action{Label: action.Label, Icon: string(action.Icon), Form: "gfw-form", Value: action.ID, FormAction: endpoint}
		switch {
		case action.Leading && len(page.Leading) == 0:
			page.Leading = append(page.Leading, button)
		case index == primary:
			page.Primary = button
		default:
			overflow = append(overflow, action)
		}
	}
	config := clientConfig{Root: renderer.screens.Root(), Route: screen.Route, Routes: renderer.screens.Routes(),
		List: screen.List, Tabs: screen.Tabs, ActiveTab: screen.ActiveTab, Live: screen.Live, Extensions: renderer.extensions,
		ComponentExtensions: renderer.componentExtensions, Components: screen.Components}
	if primary >= 0 {
		config.DefaultAction = screen.Actions[primary].ID
	}
	if effects != nil {
		config.Committed = effects.ClearDraft
	}
	controls := []render.HTML{}
	keys := make([]string, 0, len(screen.Values))
	for key := range screen.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fieldKeys := make(map[string]bool)
	if screen.Form != nil {
		config.Draft = screen.Form.Draft
		for _, field := range screen.Form.Fields {
			fieldKeys[field.Key] = true
		}
	}
	for _, key := range keys {
		if !fieldKeys[key] {
			controls = append(controls, valueInput(key, screen.Values[key], "hidden"))
		}
	}
	if token := middleware.TokenFromContext(ctx); token != "" {
		controls = append(controls, hidden("_csrf", middleware.MaskCSRFToken(token)))
	}
	status := ""
	details := []render.HTML{}
	detailAttrs := map[string]string{"class": "gfw-disclosure gfw-document-details", "data-gfw-details": ""}
	if screen.Form != nil {
		status = screen.Form.Status
		for index, field := range screen.Form.Fields {
			identifier := "gfw-field-" + strconv.Itoa(index)
			message := ""
			if failure != nil {
				message = failure.Fields[field.Key]
			}
			control := fieldControl(field, identifier, "data-gfw-field", message)
			if screen.Form.Style == "document" && field.Style == "detail" {
				details = append(details, control)
				if message != "" {
					detailAttrs["open"] = ""
				}
			} else {
				controls = append(controls, control)
			}
		}
	}
	if failure != nil {
		status = failure.Message
	}
	shareText := ""
	if effects != nil {
		messages := []string{}
		if effects.Announcement != "" {
			messages = append(messages, effects.Announcement)
		}
		if effects.Haptic != "" {
			messages = append(messages, "Haptic feedback requires a supported browser or native host.")
		}
		if effects.Share != "" {
			shareText = effects.Share
			messages = append(messages, "System sharing requires JavaScript. Select and copy the share text below.")
		}
		if len(messages) != 0 {
			status = strings.Join(messages, " ")
		}
	}
	if screen.Form != nil {
		controls = append(controls, images, attachments)
		controls = append(controls, componentMarkup(screen.Components))
		if len(details) != 0 {
			heading := screen.Form.DetailsTitle
			if heading == "" {
				heading = "Details"
			}
			controls = append(controls, render.Tag("details", detailAttrs, render.Tag("summary", nil, render.Text(heading)), render.Tag("div", map[string]string{"class": "gfw-detail-fields"}, details...)))
		}
	}
	controls = append(controls, render.Tag("p", map[string]string{"class": "gfm-save-status", "data-gfw-status": "", "role": "status", "aria-live": "polite"}, render.Text(status)))
	formAttrs := map[string]string{"id": "gfw-form", "method": "post", "action": ActionPath, "data-gfw-form": ""}
	if screen.Form != nil {
		formAttrs["class"] = "gfm-document"
		formAttrs["data-gfw-form-style"] = screen.Form.Style
		formAttrs["data-hui-leave-guard"] = ""
		formAttrs["data-hui-leave-guard-message"] = "Your changes have not been saved. Leave this screen?"
		if screen.Form.Draft != nil {
			formAttrs["data-hui-leave-guard-message"] = screen.Form.Draft.Message + " Leave this screen and keep the local draft?"
			controls = append(controls, render.Tag("button", map[string]string{"type": "button", "class": "gfw-discard", "data-gfw-discard": ""}, render.Text(screen.Form.Draft.Discard)))
		}
	}
	if failure != nil {
		formAttrs["data-gfw-error"] = "true"
	}
	content := []render.HTML{headerOverflow(screen, overflow)}
	if page.Editor && screen.Prompt != "" {
		content = append(content, render.Tag("p", map[string]string{"class": "gfw-prompt"}, render.Text(screen.Prompt)))
	}
	if screen.List != nil {
		content = append(content, listMarkup(screen, failure))
	}
	if screen.Text != "" {
		content = append(content, images, attachments)
		content = append(content, render.Tag("div", map[string]string{"class": "gfw-text"}, render.Text(screen.Text)))
	}
	if preview := screen.FilePreview; preview != nil {
		header := render.Tag("header", map[string]string{"class": "gfw-file-heading"}, fileIcon(nativeui.FileIcon(preview.File.MediaType)),
			render.Tag("h1", nil, render.Text(preview.File.Name)),
			render.Tag("p", map[string]string{"class": "gfw-file-description"}, render.Text(nativeui.FileDescription(preview.File.MediaType, preview.File.Size))),
			render.Tag("p", map[string]string{"class": "gfw-file-caption"}, render.Text(preview.Caption)))
		parts := []render.HTML{header, images, attachments}
		if preview.Text != "" {
			parts = append(parts, render.Tag("section", map[string]string{"class": "gfw-file-text"},
				render.Tag("h2", nil, render.Text(preview.PreviewTitle)), render.Tag("pre", nil, render.Text(preview.Text))))
		}
		content = append(content, render.Tag("article", map[string]string{"class": "gfw-file-preview", "data-gfw-file-preview": ""}, parts...))
	}
	if screen.List != nil {
		content = append(content, images)
	}
	if screen.Form == nil {
		content = append(content, componentMarkup(screen.Components))
	}
	shareAttrs := map[string]string{"class": "gfw-share", "data-gfw-share": ""}
	if shareText == "" {
		shareAttrs["hidden"] = ""
	} else {
		shareAttrs["open"] = ""
	}
	content = append(content, render.Tag("form", formAttrs, controls...),
		render.Tag("details", shareAttrs,
			render.Tag("summary", nil, render.Text("Share text · select and copy")),
			render.Tag("textarea", map[string]string{"readonly": "", "aria-label": "Text to share", "rows": "8"}, render.Text(shareText))))
	if len(screen.Tabs) != 0 {
		content = append(content, tabMarkup(screen))
	}
	encoded, _ := json.Marshal(config)
	return rendererStyle.WrapHTML(render.Tag("div", map[string]string{"data-gfw-screen": "", "data-gfw-config": string(encoded),
		"data-gfw-capabilities": `{"sheet":"full-page","presentation":"modal-overlay","share":"system-or-copy","haptic":"vibration-or-announcement","announcement":"aria-live","draft":"session-storage"}`}, mobileui.Screen(page, content...)))
}

func hidden(name, value string) render.HTML {
	return render.VoidTag("input", map[string]string{"type": "hidden", "name": name, "value": value})
}

func componentMarkup(components []extension.Component) render.HTML {
	children := make([]render.HTML, 0, len(components))
	for _, snapshot := range components {
		children = append(children, render.Tag("div", map[string]string{"data-gfw-component": snapshot.ID,
			"data-gfw-component-name": snapshot.Name}, render.Text("Loading component…")))
	}
	return render.Tag("div", map[string]string{"data-gfw-components": ""}, children...)
}

func attachmentList(attachments []nativeui.Attachment) render.HTML {
	attrs := map[string]string{"class": "gfw-attachments", "data-gfw-attachments": "", "aria-label": "File attachments"}
	if len(attachments) == 0 {
		attrs["hidden"] = ""
	}
	rows := make([]render.HTML, 0, len(attachments))
	for _, attachment := range attachments {
		parts := []render.HTML{
			render.Tag("a", map[string]string{"href": attachment.Destination, "data-gfw-attachment": attachment.ID},
				fileIcon(nativeui.FileIcon(attachment.MediaType)),
				render.Tag("div", map[string]string{"class": "gfw-file-copy"}, render.Tag("strong", nil, render.Text(attachment.Name)),
					render.Tag("span", nil, render.Text(nativeui.FileDescription(attachment.MediaType, attachment.Size)))))}
		if attachment.Audio != nil {
			parts = append(parts, render.Tag("p", map[string]string{"data-gfw-audio-pending": attachment.ID}, render.Text("Audio playback becomes available after this screen connects.")))
		}
		rows = append(rows, render.Tag("li", nil, parts...))
	}
	return render.Tag("section", attrs, render.Tag("h2", nil, render.Text("Attachments")), render.Tag("ul", nil, rows...))
}

func fileIcon(icon nativeui.Icon) render.HTML {
	path := "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8l-6-6Zm0 0v6h6M8 13h8M8 17h6"
	if icon == nativeui.IconPhoto {
		path = "M4 3h16v18H4zM4 16l5-5 4 4 3-3 4 4M16 7h.01"
	}
	if icon == nativeui.IconAudio {
		path = "M3 10v4m4-8v12m5-16v20m5-16v12m4-8v4"
	}
	return render.Tag("span", map[string]string{"class": "gfw-file-icon", "data-gfw-file-icon": string(icon), "aria-hidden": "true"},
		render.Tag("svg", map[string]string{"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.6", "stroke-linecap": "round", "stroke-linejoin": "round"}, render.Tag("path", map[string]string{"d": path})))
}

func imageGallery(images []nativeui.Image) render.HTML {
	attrs := map[string]string{"class": "gfw-images", "data-gfw-images": "", "aria-label": "Photos"}
	if len(images) == 0 {
		attrs["hidden"] = ""
	}
	children := []render.HTML{render.Tag("h2", nil, render.Text("Photos"))}
	for _, preview := range images {
		picture := render.VoidTag("img", map[string]string{
			"src": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(preview.Data), "alt": preview.Alt,
			"width": strconv.Itoa(preview.Width), "height": strconv.Itoa(preview.Height), "decoding": "async", "loading": "lazy",
		})
		itemAttrs := map[string]string{"data-gfw-image": preview.ID}
		kind := "figure"
		if preview.Destination != "" {
			kind, itemAttrs["href"] = "a", preview.Destination
			itemAttrs["aria-label"] = "Open photo: " + preview.Alt
		}
		children = append(children, render.Tag(kind, itemAttrs, picture))
	}
	return render.Tag("section", attrs, children...)
}

func valueInput(key, value, kind string) render.HTML {
	return render.VoidTag("input", map[string]string{"type": kind, "name": key, "value": value, "data-gfw-field": key})
}
