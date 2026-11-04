// Package notesapp assembles Notes' shared behavior and its mobile-web adapter.
// The application and host own no CSS or structural markup.
package notesapp

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesstore"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesweb"
	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
)

// Build assembles the iOS presentation for compatibility with existing callers.
// Native bootstraps should use BuildWithPlatform with their explicit platform.
func Build(dataDir, origin string) (*framework.App, *mobilehost.Host, error) {
	return BuildWithPlatform(dataDir, origin, mobileui.IOS)
}

// BuildWithPlatform selects an external presentation before initial SSR.
func BuildWithPlatform(dataDir, origin string, platform mobileui.Platform) (*framework.App, *mobilehost.Host, error) {
	if err := platform.Validate(); err != nil {
		return nil, nil, err
	}
	data, err := localapp.Open(dataDir)
	if err != nil {
		return nil, nil, err
	}
	app := framework.NewApp(append(mobilehost.Options(data),
		framework.WithConfig(framework.AppConfig{Name: "Notes", APIPrefix: "api"}),
	)...)
	registerNotesEntity(app)
	service := Service(app)
	web := notesweb.New(service, platform)
	app.Router().Post("/notes/save", notesweb.SaveHandler(service, platform, web))
	app.Mount(web)
	host, err := mobilehost.New(app, mobilehost.Config{Data: data, Origin: origin})
	if err != nil {
		data.Close()
		return nil, nil, err
	}
	return app, host, nil
}

// Service binds the registered entity to rendering-independent Notes behavior.
// As with other in-process repository calls, the caller supplies auth context.
func Service(app *framework.App) *notes.Service {
	return notes.New(notesstore.New(app.MustCrudHandler("notes")))
}

func registerNotesEntity(app *framework.App) {
	app.Entity("notes", framework.EntityConfig{
		Scope:        &framework.ScopeConfig{OwnerField: "user_id"},
		SearchFields: []string{"title", "body"},
		Display: &framework.DisplayConfig{
			Singular: "Note", Plural: "Notes", TitleFields: []string{"title"},
			Columns: []string{"title", "updated_at"},
			Fields:  map[string]framework.FieldDisplay{"user_id": {Omit: true}},
		},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String, Required: true, Max: new(200.0)},
			{Name: "body", Type: schema.Text},
		},
	})
}
