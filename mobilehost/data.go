package mobilehost

import (
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
)

// Options returns the framework options an in-process app runs on: the
// database and session secret from its data directory. Open the
// directory with localapp.Open, on the path the native side passes in
// (iOS: Application Support inside the app container; Android:
// Context.getFilesDir()). os.UserConfigDir is no use here: on Android it
// depends on $HOME, which an app process does not have.
func Options(data *localapp.Data) []framework.AppOption {
	return []framework.AppOption{framework.WithDB(data.DB), framework.WithSecret(data.Secret)}
}
