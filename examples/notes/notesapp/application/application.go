// Package application composes Notes' entity, service and installation data
// without selecting a presentation or starting a framework lifecycle.
package application

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesstore"
)

// Application holds one installation's shared composition. Data.User is the
// command identity; callers must attach it to authorized service contexts.
type Application struct {
	App     *framework.App
	Data    *localapp.Data
	Service *notes.Service
}

// Open prepares Notes without requiring a UIHost. Start App through its public
// Serve lifecycle (for example mobilehost.Host). App.Shutdown closes Data after
// later stop hooks; callers abandoning construction must also call Shutdown.
// Options configure the selected host without importing a presentation here.
func Open(dataDir string, options ...framework.AppOption) (*Application, error) {
	data, err := localapp.Open(dataDir)
	if err != nil {
		return nil, err
	}
	app := framework.NewApp(append([]framework.AppOption{framework.WithDB(data.DB), framework.WithSecret(data.Secret),
		framework.WithConfig(framework.AppConfig{Name: "Notes", APIPrefix: "api"}),
	}, options...)...)
	app.OnStop(data.Close)
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
			{Name: "pinned", Type: schema.Bool, Default: false},
			{Name: "archived", Type: schema.Bool, Default: false},
			{Name: "trashed", Type: schema.Bool, Default: false},
			{Name: "tag", Type: schema.String},
			{Name: "reminder_date", Type: schema.String},
			{Name: "reminder_time", Type: schema.String},
		},
	})
	return &Application{App: app, Data: data, Service: Service(app)}, nil
}

// Service adapts the registered scoped entity to presentation-independent Notes
// behavior. The caller supplies its authorization context on every operation.
func Service(app *framework.App) *notes.Service {
	return notes.New(notesstore.New(app.MustCrudHandler("notes")))
}
