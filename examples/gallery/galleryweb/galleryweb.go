// Package galleryweb serves the component gallery through the WebView
// renderer inside an in-process mobilehost. It opens no listener.
package galleryweb

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/examples/gallery/gallery"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	screenweb "github.com/DonaldMurillo/gofastr-native/render/webview"
)

// Build composes the gallery for platform in dataDir, which must be an
// absolute directory the caller owns. options.Drafts defaults to a file
// store under dataDir/drafts; options.Observer is passed through. Start the
// returned host before use and Stop it to close the installation data.
func Build(dataDir string, platform mobileui.Platform, options nativeui.RuntimeOptions) (*mobilehost.Host, *screenweb.Renderer, error) {
	if !filepath.IsAbs(dataDir) {
		return nil, nil, errors.New("galleryweb: data directory must be absolute")
	}
	if err := platform.Validate(); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, err
	}
	if options.Drafts == nil {
		drafts, err := behavior.NewFileDraftStore(filepath.Join(dataDir, "drafts"))
		if err != nil {
			return nil, nil, err
		}
		options.Drafts = drafts
	}
	screens, err := gallery.New()
	if err != nil {
		return nil, nil, err
	}
	renderer, err := screenweb.New(screens, platform, options)
	if err != nil {
		return nil, nil, err
	}
	data, err := localapp.Open(dataDir)
	if err != nil {
		return nil, nil, err
	}
	app := framework.NewApp(framework.WithDB(data.DB), framework.WithSecret(data.Secret),
		framework.WithConfig(framework.AppConfig{Name: "Gallery", APIPrefix: "api"}),
		framework.WithSecurityHeaders(middleware.SecurityHeadersConfig{
			PermissionsPolicy:     "geolocation=(), microphone=(), camera=()",
			ContentSecurityPolicy: "default-src 'self'; img-src 'self' data:; object-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'",
		}))
	app.OnStop(data.Close)
	app.Router().Post(screenweb.ActionPath, renderer.Actions())
	app.Mount(renderer.UIHost())
	host, err := mobilehost.New(app, mobilehost.Config{Data: data})
	if err != nil {
		_ = app.Shutdown(context.Background())
		return nil, nil, err
	}
	return host, renderer, nil
}
