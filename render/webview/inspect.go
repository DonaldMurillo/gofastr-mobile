package webview

import "github.com/DonaldMurillo/gofastr-native/nativeui"

// Runtime returns the protocol runtime this renderer drives, for diagnostics
// such as Runtime.Inspect. Commands sent through it bypass the WebView.
func (renderer *Renderer) Runtime() *nativeui.Runtime { return renderer.runtime }
