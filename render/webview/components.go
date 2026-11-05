package webview

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"

	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
	"github.com/DonaldMurillo/gofastr-mobile/nativeui"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func fieldControl(field nativeui.Field, identifier, marker, message string) render.HTML {
	attrs := map[string]string{"id": identifier, "name": field.Key, marker: field.Key, "class": "gfw-input",
		"placeholder": field.Placeholder, "autocomplete": "off"}
	if field.Style == "title" {
		attrs["class"] += " gfm-document-title"
	}
	if field.Required {
		attrs["aria-required"] = "true"
		if field.Kind != "toggle" {
			attrs["required"] = ""
		}
	}
	if field.Disabled {
		attrs["disabled"] = ""
	}
	if message != "" {
		attrs["aria-invalid"], attrs["aria-describedby"] = "true", identifier+"-error"
	}
	var input render.HTML
	switch field.Kind {
	case "multiline":
		attrs["rows"] = "12"
		input = render.Tag("textarea", attrs, render.Text(field.Value))
	case "select":
		options := []render.HTML{}
		hasEmpty := false
		for _, option := range field.Options {
			hasEmpty = hasEmpty || option.Value == ""
		}
		if !hasEmpty {
			placeholder := map[string]string{"value": ""}
			label := "None"
			if field.Required {
				placeholder["disabled"] = ""
				label = "Choose…"
			}
			if field.Value == "" {
				placeholder["selected"] = ""
			}
			options = append(options, render.Tag("option", placeholder, render.Text(label)))
		}
		for _, option := range field.Options {
			optionAttrs := map[string]string{"value": option.Value}
			if option.Value == field.Value {
				optionAttrs["selected"] = ""
			}
			if option.Disabled {
				optionAttrs["disabled"] = ""
			}
			options = append(options, render.Tag("option", optionAttrs, render.Text(option.Label)))
		}
		input = render.Tag("select", attrs, options...)
	default:
		attrs["type"], attrs["value"] = "text", field.Value
		switch field.Kind {
		case "toggle":
			attrs["type"], attrs["value"], attrs["data-gfw-initial"] = "checkbox", "true", field.Value
			if field.Value == "true" {
				attrs["checked"] = ""
			} else if field.Value == "" {
				attrs["data-gfw-optional-empty"] = "true"
			}
		case "date", "time":
			attrs["type"] = field.Kind
			if field.Kind == "time" {
				attrs["step"] = "60"
			}
		}
		input = render.VoidTag("input", attrs)
	}
	return render.Tag("div", map[string]string{"class": "gfw-field", "data-gfw-style": field.Style, "data-gfw-kind": field.Kind},
		render.Tag("label", map[string]string{"for": identifier}, render.Text(field.Label)), input,
		render.Tag("p", map[string]string{"id": identifier + "-error", marker + "-error": field.Key, "class": "gfw-field-error"}, render.Text(message)))
}

func collectionAction(screen nativeui.Screen, action nativeui.Action) bool {
	return screen.List != nil && screen.List.Selection != nil && action.Commit && !action.Primary && !action.Leading
}

func actionControl(screen nativeui.Screen, action nativeui.Action) render.HTML {
	return render.Tag("button", map[string]string{"type": "submit", "form": "gfw-form", "name": "action", "value": action.ID,
		"formaction": ActionPath + "?" + url.Values{"route": {screen.Route}, "action": {action.ID}}.Encode()}, render.Text(action.Label))
}

func headerOverflow(screen nativeui.Screen, actions []nativeui.Action) render.HTML {
	if len(actions) == 0 {
		return ""
	}
	buttons := make([]render.HTML, 0, len(actions))
	for _, action := range actions {
		buttons = append(buttons, actionControl(screen, action))
	}
	return render.Tag("details", map[string]string{"class": "gfw-header-menu", "data-gfw-header-menu": ""},
		render.Tag("summary", map[string]string{"aria-label": "More actions"}, render.Text("•••")),
		render.Tag("div", map[string]string{"role": "group", "aria-label": "More actions"}, buttons...))
}

func listMarkup(screen nativeui.Screen, failure *nativeui.ActionError) render.HTML {
	list := screen.List
	content := []render.HTML{}
	queryURL, _ := url.ParseRequestURI(screen.Route)
	query := queryURL.Query()
	queryControls := []render.HTML{}
	query.Del(list.SearchKey)
	for _, field := range list.Filters {
		query.Del(field.Key)
	}
	if list.Selection != nil {
		query.Del(list.Selection.Key)
	}
	if list.Paging != nil {
		query.Del(list.Paging.Key)
	}
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range query[key] {
			queryControls = append(queryControls, hidden(key, value))
		}
	}
	if list.SearchKey != "" {
		label := list.SearchPlaceholder
		if label == "" {
			label = "Search"
		}
		queryControls = append(queryControls,
			render.Tag("label", map[string]string{"class": "gfm-sr", "for": "gfw-search"}, render.Text(label)),
			render.VoidTag("input", map[string]string{"id": "gfw-search", "type": "search", "name": list.SearchKey,
				"value": screen.Values[list.SearchKey], "placeholder": label, "enterkeyhint": "search", "data-gfw-query": list.SearchKey}))
	}
	filterControls := []render.HTML{}
	filterAttrs := map[string]string{"class": "gfw-disclosure gfw-filters", "data-gfw-filters": ""}
	for index, field := range list.Filters {
		message := ""
		if failure != nil {
			message = failure.Fields[field.Key]
		}
		if message != "" {
			filterAttrs["open"] = ""
		}
		filterControls = append(filterControls, fieldControl(field, "gfw-filter-"+strconv.Itoa(index), "data-gfw-filter", message))
	}
	if len(filterControls) != 0 {
		queryControls = append(queryControls, render.Tag("details", filterAttrs, render.Tag("summary", nil, render.Text("Filters")), render.Tag("div", map[string]string{"class": "gfw-detail-fields"}, filterControls...)))
	}
	if list.SearchKey != "" || len(list.Filters) != 0 {
		queryControls = append(queryControls, render.Tag("button", map[string]string{"type": "submit", "aria-label": "Search"},
			render.Tag("svg", map[string]string{"viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.8", "aria-hidden": "true"},
				render.Tag("circle", map[string]string{"cx": "10.5", "cy": "10.5", "r": "6.5"}), render.Tag("path", map[string]string{"d": "m16 16 5 5"}))))
		content = append(content, render.Tag("form", map[string]string{"class": "gfm-search gfw-query", "role": "search",
			"method": "get", "action": queryURL.Path, "data-gfw-search": ""}, queryControls...))
	}
	tools := []render.HTML{}
	if list.Refreshable {
		tools = append(tools, render.Tag("a", map[string]string{"class": "gfw-refresh", "href": screen.Route, "data-gfw-refresh": ""}, render.Text("Refresh")))
	}
	if list.Selection != nil {
		tools = append(tools, render.Tag("button", map[string]string{"type": "button", "data-gfw-selection-toggle": "", "aria-pressed": "false"}, render.Text("Select")))
	}
	if len(tools) != 0 {
		content = append(content, render.Tag("div", map[string]string{"class": "gfw-collection-tools"}, tools...))
	}
	selected := map[string]bool{}
	if list.Selection != nil {
		for _, identifier := range list.Selection.Selected {
			selected[identifier] = true
		}
		toolbar := []render.HTML{render.Tag("span", map[string]string{"data-gfw-selection-count": "", "role": "status", "aria-live": "polite"}, render.Text("Select items"))}
		for _, action := range screen.Actions {
			if collectionAction(screen, action) {
				toolbar = append(toolbar, actionControl(screen, action))
			}
		}
		content = append(content, render.Tag("div", map[string]string{"class": "gfw-selection-toolbar", "role": "group", "aria-label": "Selection actions"}, toolbar...))
	}
	bodyStart := len(content)
	rows := []render.HTML{}
	for _, row := range list.Rows {
		parts := []render.HTML{}
		if list.Selection != nil {
			attrs := map[string]string{"type": "checkbox", "data-gfw-select": row.ID, "aria-label": "Select " + row.Title,
				"form": "gfw-form", "name": "_gfw_selection", "value": row.ID}
			if row.Disabled {
				attrs["disabled"] = ""
			}
			if selected[row.ID] && !row.Disabled {
				attrs["checked"] = ""
			}
			parts = append(parts, render.VoidTag("input", attrs))
		}
		linkAttrs := map[string]string{"class": "gfm-row", "href": row.Destination}
		linkTag := "a"
		if row.Disabled || row.Destination == "" {
			delete(linkAttrs, "href")
			linkTag = "div"
			if row.Disabled {
				linkAttrs["aria-disabled"] = "true"
			}
		}
		copy := []render.HTML{render.Tag("h2", nil, render.Text(row.Title)), render.Tag("p", nil, render.Text(row.Subtitle))}
		if row.Metadata != "" {
			copy = append(copy, render.Tag("span", map[string]string{"class": "gfw-row-metadata"}, render.Text(row.Metadata)))
		}
		rowContent := []render.HTML{}
		if row.Icon != "" {
			rowContent = append(rowContent, fileIcon(row.Icon))
		}
		rowContent = append(rowContent, render.Tag("div", map[string]string{"class": "gfm-row-copy"}, copy...))
		parts = append(parts, render.Tag(linkTag, linkAttrs, rowContent...))
		if len(row.LeadingActions)+len(row.TrailingActions) != 0 {
			buttons := []render.HTML{}
			for _, side := range []struct {
				name    string
				actions []nativeui.RowAction
			}{{"leading", row.LeadingActions}, {"trailing", row.TrailingActions}} {
				for _, action := range side.actions {
					attrs := map[string]string{"type": "submit", "form": "gfw-form", "name": "action", "value": action.ID,
						"data-gfw-row-action": action.ID, "data-gfw-row-id": row.ID, "data-gfw-side": side.name, "data-gfw-role": action.Role,
						"formaction": ActionPath + "?" + url.Values{"route": {screen.Route}, "action": {action.ID}, "row": {row.ID}}.Encode()}
					if action.FullSwipe && action.Role != "destructive" {
						attrs["data-gfw-full-swipe"] = "true"
					}
					if row.Disabled {
						attrs["disabled"] = ""
					}
					buttons = append(buttons, render.Tag("button", attrs, render.Text(action.Label)))
				}
			}
			parts = append(parts, render.Tag("details", map[string]string{"data-gfw-row-menu": row.ID, "class": "gfw-row-menu"},
				render.Tag("summary", map[string]string{"aria-label": "Actions for " + row.Title}, render.Text("•••")), render.Tag("div", nil, buttons...)))
		}
		attrs := map[string]string{"class": "gfw-row", "data-gfw-row": row.ID}
		if row.Disabled {
			attrs["data-gfw-disabled"] = "true"
		}
		rows = append(rows, render.Tag("li", attrs, render.Tag("div", map[string]string{"class": "gfw-row-content"}, parts...)))
	}
	if len(rows) == 0 {
		content = append(content, mobileui.Empty("", list.Empty, mobileui.Action{}))
	} else {
		content = append(content, render.Tag("section", map[string]string{"class": "gfm-list-section", "aria-label": list.Header},
			render.Tag("h2", map[string]string{"class": "gfm-section-heading"}, render.Text(list.Header)), render.Tag("ul", map[string]string{"class": "gfm-list"}, rows...)))
	}
	if list.Paging != nil && list.Paging.Next != "" {
		next := queryURL.Query()
		next.Set(list.Paging.Key, list.Paging.Next)
		label := list.Paging.Label
		if label == "" {
			label = "Load more"
		}
		content = append(content, render.Tag("a", map[string]string{"class": "gfw-load-more", "data-gfw-load-more": "", "href": queryURL.Path + "?" + next.Encode()}, render.Text(label)))
	}
	if list.Footer != "" {
		content = append(content, mobileui.Footer(list.Footer))
	}
	body := render.Tag("div", map[string]string{"data-gfw-collection-content": ""}, content[bodyStart:]...)
	content = append(content[:bodyStart], body)
	return render.Tag("div", map[string]string{"data-gfw-list": ""}, content...)
}

func tabMarkup(screen nativeui.Screen) render.HTML {
	tabs := []render.HTML{}
	for _, tab := range screen.Tabs {
		attrs := map[string]string{"href": tab.Route, "data-gfw-tab": tab.ID}
		if tab.ID == screen.ActiveTab {
			attrs["aria-current"] = "page"
		}
		tabs = append(tabs, render.Tag("a", attrs, tabSymbol(tab.Icon), render.Text(tab.Label)))
	}
	return render.Tag("nav", map[string]string{"class": "gfw-tabs", "aria-label": "App tabs"}, tabs...)
}

func tabSymbol(icon nativeui.Icon) render.HTML {
	path := "M5 3h14v18H5zM8 8h8M8 12h8M8 16h5"
	switch icon {
	case nativeui.IconFile:
		path = "M3 6h7l2 2h9v12H3z"
	case nativeui.IconSettings, nativeui.IconInfo:
		path = "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8M12 2v3m0 14v3M2 12h3m14 0h3M5 5l2 2m10 10 2 2M5 19l2-2M17 7l2-2"
	}
	return render.Tag("svg", map[string]string{"aria-hidden": "true", "data-gfw-tab-icon": string(icon), "viewBox": "0 0 24 24", "fill": "none", "stroke": "currentColor", "stroke-width": "1.7", "stroke-linecap": "round", "stroke-linejoin": "round"}, render.Tag("path", map[string]string{"d": path}))
}

func selectionJSON(selected []string) string {
	if selected == nil {
		selected = []string{}
	}
	encoded, _ := json.Marshal(selected)
	return string(encoded)
}
