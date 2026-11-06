package notesapp

import (
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesbackup"
	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesscreens"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	screenweb "github.com/DonaldMurillo/gofastr-native/render/webview"
)

// BuildScreensWithPlatform renders the same Go mobile screen definitions used by
// UIKit through the WebView adapter. BuildWithPlatform preserves the independent
// handwritten web presentation for existing callers.
func BuildScreensWithPlatform(dataDir, origin string, platform mobileui.Platform) (*framework.App, *mobilehost.Host, error) {
	return buildWithPresentation(dataDir, origin, platform, func(app *framework.App, service *notes.Service) error {
		attachments, err := files.NewStore(filepath.Join(dataDir, "mobile-files"))
		if err != nil {
			return err
		}
		backups, err := notesbackup.New(filepath.Join(dataDir, "mobile-backups"), service, attachments)
		if err != nil {
			return err
		}
		screens, err := notesscreens.NewWithFeatures(service, attachments, notesscreens.Features{Backups: backups})
		if err != nil {
			return err
		}
		drafts, err := behavior.NewFileDraftStore(filepath.Join(dataDir, "mobile-drafts"))
		if err != nil {
			return err
		}
		renderer, err := screenweb.New(screens, platform, nativeui.RuntimeOptions{Drafts: drafts})
		if err != nil {
			return err
		}
		app.Router().Post(screenweb.ActionPath, renderer.Actions())
		app.Mount(renderer.UIHost())
		return nil
	}, framework.WithSecurityHeaders(middleware.SecurityHeadersConfig{
		PermissionsPolicy:     "geolocation=(), microphone=(self), camera=()",
		ContentSecurityPolicy: "default-src 'self'; img-src 'self' data:; media-src 'self' blob:; object-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'",
	}))
}
