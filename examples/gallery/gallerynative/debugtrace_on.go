//go:build gofastr_debugtrace

package gallerynative

import (
	"os"

	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/nativeui/devtrace"
)

// logs is process-wide so Stop and Start reuse one writer goroutine.
var logs = devtrace.NewLogger(os.Stderr, devtrace.DefaultSize)

// observer writes redacted gofastr-trace lines to stderr. Builds made with
// -tags gofastr_debugtrace are for development only; do not distribute them.
func observer() nativeui.Observer { return logs.Observe }
