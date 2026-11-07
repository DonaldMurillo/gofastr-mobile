//go:build gofastr_debugtrace

package notesnative

import (
	"os"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/nativeui/devtrace"
)

// debugLogs is process-wide so Stop and Start reuse one writer goroutine.
var debugLogs = devtrace.NewLogger(os.Stderr, devtrace.DefaultSize)

// debugTap lets tests in this build also observe events.
var debugTap atomic.Pointer[nativeui.Observer]

// debugObserver writes redacted gofastr-trace lines to stderr. Builds made
// with -tags gofastr_debugtrace are for development only; do not distribute them.
func debugObserver() nativeui.Observer {
	return func(event nativeui.TraceEvent) {
		debugLogs.Observe(event)
		if tap := debugTap.Load(); tap != nil {
			(*tap)(event)
		}
	}
}
