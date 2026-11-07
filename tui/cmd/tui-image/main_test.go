package main

import (
	"image"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/vt"
)

func TestImageDemoScrollAndStatus(t *testing.T) {
	source := cell.NewImage(image.NewNRGBA(image.Rect(0, 0, 200, 1000)))
	tree := view.NewTree(uni.Graphemes)
	tree.Images = cell.ImageKitty
	tree.CellWidth, tree.CellHeight = 10, 20
	buffer := cell.NewBuffer(40, 10)
	screen := vt.New(40, 10)
	screen.Autowrap = false
	renderer := cell.NewRenderer(40, 10, cell.Options{Images: cell.ImageKitty})
	root := imageView(source, "sample.png", "kitty")
	screen.Write(renderer.Render(buffer, tree.Frame(root, buffer)))
	if !strings.Contains(screen.Text(), "Image: kitty") || len(screen.Placements) != 1 || len(screen.Errors) != 0 {
		t.Fatalf("screen %s graphics %+v errors %v", screen.Text(), screen.Placements, screen.Errors)
	}
	before := buffer.Images()[0].Crop
	if !tree.Dispatch(input.Mouse{X: 4, Y: 3, Button: input.ButtonWheelDown}) {
		t.Fatal("wheel not handled")
	}
	screen.Write(renderer.Render(buffer, tree.Frame(root, buffer)))
	if buffer.Images()[0].Crop == before || len(screen.Placements) != 1 || len(screen.Errors) != 0 {
		t.Fatalf("scroll crop %+v before %v errors %v", buffer.Images(), before, screen.Errors)
	}
	if !tree.Dispatch(input.Key{Code: input.KeyPageDown}) {
		t.Fatal("page-down did not reach scroll panel")
	}
}
