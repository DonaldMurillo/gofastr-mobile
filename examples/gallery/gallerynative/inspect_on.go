//go:build gofastr_inspect

package gallerynative

import "github.com/DonaldMurillo/gofastr-native/nativeui"

// InspectApp hands gofastr-native inspect the started registry, so its
// report can name route patterns and registered action IDs. Only inspect
// builds with the gofastr_inspect tag, so gomobile never binds it.
func InspectApp() *nativeui.App {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	return screens
}
