// Package mobileui is an experimental external mobile-web design system.
// It owns its markup, styles, and behavior; applications only compose it.
// It uses GoFastr's public registry, not framework/ui component overrides.
package mobileui

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// Platform selects presentation explicitly at app boot, never by user agent.
type Platform string

const (
	IOS     Platform = "ios"
	Android Platform = "android"
)

// Validate refuses unsupported presentation modes.
func (platform Platform) Validate() error {
	if platform != IOS && platform != Android {
		return fmt.Errorf("mobileui: unsupported platform %q", platform)
	}
	return nil
}

//go:embed mobile.css
var stylesheet string

//go:embed mobile.js
var behavior string

var mobileStyle = registry.RegisterStyle("gfm-mobile", func(style.Theme) string { return stylesheet })
var mobileBehavior = registry.RegisterBehavior("gfm-mobile", behavior, registry.Markers("[data-gfm-screen]"))

// Theme returns the presentation package's adaptive palette. Desktop defaults
// are not modified, and callers need no CSS to select this presentation.
func Theme(platform Platform) style.Theme {
	if err := platform.Validate(); err != nil {
		panic(err)
	}
	if platform == Android {
		return theme.Default(theme.Overrides{
			Background: "#fdf8ff", Surface: "#f3edf7", SurfaceSoft: "#ece6f0", Text: "#211f26",
			TextMuted: "#625d6b", Primary: "#6750a4", PrimaryFg: "#ffffff", Border: "#ded8e4",
			Dark: &theme.Overrides{Background: "#141218", Surface: "#211f26", SurfaceSoft: "#2b2930", Text: "#e6e0e9",
				TextMuted: "#cac4d0", Primary: "#d0bcff", PrimaryFg: "#381e72", Border: "#49454f"},
		})
	}
	return theme.Default(theme.Overrides{
		Background: "#f2f2f7", Surface: "#ffffff", SurfaceSoft: "#e8e8ed", Text: "#1c1c1e",
		TextMuted: "#66666c", Primary: "#a85e00", PrimaryFg: "#ffffff", Border: "#d9d9de",
		Dark: &theme.Overrides{Background: "#000000", Surface: "#1c1c1e", SurfaceSoft: "#2c2c2e", Text: "#f5f5f7",
			TextMuted: "#aeaeb2", Primary: "#ffcc68", PrimaryFg: "#382000", Border: "#38383a"},
	})
}

// Action is a link or a submit action associated with a named form.
type Action struct {
	Label, Href, Icon, Form string
}

// Page configures presentation chrome, not application routing semantics.
type Page struct {
	Platform            Platform
	Title, Subtitle     string
	BackHref, BackLabel string
	Primary             Action
	Editor              bool
}

// Screen renders mobile chrome around composed design-system components.
func Screen(page Page, children ...render.HTML) render.HTML {
	if err := page.Platform.Validate(); err != nil {
		panic(err)
	}
	var leading render.HTML
	if page.BackHref != "" {
		leading = render.Tag("a", map[string]string{"class": "gfm-back", "href": urlsafe.CleanAnchor(page.BackHref), "aria-label": page.BackLabel},
			icon("back"), render.Tag("span", nil, render.Text(page.BackLabel)))
	} else {
		leading = render.Tag("span", map[string]string{"class": "gfm-wordmark"}, icon("notebook"), render.Text("ON THIS DEVICE"))
	}
	var trailing render.HTML
	if page.Primary.Label != "" && (page.Platform == IOS || page.Editor) {
		trailing = action(page.Primary, "gfm-nav-action")
	}
	titleTag := "span"
	if page.Editor {
		titleTag = "h1"
	}
	bar := render.Tag("header", map[string]string{"class": "gfm-nav"}, leading,
		render.Tag(titleTag, map[string]string{"class": "gfm-nav-title"}, render.Text(editorTitle(page))), trailing)
	content := []render.HTML{}
	if !page.Editor {
		content = append(content, render.Tag("div", map[string]string{"class": "gfm-heading"},
			render.Tag("h1", nil, render.Text(page.Title)), render.Tag("p", nil, render.Text(page.Subtitle))))
	}
	content = append(content, children...)
	parts := []render.HTML{bar, render.Tag("div", map[string]string{"class": "gfm-content"}, content...)}
	if page.Platform == Android && !page.Editor && page.Primary.Label != "" {
		parts = append(parts, action(page.Primary, "gfm-fab"))
	}
	attrs := map[string]string{"class": "gfm-screen", "data-gfm-screen": "list", "data-gfm-platform": string(page.Platform)}
	if page.Editor {
		attrs["data-gfm-screen"] = "editor"
	}
	return mobileStyle.WrapHTML(render.Tag("section", attrs, parts...))
}

func editorTitle(page Page) string {
	if page.Editor {
		return page.Title
	}
	return ""
}

func action(config Action, class string) render.HTML {
	children := []render.HTML{icon(config.Icon), render.Tag("span", nil, render.Text(config.Label))}
	attrs := map[string]string{"class": class, "aria-label": config.Label}
	if config.Form != "" {
		attrs["type"], attrs["form"] = "submit", config.Form
		return render.Tag("button", attrs, children...)
	}
	attrs["href"] = urlsafe.CleanAnchor(config.Href)
	return render.Tag("a", attrs, children...)
}

// Search renders a labeled search field with a standard GET fallback.
func Search(value, actionURL string) render.HTML {
	return render.Tag("form", map[string]string{"class": "gfm-search", "role": "search", "method": "get", "action": urlsafe.CleanAnchor(actionURL)},
		icon("search"), render.Tag("label", map[string]string{"class": "gfm-sr", "for": "notes-search"}, render.Text("Search notes")),
		render.VoidTag("input", map[string]string{"type": "search", "id": "notes-search", "name": "q", "value": value,
			"placeholder": "Search your notes", "maxlength": "200", "enterkeyhint": "search"}),
		render.Tag("button", map[string]string{"type": "submit", "aria-label": "Search notes"}, icon("forward")))
}

// Row is a fully tappable list destination, with no nested interactive control.
type Row struct{ Title, Preview, Meta, Href string }

// List renders a section of task-shaped list rows.
func List(title, count string, rows []Row) render.HTML {
	items := make([]render.HTML, 0, len(rows))
	for _, row := range rows {
		items = append(items, render.Tag("li", nil, render.Tag("a", map[string]string{"class": "gfm-row", "href": urlsafe.CleanAnchor(row.Href)},
			render.Tag("div", map[string]string{"class": "gfm-row-copy"},
				render.Tag("h2", nil, render.Text(row.Title)),
				render.Tag("p", nil, render.Text(row.Preview)),
				render.Tag("span", map[string]string{"class": "gfm-row-meta"}, render.Text(row.Meta))), icon("forward"))))
	}
	return render.Tag("section", map[string]string{"class": "gfm-list-section", "aria-label": title},
		render.Tag("div", map[string]string{"class": "gfm-section-heading"},
			render.Tag("h2", nil, render.Text(title)), render.Tag("span", nil, render.Text(count))),
		render.Tag("ul", map[string]string{"class": "gfm-list"}, items...))
}

// Empty renders an empty, error, or no-results state using the same components.
func Empty(title, message string, primary Action) render.HTML {
	children := []render.HTML{icon("notebook"), render.Tag("h2", nil, render.Text(title)), render.Tag("p", nil, render.Text(message))}
	if primary.Label != "" {
		children = append(children, action(primary, "gfm-empty-action"))
	}
	return render.Tag("div", map[string]string{"class": "gfm-empty", "role": "status"}, children...)
}

// Footer renders low-priority, truthful persistence information.
func Footer(text string) render.HTML {
	return render.Tag("p", map[string]string{"class": "gfm-footer"}, icon("device"), render.Text(text))
}

// Editor configures native text inputs; application data is always escaped.
type Editor struct {
	ID, Action, Title, Body, Status, Error string
}

// DocumentEditor renders a labeled document form with progressively enhanced
// saves and per-note, session-only draft restoration.
func DocumentEditor(ctx context.Context, config Editor) render.HTML {
	status := config.Status
	if config.Error != "" {
		status = config.Error
	}
	children := []render.HTML{
		render.VoidTag("input", map[string]string{"type": "hidden", "name": "id", "value": config.ID}),
		render.Tag("label", map[string]string{"class": "gfm-sr", "for": "note-title"}, render.Text("Title")),
		render.VoidTag("input", map[string]string{"class": "gfm-document-title", "id": "note-title", "name": "title", "type": "text",
			"value": config.Title, "placeholder": "Title", "required": "", "maxlength": "200", "autocomplete": "off", "enterkeyhint": "next"}),
		render.Tag("label", map[string]string{"class": "gfm-sr", "for": "note-body"}, render.Text("Note")),
		render.Tag("textarea", map[string]string{"class": "gfm-document-body", "id": "note-body", "name": "body", "rows": "12",
			"placeholder": "Start writing…", "maxlength": "1048576"}, render.Text(config.Body)),
		render.Tag("p", map[string]string{"class": "gfm-save-status", "data-gfm-status": "", "role": "status", "aria-live": "polite"}, render.Text(status)),
	}
	if token := middleware.TokenFromContext(ctx); token != "" {
		children = append(children, render.VoidTag("input", map[string]string{"type": "hidden", "name": "_csrf", "value": middleware.MaskCSRFToken(token)}))
	}
	attrs := map[string]string{"id": "note-editor", "class": "gfm-document", "method": "post", "action": urlsafe.CleanAnchor(config.Action), "data-gfm-editor": config.ID}
	if config.Error != "" {
		attrs["data-gfm-error"] = "true"
	}
	return render.Tag("form", attrs, children...)
}

func icon(name string) render.HTML {
	paths := map[string]string{
		"back": "m14 5-7 7 7 7", "forward": "m9 5 7 7-7 7", "search": "m21 21-5-5 M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0",
		"compose": "M13 5H5v14h14v-8 M15 5l4-4 4 4-11 11-5 1 1-5 7-7", "add": "M12 5v14 M5 12h14",
		"notebook": "M6 3h13v18H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2 M8 3v18 M12 8h4 M12 12h4",
		"device":   "M8 2h8a1 1 0 0 1 1 1v18a1 1 0 0 1-1 1H8a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1 M10 18h4",
		"check":    "m5 12 4 4L19 6",
	}
	path, present := paths[name]
	if !present || strings.TrimSpace(name) == "" {
		return ""
	}
	return render.Tag("svg", map[string]string{"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8",
		"stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", "focusable": "false"},
		render.Tag("path", map[string]string{"d": path}))
}
