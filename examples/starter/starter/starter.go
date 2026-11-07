// Package starter is the gomobile bind surface. The bundled native host
// calls exactly these functions; the screens live in the screens package.
package starter

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/nativeui"

	"github.com/DonaldMurillo/gofastr-native/examples/starter/screens"
)

var state struct {
	sync.RWMutex
	app     *nativeui.App
	runtime *nativeui.Runtime
}

// Start opens the app's data in dataDir, the private directory the host
// passes in. Saved items go to items/ and unsaved editor drafts to
// mobile-drafts/, so both survive a reinstall that keeps app data.
func Start(dataDir string) error {
	state.Lock()
	defer state.Unlock()
	if state.app != nil {
		return errors.New("starter: already started")
	}
	if !filepath.IsAbs(dataDir) {
		return errors.New("starter: data directory must be absolute")
	}
	store, err := screens.OpenStore(filepath.Join(dataDir, "items"))
	if err != nil {
		return err
	}
	app, err := screens.New(store)
	if err != nil {
		return err
	}
	drafts, err := behavior.NewFileDraftStore(filepath.Join(dataDir, "mobile-drafts"))
	if err != nil {
		return err
	}
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{Drafts: drafts})
	if err != nil {
		return err
	}
	state.app, state.runtime = app, runtime
	return nil
}

// Stop waits for in-flight calls, then releases the app.
func Stop() error {
	state.Lock()
	defer state.Unlock()
	state.app, state.runtime = nil, nil
	return nil
}

// Root is the first route the renderer opens.
func Root() string { return screens.Root }

// Render returns one screen as JSON.
func Render(route, values string) (string, error) {
	state.RLock()
	defer state.RUnlock()
	if state.app == nil {
		return "", errors.New("starter: not started")
	}
	return state.app.RenderJSON(context.Background(), route, values)
}

// Dispatch runs a screen action and returns its result as JSON.
func Dispatch(route, action, values string) (string, error) {
	state.RLock()
	defer state.RUnlock()
	if state.app == nil {
		return "", errors.New("starter: not started")
	}
	return state.app.DispatchJSON(context.Background(), route, action, values)
}

// Command runs the renderer's editor and draft protocol.
func Command(payload string) (string, error) {
	state.RLock()
	defer state.RUnlock()
	if state.runtime == nil {
		return "", errors.New("starter: not started")
	}
	return state.runtime.CommandJSON(context.Background(), payload)
}
