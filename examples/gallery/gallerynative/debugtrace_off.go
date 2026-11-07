//go:build !gofastr_debugtrace

package gallerynative

import "github.com/DonaldMurillo/gofastr-native/nativeui"

// observer is nil in ordinary builds, so the runtime records nothing.
func observer() nativeui.Observer { return nil }
