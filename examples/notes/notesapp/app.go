// Package notesapp assembles Notes' shared behavior and its mobile-web adapter.
// The application and host own no CSS or structural markup.
package notesapp

import (
	"context"

	"github.com/DonaldMurillo/gofastr/framework"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp/application"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesweb"
	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
)

// Build assembles the iOS presentation for compatibility with existing callers.
// Native bootstraps use application.Open without importing a web presentation.
func Build(dataDir, origin string) (*framework.App, *mobilehost.Host, error) {
	return BuildWithPlatform(dataDir, origin, mobileui.IOS)
}

// BuildWithPlatform selects an external presentation before initial SSR.
func BuildWithPlatform(dataDir, origin string, platform mobileui.Platform) (*framework.App, *mobilehost.Host, error) {
	return buildWithPresentation(dataDir, origin, platform, func(app *framework.App, service *notes.Service) error {
		web := notesweb.New(service, platform)
		app.Router().Post("/notes/save", notesweb.SaveHandler(service, platform, web))
		app.Mount(web)
		return nil
	})
}

func buildWithPresentation(dataDir, origin string, platform mobileui.Platform, mount func(*framework.App, *notes.Service) error, options ...framework.AppOption) (*framework.App, *mobilehost.Host, error) {
	if err := platform.Validate(); err != nil {
		return nil, nil, err
	}
	shared, err := application.Open(dataDir, options...)
	if err != nil {
		return nil, nil, err
	}
	app := shared.App
	if err := mount(app, shared.Service); err != nil {
		_ = app.Shutdown(context.Background())
		return nil, nil, err
	}
	host, err := mobilehost.New(app, mobilehost.Config{Data: shared.Data, Origin: origin})
	if err != nil {
		_ = app.Shutdown(context.Background())
		return nil, nil, err
	}
	return app, host, nil
}

// Service binds the registered entity to rendering-independent Notes behavior.
// As with other in-process repository calls, the caller supplies auth context.
func Service(app *framework.App) *notes.Service {
	return application.Service(app)
}
