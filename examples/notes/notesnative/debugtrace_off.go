//go:build !gofastr_debugtrace

package notesnative

import "github.com/DonaldMurillo/gofastr-native/nativeui"

// debugObserver is nil in ordinary builds, so the runtime records nothing.
func debugObserver() nativeui.Observer { return nil }
