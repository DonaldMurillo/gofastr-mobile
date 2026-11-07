// Package gallerynative is the gomobile bind package for the component
// gallery. Build it with gofastr-native build --pkg ./examples/gallery/gallerynative.
package gallerynative

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/examples/gallery/gallery"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

var lifecycle sync.RWMutex
var runtime *nativeui.Runtime
var screens *nativeui.App

// Start builds fresh gallery state. dataDir must be absolute; drafts are
// stored under dataDir/drafts.
func Start(dataDir string) error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if runtime != nil {
		return errors.New("gallerynative: already started")
	}
	if !filepath.IsAbs(dataDir) {
		return errors.New("gallerynative: data directory must be absolute")
	}
	drafts, err := behavior.NewFileDraftStore(filepath.Join(dataDir, "drafts"))
	if err != nil {
		return err
	}
	app, err := gallery.New()
	if err != nil {
		return err
	}
	r, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{Drafts: drafts, Observer: observer()})
	if err != nil {
		return err
	}
	screens, runtime = app, r
	return nil
}

// Stop releases the gallery. Start may be called again afterwards.
func Stop() error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	screens, runtime = nil, nil
	return nil
}

// Root is the first route.
func Root() string { return gallery.Root }

// Command handles one versioned protocol command.
func Command(payload string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if runtime == nil {
		return "", errors.New("gallerynative: not started")
	}
	return runtime.CommandJSON(context.Background(), payload)
}

// Render is the legacy unversioned render export.
func Render(route, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if screens == nil {
		return "", errors.New("gallerynative: not started")
	}
	return screens.RenderJSON(context.Background(), route, values)
}

// Dispatch is the legacy unversioned action export.
func Dispatch(route, action, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if screens == nil {
		return "", errors.New("gallerynative: not started")
	}
	return screens.DispatchJSON(context.Background(), route, action, values)
}
