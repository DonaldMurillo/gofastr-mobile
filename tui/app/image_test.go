//go:build darwin || linux

package app

import (
	"image"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/widget"
)

func TestImageAppProbeToRenderer(t *testing.T) {
	source := cell.NewImage(image.NewNRGBA(image.Rect(0, 0, 20, 20)))
	session := start(t, 20, 6, func(application *App) view.Element {
		if application.Tree().Images != cell.ImageKitty || application.Tree().CellWidth != 10 || application.Tree().CellHeight != 20 {
			panic("image caps not wired into tree")
		}
		return widget.Column(widget.Fixed(1, widget.Label("image ready")), widget.Grow(1, view.E(widget.Image{Source: source})))
	}, Options{})
	session.until("image on screen", func() bool { return len(session.screen.Placements) == 1 })
	session.send("\x03")
	if err := session.exit(); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.screen.Errors) != 0 {
		t.Fatal(session.screen.Errors)
	}
}
