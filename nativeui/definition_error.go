package nativeui

import "strings"

// DefinitionError reports a screen definition that App.Render rejected.
// Route is the registered route pattern, never the concrete destination, and
// Part names the failing section: screen, actions, list, form, tabs,
// components, images, attachments, file-preview, live, protection or
// row-destination.
//
// Err is the original validator error. Some validators, such as extension
// component checks, may build their message from application data, so treat
// Error() as developer output for the app author. Diagnostics that leave the
// process (devtrace, inspect) report only Route, Part and Go type names.
type DefinitionError struct {
	Route string
	Part  string
	Err   error
}

func (err *DefinitionError) Error() string {
	inner := "invalid definition"
	if err.Err != nil {
		inner = strings.TrimPrefix(err.Err.Error(), "nativeui: ")
	}
	return "nativeui: route " + err.Route + ": " + err.Part + ": " + inner
}

// Unwrap keeps errors.Is and errors.As behavior for the existing sentinels.
func (err *DefinitionError) Unwrap() error { return err.Err }

// definitionParts is the fixed vocabulary trace output may echo.
var definitionParts = map[string]bool{"screen": true, "actions": true, "list": true, "form": true, "tabs": true, "components": true, "images": true,
	"attachments": true, "file-preview": true, "live": true, "protection": true, "row-destination": true}

// definitionPart tags a validator failure inside validateScreen; Render adds
// the registered route pattern before returning it.
func definitionPart(part string, err error) error {
	if err == nil {
		return nil
	}
	return &DefinitionError{Part: part, Err: err}
}

// definitionAt attaches the registered pattern to a tagged error, or tags an
// untagged one with part.
func definitionAt(pattern, part string, err error) error {
	if err == nil {
		return nil
	}
	if tagged, ok := err.(*DefinitionError); ok && tagged.Route == "" {
		tagged.Route = pattern
		return tagged
	}
	return &DefinitionError{Route: pattern, Part: part, Err: err}
}
