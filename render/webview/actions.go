package webview

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr-mobile/nativeui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type actionRequest struct {
	Route  string            `json:"route"`
	Action string            `json:"action"`
	Values map[string]string `json:"values"`
}

func (renderer *Renderer) serveAction(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.URL.Path != ActionPath {
		http.NotFound(writer, request)
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "Use POST", http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(request) {
		writeFailure(writer, http.StatusForbidden, &nativeui.ActionError{Code: "forbidden", Message: "Cross-origin actions are not allowed."})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8<<20)
	if request.Header.Get("X-GoFastr-Mobile-Protocol") == "1" {
		encoded, err := io.ReadAll(request.Body)
		if err != nil {
			writeFailure(writer, http.StatusRequestEntityTooLarge, &nativeui.ActionError{Code: "invalid_request", Message: "Invalid or oversized mobile command."})
			return
		}
		response, err := renderer.runtime.CommandJSON(request.Context(), string(encoded))
		if err != nil {
			writeFailure(writer, http.StatusBadRequest, &nativeui.ActionError{Code: "invalid_request", Message: "Invalid mobile command."})
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, response)
		return
	}
	input, err := readAction(request)
	if err != nil {
		status := http.StatusBadRequest
		var oversized *http.MaxBytesError
		if errors.As(err, &oversized) {
			status = http.StatusRequestEntityTooLarge
		}
		writeFailure(writer, status, &nativeui.ActionError{Code: "invalid_request", Message: "Invalid or oversized action request."})
		return
	}
	if strings.HasPrefix(request.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		if definition, renderErr := renderer.screens.Render(request.Context(), input.Route, nil); renderErr == nil {
			normalizeFormValues(definition, input.Values)
		}
	}
	response, err := renderer.screens.Dispatch(request.Context(), input.Route, input.Action, input.Values)
	wantsJSON := strings.Contains(request.Header.Get("Accept"), "application/json") || strings.HasPrefix(request.Header.Get("Content-Type"), "application/json")
	if err != nil {
		status, failure := actionFailure(err)
		if wantsJSON {
			writeFailure(writer, status, failure)
			return
		}
		definition, renderErr := renderer.screens.Render(request.Context(), input.Route, nil)
		if renderErr != nil {
			writeFailure(writer, status, failure)
			return
		}
		retainFields(&definition, input.Values)
		renderer.host.RenderScreen(writer, request, &pageScreen{renderer: renderer, definition: &definition, failure: failure}, uihost.ScreenResponse{Status: status})
		return
	}
	if wantsJSON {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(response)
		return
	}
	if response.Navigation != nil {
		destination := response.Navigation.Route
		if destination == "" {
			destination = renderer.screens.Root()
		}
		http.Redirect(writer, request, destination, http.StatusSeeOther)
		return
	}
	definition, err := renderer.screens.Render(request.Context(), input.Route, nil)
	if err != nil {
		status, failure := actionFailure(err)
		writeFailure(writer, status, failure)
		return
	}
	retainFields(&definition, input.Values)
	renderer.host.RenderScreen(writer, request, &pageScreen{renderer: renderer, definition: &definition, effects: &response.Result}, uihost.ScreenResponse{})
}

func retainFields(screen *nativeui.Screen, values map[string]string) {
	if screen.Form == nil {
		return
	}
	form := *screen.Form
	form.Fields = append([]nativeui.Field(nil), form.Fields...)
	for index := range form.Fields {
		if value, present := values[form.Fields[index].Key]; present && !form.Fields[index].Disabled {
			form.Fields[index].Value = value
		}
	}
	screen.Form = &form
}

func readAction(request *http.Request) (actionRequest, error) {
	var input actionRequest
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		return input, err
	}
	switch mediaType {
	case "application/json":
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			return input, err
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return input, errors.New("webview: expected one action")
		}
	case "application/x-www-form-urlencoded":
		if err := request.ParseForm(); err != nil {
			return input, err
		}
		input.Values = make(map[string]string)
		query, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil || len(query["route"]) != 1 || len(query["action"]) != 1 {
			return input, errors.New("webview: invalid action target")
		}
		input.Route, input.Action = query.Get("route"), query.Get("action")
		if len(query["row"]) == 1 {
			input.Values["row"] = query.Get("row")
		}
		for key, values := range request.PostForm {
			if key == "_gfw_selection" {
				input.Values[key] = selectionJSON(values)
				continue
			}
			if len(values) != 1 {
				return input, errors.New("webview: duplicate form field")
			}
			if key != "_csrf" {
				input.Values[key] = values[0]
			}
		}
	default:
		return input, errors.New("webview: unsupported content type")
	}
	if input.Route == "" || input.Action == "" || len(input.Action) > 256 || len(input.Values) > 128 {
		return input, errors.New("webview: invalid action")
	}
	return input, nil
}

func normalizeFormValues(screen nativeui.Screen, values map[string]string) {
	fields := []nativeui.Field{}
	if screen.Form != nil {
		fields = screen.Form.Fields
	}
	if screen.List != nil {
		fields = append(append([]nativeui.Field(nil), fields...), screen.List.Filters...)
		if screen.List.Selection != nil {
			selected := values["_gfw_selection"]
			if selected == "" {
				selected = "[]"
			}
			values[screen.List.Selection.Key] = selected
		}
	}
	delete(values, "_gfw_selection")
	for _, field := range fields {
		if _, present := values[field.Key]; !present && field.Kind == "toggle" && !field.Disabled {
			values[field.Key] = "false"
		}
	}
}

func sameOrigin(request *http.Request) bool {
	if request.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := request.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		scheme := "http"
		if request.TLS != nil || request.URL.Scheme == "https" || request.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		return err == nil && parsed.Scheme == scheme && parsed.Host == request.Host &&
			parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
	}
	return true
}

func actionFailure(err error) (int, *nativeui.ActionError) {
	var failure *nativeui.ActionError
	if errors.As(err, &failure) {
		status := map[string]int{"validation": http.StatusUnprocessableEntity, "not_found": http.StatusNotFound,
			"forbidden": http.StatusForbidden, "conflict": http.StatusConflict}[failure.Code]
		if status != 0 {
			return status, failure
		}
	}
	if errors.Is(err, nativeui.ErrInvalidRoute) || errors.Is(err, nativeui.ErrUnknownAction) || errors.Is(err, nativeui.ErrInvalidValues) {
		return http.StatusBadRequest, &nativeui.ActionError{Code: "invalid_request", Message: "This action is not available."}
	}
	if errors.Is(err, nativeui.ErrScreenNotFound) {
		return http.StatusNotFound, &nativeui.ActionError{Code: "not_found", Message: "This screen is no longer available. Your text has not been discarded."}
	}
	return http.StatusInternalServerError, &nativeui.ActionError{Code: "internal", Message: "Could not complete the action. Your text is still here; please try again."}
}

func writeFailure(writer http.ResponseWriter, status int, failure *nativeui.ActionError) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(struct {
		Version int                   `json:"version"`
		Error   *nativeui.ActionError `json:"error"`
	}{Version: 1, Error: failure})
}
