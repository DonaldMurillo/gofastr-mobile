package nativeui

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr-mobile/behavior"
)

func validateFields(fields []Field, keys map[string]bool) error {
	if len(fields) > 128 {
		return errors.New("nativeui: too many fields")
	}
	for _, field := range fields {
		if !behavior.SafeToken(field.Key) || field.Key == "row" || keys[field.Key] || strings.TrimSpace(field.Label) == "" || (field.Style != "" && field.Style != "body" && field.Style != "title" && field.Style != "detail") {
			return errors.New("nativeui: invalid or duplicate field")
		}
		keys[field.Key] = true
		switch field.Kind {
		case "text", "multiline", "toggle", "date", "time":
			if len(field.Options) != 0 {
				return errors.New("nativeui: only select fields accept options")
			}
		case "select":
			if len(field.Options) == 0 || len(field.Options) > 256 {
				return errors.New("nativeui: select requires bounded options")
			}
			seen := make(map[string]bool)
			for _, option := range field.Options {
				if option.Value != "" && !behavior.SafeToken(option.Value) || seen[option.Value] || strings.TrimSpace(option.Label) == "" {
					return errors.New("nativeui: invalid or duplicate select option")
				}
				seen[option.Value] = true
			}
		default:
			return errors.New("nativeui: unsupported field kind")
		}
		if field.Value != "" {
			if message := fieldValueError(field, field.Value, false); message != "" {
				return fmt.Errorf("nativeui: invalid field %q: %s", field.Key, message)
			}
		}
	}
	return nil
}

func fieldValueError(field Field, value string, required bool) string {
	if value == "" {
		if required && field.Required {
			return "Required."
		}
		return ""
	}
	if required && field.Required && strings.TrimSpace(value) == "" {
		return "Required."
	}
	switch field.Kind {
	case "toggle":
		if value != "true" && value != "false" {
			return "Choose true or false."
		}
	case "select":
		for _, option := range field.Options {
			if option.Value == value && (!option.Disabled || field.Disabled && field.Value == value) {
				return ""
			}
		}
		return "Choose an available option."
	case "date", "time":
		layout, message := "2006-01-02", "Use a valid date in YYYY-MM-DD format."
		if field.Kind == "time" {
			layout, message = "15:04", "Use a valid time in HH:MM format."
		}
		parsed, err := time.Parse(layout, value)
		if err != nil || parsed.Format(layout) != value || field.Kind == "date" && parsed.Year() < 1 {
			return message
		}
	}
	return ""
}

func validateList(list List, actions map[string]Handle) error {
	keys := make(map[string]bool)
	if list.SearchKey != "" {
		if !behavior.SafeToken(list.SearchKey) || list.SearchKey == "row" {
			return errors.New("nativeui: invalid search key")
		}
		keys[list.SearchKey] = true
	}
	if err := validateFields(list.Filters, keys); err != nil {
		return err
	}
	available := make(map[string]bool)
	for _, row := range list.Rows {
		rowActions := append(append([]RowAction(nil), row.LeadingActions...), row.TrailingActions...)
		if row.ID != "" || list.Selection != nil || len(rowActions) != 0 {
			if !behavior.SafeToken(row.ID) {
				return errors.New("nativeui: invalid row ID")
			}
			if _, exists := available[row.ID]; exists {
				return errors.New("nativeui: duplicate row ID")
			}
			available[row.ID] = !row.Disabled
		}
		if strings.TrimSpace(row.Title) == "" || len(rowActions) > 16 {
			return errors.New("nativeui: invalid row title or action count")
		}
		if row.Icon != "" && row.Icon != IconFile && row.Icon != IconPhoto && row.Icon != IconAudio {
			return errors.New("nativeui: unsupported row icon")
		}
		seen := make(map[string]bool)
		for _, action := range rowActions {
			if !behavior.SafeToken(action.ID) || actions[action.ID] == nil || seen[action.ID] || strings.TrimSpace(action.Label) == "" || action.Role != "" && action.Role != "destructive" || action.Role == "destructive" && action.FullSwipe {
				return errors.New("nativeui: invalid, undeclared, or duplicate row action")
			}
			seen[action.ID] = true
		}
	}
	if list.Selection != nil {
		selection := list.Selection
		if !behavior.SafeToken(selection.Key) || selection.Key == "row" || keys[selection.Key] || len(selection.Options) != 0 || selection.Multiple || selection.Min != 0 || selection.Max != 0 {
			return errors.New("nativeui: invalid collection selection")
		}
		keys[selection.Key] = true
		if err := behavior.ValidateSelection(selection.Selected, available, selection.Maximum); err != nil {
			return fmt.Errorf("nativeui: invalid collection selection: %w", err)
		}
	}
	if list.Paging != nil {
		paging := list.Paging
		if !behavior.SafeToken(paging.Key) || paging.Key == "row" || keys[paging.Key] || strings.TrimSpace(paging.Label) == "" {
			return errors.New("nativeui: invalid paging control")
		}
		if paging.Next != "" {
			if _, err := (behavior.CollectionState{}).LoadMore(paging.Next); err != nil {
				return fmt.Errorf("nativeui: invalid next page: %w", err)
			}
		}
	}
	return nil
}

func componentValues(screen *Screen) error {
	values := maps.Clone(screen.Values)
	if values == nil {
		values = make(map[string]string)
	}
	if screen.Form != nil {
		for _, field := range screen.Form.Fields {
			if value, exists := values[field.Key]; !exists {
				values[field.Key] = field.Value
			} else if field.Disabled && value != field.Value {
				return invalidComponents(map[string]string{field.Key: "This control is disabled."})
			}
		}
	}
	if screen.List != nil {
		list := *screen.List
		for _, field := range list.Filters {
			if value, exists := values[field.Key]; exists {
				if failure := validateFieldSubmission([]Field{field}, map[string]string{field.Key: value}, false); len(failure) != 0 {
					return invalidComponents(failure)
				}
			}
			values[field.Key] = field.Value
		}
		if list.SearchKey != "" {
			values[list.SearchKey] = screen.Values[list.SearchKey]
		}
		if list.Selection != nil {
			selection := *list.Selection
			selection.Maximum, _ = behavior.SelectionMaximum(selection.Maximum)
			if encoded, exists := values[selection.Key]; exists {
				selected, err := behavior.DecodeSelection(encoded, listRows(list), selection.Maximum)
				if err != nil {
					return invalidComponents(map[string]string{selection.Key: err.Error()})
				}
				selection.Selected = selected
			}
			if selection.Selected == nil {
				selection.Selected = []string{}
			}
			encoded, _ := json.Marshal(selection.Selected)
			values[selection.Key] = string(encoded)
			list.Selection = &selection
		}
		if list.Paging != nil {
			page := values[list.Paging.Key]
			if page != "" && !behavior.SafeToken(page) {
				return invalidComponents(map[string]string{list.Paging.Key: "Invalid page token."})
			}
			if list.Paging.Next != "" {
				if _, err := (behavior.CollectionState{Page: page}).LoadMore(list.Paging.Next); err != nil {
					return fmt.Errorf("nativeui: invalid paging progression: %w", err)
				}
			}
			values[list.Paging.Key] = page
		}
		screen.List = &list
	}
	screen.Values = values
	return nil
}

func validateFieldSubmission(fields []Field, values map[string]string, required bool) map[string]string {
	failures := make(map[string]string)
	for _, field := range fields {
		value, exists := values[field.Key]
		if !exists {
			value = field.Value
		}
		if field.Disabled {
			if value != field.Value {
				failures[field.Key] = "This control is disabled."
			}
		} else if message := fieldValueError(field, value, required); message != "" {
			failures[field.Key] = message
		}
	}
	return failures
}

func invalidComponents(fields map[string]string) *ActionError {
	return &ActionError{Code: "validation", Message: "Check the submitted controls and records.", Fields: fields, Err: ErrInvalidValues}
}

func listRows(list List) map[string]bool {
	available := make(map[string]bool, len(list.Rows))
	for _, row := range list.Rows {
		available[row.ID] = !row.Disabled
	}
	return available
}

func collectionQuery(screen Screen) behavior.Values {
	query := make(behavior.Values)
	if screen.List != nil {
		if screen.List.SearchKey != "" {
			query[screen.List.SearchKey] = screen.Values[screen.List.SearchKey]
		}
		for _, field := range screen.List.Filters {
			query[field.Key] = field.Value
		}
	}
	return query
}

func authorizedQuery(screen Screen) map[string]string {
	values := map[string]string(collectionQuery(screen))
	if screen.List != nil && screen.List.Paging != nil {
		values[screen.List.Paging.Key] = screen.Values[screen.List.Paging.Key]
	}
	return values
}

func submittedComponents(screen Screen, action string, submitted map[string]string) (map[string]string, Action, error) {
	values := maps.Clone(screen.Values)
	if values == nil {
		values = make(map[string]string)
	}
	maps.Copy(values, submitted)
	failures := make(map[string]string)
	if screen.Form != nil {
		for _, field := range screen.Form.Fields {
			if _, exists := submitted[field.Key]; !exists {
				values[field.Key] = field.Value
			}
		}
		maps.Copy(failures, validateFieldSubmission(screen.Form.Fields, values, true))
	}
	if screen.List != nil {
		list := screen.List
		maps.Copy(failures, validateFieldSubmission(list.Filters, values, true))
		query := collectionQuery(screen)
		nextQuery := maps.Clone(query)
		for key := range nextQuery {
			nextQuery[key] = values[key]
		}
		state := behavior.CollectionState{Query: query}
		if list.Paging != nil {
			state.Page = screen.Values[list.Paging.Key]
		}
		updated := state.WithQuery(nextQuery)
		if !maps.Equal(query, updated.Query) {
			for key, value := range nextQuery {
				if value != query[key] {
					failures[key] = "Reopen this list after changing its query; paging and selection reset."
				}
			}
		}
		if list.Paging != nil && (values[list.Paging.Key] != state.Page || values[list.Paging.Key] != updated.Page) {
			failures[list.Paging.Key] = "Reopen this list to load another page."
		}
		if list.Selection != nil {
			selection := list.Selection
			selected, err := behavior.DecodeSelection(values[selection.Key], listRows(*list), selection.Maximum)
			if err != nil {
				failures[selection.Key] = err.Error()
			} else {
				encoded, _ := json.Marshal(selected)
				values[selection.Key] = string(encoded)
			}
		}
	}
	definition, err := availableAction(screen, action, values)
	if err != nil {
		var failure *ActionError
		if errors.As(err, &failure) {
			maps.Copy(failures, failure.Fields)
		} else {
			return nil, Action{}, err
		}
	}
	if len(failures) != 0 {
		return nil, Action{}, invalidComponents(failures)
	}
	return values, definition, nil
}

func availableAction(screen Screen, action string, values map[string]string) (Action, error) {
	identifier, rowSubmitted := values["row"]
	if rowSubmitted {
		if screen.List != nil {
			for _, row := range screen.List.Rows {
				if row.ID != identifier || row.Disabled {
					continue
				}
				for _, candidates := range [][]RowAction{row.LeadingActions, row.TrailingActions} {
					for _, candidate := range candidates {
						if candidate.ID == action {
							return Action{ID: action, Label: candidate.Label, Commit: true}, nil
						}
					}
				}
			}
		}
		return Action{}, invalidComponents(map[string]string{"row": "Choose an enabled row exposing this action."})
	}
	for _, definition := range screen.Actions {
		if definition.ID == action {
			return definition, nil
		}
	}
	if screen.List != nil {
		for _, row := range screen.List.Rows {
			for _, candidates := range [][]RowAction{row.LeadingActions, row.TrailingActions} {
				for _, candidate := range candidates {
					if candidate.ID == action {
						return Action{}, invalidComponents(map[string]string{"row": "Choose an enabled row exposing this action."})
					}
				}
			}
		}
	}
	return Action{}, fmt.Errorf("%w %q", ErrUnknownAction, action)
}

func (app *App) validateTabs(screen Screen) error {
	if len(screen.Tabs) > 16 || len(screen.Tabs) == 0 && screen.ActiveTab != "" {
		return errors.New("nativeui: invalid tab shell")
	}
	seen, roots := make(map[string]bool), make(map[string]bool)
	for _, tab := range screen.Tabs {
		if !behavior.SafeToken(tab.ID) || seen[tab.ID] || strings.TrimSpace(tab.Label) == "" || tab.Icon != "" && !behavior.SafeToken(string(tab.Icon)) {
			return errors.New("nativeui: invalid or duplicate tab")
		}
		root, err := app.Resolve(tab.Route)
		if err != nil {
			return fmt.Errorf("nativeui: invalid tab destination: %w", err)
		}
		if root.Path != tab.Route || strings.ContainsAny(root.Path, "{}") || roots[tab.Route] {
			return errors.New("nativeui: tabs require distinct registered literal roots")
		}
		seen[tab.ID], roots[tab.Route] = true, true
	}
	if len(screen.Tabs) != 0 && !seen[screen.ActiveTab] {
		return errors.New("nativeui: active tab must identify a declared tab")
	}
	return nil
}
