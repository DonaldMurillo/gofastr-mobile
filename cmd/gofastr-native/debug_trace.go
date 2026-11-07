package main

import "errors"

// debugTraceTag selects the debugtrace_on.go files that wire a
// nativeui/devtrace logger into an app's Runtime. Apps without such a file
// compile the same with or without it.
const debugTraceTag = "gofastr_debugtrace"

// releaseTag makes the gomobile tag selection explicit, overriding ambient
// GOFLAGS tags so inspection exports and trace hooks stay out of releases.
const releaseTag = "gofastr_release"

var (
	errDebugTraceRelease  = errors.New("--debug-trace is for debug builds; remove it or use --profile debug")
	errDebugTraceRenderer = errors.New("--debug-trace needs --renderer native; the WebView renderer has no Go trace hook")
)

// debugTraceBindFlags returns the extra gomobile bind flags for
// --debug-trace. Callers append them to bindArgs before the package path.
// Callers should treat an error as a usage error (exit 2).
func debugTraceBindFlags(enabled, release bool, renderer string) ([]string, error) {
	if !enabled {
		return nil, nil
	}
	if release {
		return nil, errDebugTraceRelease
	}
	if renderer != "native" {
		return nil, errDebugTraceRenderer
	}
	return []string{"-tags=" + debugTraceTag}, nil
}
