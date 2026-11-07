package view

import (
	"image"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
)

type imageClipWidget struct{ source *cell.Image }

func (imageClipWidget) Layout(_ *Node, constraints layout.Constraints) layout.Size {
	return constraints.Max()
}
func (widget imageClipWidget) Draw(_ *Node, canvas *Canvas) {
	canvas.PlaceImage(widget.source, layout.Rect{X: -2, Y: -1, W: 8, H: 4}, image.Rect(0, 0, 80, 80))
}

func TestImageCanvasClip(t *testing.T) {
	tree := NewTree(uni.Graphemes)
	tree.Images = cell.ImageKitty
	buffer := cell.NewBuffer(4, 2)
	tree.Frame(E(imageClipWidget{cell.NewImage(image.NewNRGBA(image.Rect(0, 0, 80, 80)))}), buffer)
	if len(buffer.Images()) != 1 || buffer.Images()[0].Rect != image.Rect(0, 0, 4, 2) || buffer.Images()[0].Crop != image.Rect(20, 20, 60, 60) {
		t.Fatalf("clipped images %+v", buffer.Images())
	}
}
