package widget

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/input"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/uni"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
	"github.com/DonaldMurillo/gofastr-native/tui/vt"
)

func knownImage() *cell.Image {
	pixels := image.NewNRGBA(image.Rect(0, 0, 2, 4))
	colors := []color.NRGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}, {R: 255, G: 255, A: 255}, {R: 255, B: 255, A: 255}, {G: 255, B: 255, A: 255}, {R: 255, G: 255, B: 255, A: 255}, {A: 255}}
	for index, shade := range colors {
		pixels.SetNRGBA(index%2, index/2, shade)
	}
	return cell.NewImage(pixels)
}

func TestImageHalfBlockColors(t *testing.T) {
	source := knownImage()
	tree := view.NewTree(uni.Graphemes)
	tree.CellWidth, tree.CellHeight = 1, 2
	buffer := cell.NewBuffer(2, 2)
	cursor := tree.Frame(view.E(Image{Source: source}), buffer)
	if len(buffer.Images()) != 0 {
		t.Fatal("fallback recorded protocol placements")
	}
	for row := 0; row < 2; row++ {
		for col := 0; col < 2; col++ {
			actual := buffer.At(col, row)
			topRed, topGreen, topBlue, _ := source.Pixels.At(col, row*2).RGBA()
			bottomRed, bottomGreen, bottomBlue, _ := source.Pixels.At(col, row*2+1).RGBA()
			wanted := cell.Cell{Text: "▀", Width: 1, Style: cell.Style{Fg: cell.RGB(uint8(topRed>>8), uint8(topGreen>>8), uint8(topBlue>>8)), Bg: cell.RGB(uint8(bottomRed>>8), uint8(bottomGreen>>8), uint8(bottomBlue>>8))}}
			if actual != wanted {
				t.Fatalf("cell %d,%d got %+v want %+v", col, row, actual, wanted)
			}
		}
	}
	for _, mode := range []cell.ColorMode{cell.ColorTrue, cell.Color256, cell.Color16} {
		renderer := cell.NewRenderer(2, 2, cell.Options{Colors: mode})
		screen := vt.New(2, 2)
		screen.Autowrap = false
		screen.Write(renderer.Render(buffer, cursor))
		if len(screen.Errors) != 0 {
			t.Fatal(screen.Errors)
		}
		for row := 0; row < 2; row++ {
			for col := 0; col < 2; col++ {
				wanted := buffer.At(col, row)
				wanted.Style.Fg = wanted.Style.Fg.Downgrade(mode)
				wanted.Style.Bg = wanted.Style.Bg.Downgrade(mode)
				if screen.Cell(col, row) != wanted {
					t.Fatalf("mode %v cell %+v want %+v", mode, screen.Cell(col, row), wanted)
				}
			}
		}
	}
}

func TestImageFitModes(t *testing.T) {
	source := cell.NewImage(image.NewNRGBA(image.Rect(0, 0, 100, 100)))
	for _, test := range []struct {
		name string
		mode FitMode
		rect layout.Rect
		crop image.Rectangle
	}{
		{"contain", Contain, layout.Rect{X: 2, W: 5, H: 5}, image.Rect(0, 0, 100, 100)},
		{"cover", Cover, layout.Rect{W: 10, H: 5}, image.Rect(0, 25, 100, 75)},
		{"stretch", Stretch, layout.Rect{W: 10, H: 5}, image.Rect(0, 0, 100, 100)},
		{"none", None, layout.Rect{W: 10, H: 10}, image.Rect(0, 0, 100, 100)},
	} {
		t.Run(test.name, func(t *testing.T) {
			widget := Image{Source: source, Fit: test.mode, Align: layout.Center}
			rect, crop := widget.geometry(layout.Size{W: 10, H: 5}, 10, 10)
			if rect != test.rect || crop != test.crop {
				t.Fatalf("geometry %v %v want %v %v", rect, crop, test.rect, test.crop)
			}
			tree := view.NewTree(uni.Graphemes)
			tree.Images = cell.ImageKitty
			tree.CellWidth, tree.CellHeight = 10, 10
			buffer := cell.NewBuffer(10, 5)
			tree.Frame(view.E(widget), buffer)
			if len(buffer.Images()) != 1 {
				t.Fatalf("placements %v", buffer.Images())
			}
			placement := buffer.Images()[0]
			if placement.Rect != image.Rect(rect.X, rect.Y, rect.X+min(rect.W, 10), rect.Y+min(rect.H, 5)) {
				t.Fatalf("placement %+v", placement)
			}
			if test.mode == Stretch && placement.Image.Pixels.Bounds() != image.Rect(0, 0, 100, 50) {
				t.Fatal("kitty stretch did not resample")
			}
		})
	}
}

type imageLayoutParent struct{ constraints layout.Constraints }

func (parent imageLayoutParent) Layout(node *view.Node, constraints layout.Constraints) layout.Size {
	node.LayoutChild(0, parent.constraints)
	node.Place(0, layout.Point{})
	return constraints.Max()
}
func (imageLayoutParent) Draw(*view.Node, *view.Canvas) {}

func TestImageLayoutAndAspect(t *testing.T) {
	for _, mode := range []FitMode{Contain, Cover, Stretch, None} {
		for _, test := range []struct {
			name          string
			width, height int
			limit, want   layout.Size
		}{
			{"intrinsic", 0, 0, layout.Size{W: 30, H: 30}, layout.Size{W: 10, H: 5}},
			{"constrained", 0, 0, layout.Size{W: 4, H: 30}, layout.Size{W: 4, H: 2}},
			{"width", 6, 0, layout.Size{W: 30, H: 30}, layout.Size{W: 6, H: 3}},
			{"height", 0, 3, layout.Size{W: 30, H: 30}, layout.Size{W: 6, H: 3}},
			{"both", 8, 8, layout.Size{W: 30, H: 30}, layout.Size{W: 8, H: 8}},
		} {
			t.Run(test.name+string(rune('0'+mode)), func(t *testing.T) {
				tree := view.NewTree(uni.Graphemes)
				source := cell.NewImage(image.NewNRGBA(image.Rect(0, 0, 100, 100)))
				tree.Frame(view.E(imageLayoutParent{layout.Loose(test.limit)}, view.E(Image{Source: source, Fit: mode, Width: test.width, Height: test.height})), cell.NewBuffer(30, 30))
				if size := tree.Root().Child(0).Size(); size != test.want {
					t.Fatalf("size %v want %v", size, test.want)
				}
			})
		}
	}
	widget := Image{Source: cell.NewImage(image.NewNRGBA(image.Rect(0, 0, 100, 100))), Align: layout.Center}
	rect, _ := widget.geometry(layout.Size{W: 20, H: 10}, 10, 20)
	if rect != (layout.Rect{X: 0, W: 20, H: 10}) {
		t.Fatalf("non-square cell aspect %v", rect)
	}
}

func TestImageScrollClip(t *testing.T) {
	for _, protocol := range []cell.ImageProtocol{cell.ImageNone, cell.ImageKitty, cell.ImageSixel, cell.ImageITerm} {
		t.Run(protocol.String(), func(t *testing.T) {
			tree := view.NewTree(uni.Graphemes)
			tree.Images = protocol
			tree.CellWidth, tree.CellHeight = 1, 2
			buffer := cell.NewBuffer(4, 2)
			source := cell.NewImage(image.NewNRGBA(image.Rect(0, 0, 4, 16)))
			root := view.E(Scroll{NoBar: true}, view.E(Image{Source: source, Width: 4, Height: 8, Fit: Stretch}))
			tree.Frame(root, buffer)
			if !tree.Dispatch(input.Mouse{X: 1, Y: 0, Button: input.ButtonWheelDown}) {
				t.Fatal("scroll did not handle wheel")
			}
			tree.Frame(root, buffer)
			if protocol == cell.ImageNone {
				if buffer.At(0, 0).Text != "▀" || len(buffer.Images()) != 0 {
					t.Fatal("fallback did not scroll")
				}
			} else {
				if len(buffer.Images()) != 1 {
					t.Fatal(buffer.Images())
				}
				placement := buffer.Images()[0]
				if placement.Rect != image.Rect(0, 0, 4, 2) || placement.Crop != image.Rect(0, 6, 4, 10) {
					t.Fatalf("scroll crop %+v", placement)
				}
				renderer := cell.NewRenderer(4, 2, cell.Options{Images: protocol, CellWidth: 1, CellHeight: 2})
				screen := vt.New(4, 2)
				screen.Autowrap = false
				screen.CellWidth, screen.CellHeight = 1, 2
				screen.Write(renderer.Render(buffer, cell.Cursor{}))
				if len(screen.Errors) != 0 || len(screen.Placements) != 1 || screen.Placements[0].Rect != placement.Rect {
					t.Fatalf("VT %+v errors %v", screen.Placements, screen.Errors)
				}
			}
		})
	}
}

func TestImageAltAndCachedRaster(t *testing.T) {
	tree := view.NewTree(uni.Graphemes)
	buffer := cell.NewBuffer(6, 2)
	tree.Frame(view.E(Image{Alt: "absent"}), buffer)
	if buffer.At(0, 0).Text != "a" {
		t.Fatal("missing alt")
	}
	tree.Frame(view.E(imageLayoutParent{layout.Loose(layout.Size{W: 6, H: 2})}, view.E(Image{Alt: "absent"})), buffer)
	if tree.Root().Child(0).Size() != (layout.Size{W: 6, H: 1}) || buffer.At(0, 0).Text != "a" {
		t.Fatal("alt text collapsed under loose constraints")
	}
	source := knownImage()
	tree.CellWidth, tree.CellHeight = 1, 2
	root := view.E(Image{Source: source})
	tree.Frame(root, buffer)
	state := tree.Root().State().(*imageState)
	previous, version := state.raster, state.raster.Version
	tree.Frame(root, buffer)
	if state.raster != previous || state.raster.Version != version {
		t.Fatal("steady raster was rebuilt")
	}
	source.SetPixels(knownImage().Pixels)
	tree.Frame(root, buffer)
	if state.raster.Version == version {
		t.Fatal("version did not invalidate raster")
	}
}

func TestImageDecodeFormats(t *testing.T) {
	pixels := knownImage().Pixels
	for _, format := range []string{"png", "jpeg", "gif", "bmp", "webp"} {
		t.Run(format, func(t *testing.T) {
			var encoded bytes.Buffer
			var err error
			wanted := pixels.Bounds()
			switch format {
			case "png":
				err = png.Encode(&encoded, pixels)
			case "jpeg":
				err = jpeg.Encode(&encoded, pixels, nil)
			case "gif":
				err = gif.Encode(&encoded, pixels, nil)
			case "bmp":
				err = bmp.Encode(&encoded, pixels)
			case "webp":
				fixture, decodeErr := base64.StdEncoding.DecodeString("UklGRhwAAABXRUJQVlA4TA8AAAAvAAAAAAcQ/Y/+ByKi/wEA")
				err = decodeErr
				encoded.Write(fixture)
				wanted = image.Rect(0, 0, 1, 1)
			}
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeImage(bytes.NewReader(encoded.Bytes()))
			if err != nil || decoded.Pixels.Bounds() != wanted {
				t.Fatalf("decode %v error %v", decoded, err)
			}
			path := filepath.Join(t.TempDir(), "tiny."+format)
			if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadImage(path)
			if err != nil || loaded.Pixels.Bounds() != wanted {
				t.Fatalf("load %v error %v", loaded, err)
			}
		})
	}
	if _, err := DecodeImage(bytes.NewBufferString("not an image")); err == nil {
		t.Fatal("invalid image accepted")
	}
	if _, err := LoadImage(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
}
