package widget

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"github.com/DonaldMurillo/gofastr-native/tui/cell"
	"github.com/DonaldMurillo/gofastr-native/tui/layout"
	"github.com/DonaldMurillo/gofastr-native/tui/view"
)

// FitMode controls how source pixels fit the image's allocated cell box.
type FitMode uint8

const (
	Contain FitMode = iota
	Cover
	Stretch
	None
)

// Image displays decoded pixels. Zero Width/Height use the intrinsic size,
// bounded by constraints; one explicit dimension derives the other by aspect
// ratio. Fit applies within the resulting box. Align positions both axes.
type Image struct {
	Source        *cell.Image
	Fit           FitMode
	Width, Height int
	Align         layout.Align
	Alt           string
}

type imageState struct {
	source        *cell.Image
	version       uint64
	crop          image.Rectangle
	width, height int
	raster        *cell.Image
}

func (Image) NewState() any { return &imageState{} }

// DecodeImage decodes PNG, JPEG, GIF's first frame, WebP, or BMP.
func DecodeImage(reader io.Reader) (*cell.Image, error) {
	pixels, _, err := image.Decode(reader)
	if err != nil {
		return nil, err
	}
	return cell.NewImage(pixels), nil
}

// LoadImage opens and decodes an image, closing the file before returning.
func LoadImage(path string) (*cell.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return DecodeImage(file)
}

func (img Image) bounds() image.Rectangle {
	if img.Source == nil || img.Source.Pixels == nil {
		return image.Rectangle{}
	}
	return img.Source.Pixels.Bounds()
}

func (img Image) Layout(node *view.Node, constraints layout.Constraints) layout.Size {
	bounds := img.bounds()
	if bounds.Empty() {
		width, height := img.Width, img.Height
		if img.Alt != "" {
			if width <= 0 {
				width = node.Tree().Method.Width(img.Alt)
			}
			if height <= 0 {
				height = 1
			}
		}
		return constraints.Constrain(layout.Size{W: width, H: height})
	}
	cellWidth, cellHeight := (cell.Options{CellWidth: node.Tree().CellWidth, CellHeight: node.Tree().CellHeight}).CellPixels()
	width, height := img.Width, img.Height
	switch {
	case width <= 0 && height <= 0:
		scale := math.Min(1, math.Min(float64(constraints.MaxW*cellWidth)/float64(bounds.Dx()), float64(constraints.MaxH*cellHeight)/float64(bounds.Dy())))
		width = int(math.Ceil(float64(bounds.Dx()) * scale / float64(cellWidth)))
		height = int(math.Ceil(float64(bounds.Dy()) * scale / float64(cellHeight)))
	case height <= 0:
		height = (width*cellWidth*bounds.Dy() + cellHeight*bounds.Dx() - 1) / (cellHeight * bounds.Dx())
	case width <= 0:
		width = (height*cellHeight*bounds.Dx() + cellWidth*bounds.Dy() - 1) / (cellWidth * bounds.Dy())
	}
	return constraints.Constrain(layout.Size{W: width, H: height})
}

func (img Image) geometry(size layout.Size, cellWidth, cellHeight int) (layout.Rect, image.Rectangle) {
	crop := img.bounds()
	if crop.Empty() || size.W <= 0 || size.H <= 0 {
		return layout.Rect{}, image.Rectangle{}
	}
	rect := layout.Rect{W: size.W, H: size.H}
	switch img.Fit {
	case Cover:
		if crop.Dx()*size.H*cellHeight > crop.Dy()*size.W*cellWidth {
			width := max(crop.Dy()*size.W*cellWidth/(size.H*cellHeight), 1)
			crop.Min.X += img.Align.Offset(crop.Dx(), width)
			crop.Max.X = crop.Min.X + width
		} else {
			height := max(crop.Dx()*size.H*cellHeight/(size.W*cellWidth), 1)
			crop.Min.Y += img.Align.Offset(crop.Dy(), height)
			crop.Max.Y = crop.Min.Y + height
		}
	case Stretch:
	case None:
		rect.W = (crop.Dx() + cellWidth - 1) / cellWidth
		rect.H = (crop.Dy() + cellHeight - 1) / cellHeight
	default:
		scale := math.Min(float64(size.W*cellWidth)/float64(crop.Dx()), float64(size.H*cellHeight)/float64(crop.Dy()))
		rect.W = min(size.W, max(int(math.Round(float64(crop.Dx())*scale/float64(cellWidth))), 1))
		rect.H = min(size.H, max(int(math.Round(float64(crop.Dy())*scale/float64(cellHeight))), 1))
	}
	rect.X, rect.Y = img.Align.Offset(size.W, rect.W), img.Align.Offset(size.H, rect.H)
	return rect, crop
}

func (state *imageState) scaled(source *cell.Image, crop image.Rectangle, width, height int) *cell.Image {
	if state.source != source || state.version != source.Version || state.crop != crop || state.width != width || state.height != height {
		pixels := source.Raster(crop, width, height)
		if state.raster == nil {
			state.raster = cell.NewImage(pixels)
		} else {
			state.raster.SetPixels(pixels)
		}
		state.source, state.version, state.crop, state.width, state.height = source, source.Version, crop, width, height
	}
	return state.raster
}

func (img Image) Draw(node *view.Node, canvas *view.Canvas) {
	cellWidth, cellHeight := canvas.CellPixels()
	rect, crop := img.geometry(canvas.Size(), cellWidth, cellHeight)
	if rect.Empty() || crop.Empty() {
		canvas.Text(0, 0, img.Alt, cell.Style{})
		return
	}
	state := node.State().(*imageState)
	if canvas.ImageProtocol() != cell.ImageNone {
		if img.Fit == Stretch && canvas.ImageProtocol() == cell.ImageKitty {
			scaled := state.scaled(img.Source, crop, rect.W*cellWidth, rect.H*cellHeight)
			canvas.PlaceImage(scaled, rect, scaled.Pixels.Bounds())
		} else {
			canvas.PlaceImage(img.Source, rect, crop)
		}
		return
	}
	pixels := state.scaled(img.Source, crop, rect.W, rect.H*2).Pixels
	visible := node.Visible()
	local := layout.Rect{X: visible.X - node.Rect().X, Y: visible.Y - node.Rect().Y, W: visible.W, H: visible.H}.Intersect(rect)
	for row := local.Y; row < local.Y+local.H; row++ {
		for col := local.X; col < local.X+local.W; col++ {
			topRed, topGreen, topBlue, _ := pixels.At(col-rect.X, (row-rect.Y)*2).RGBA()
			bottomRed, bottomGreen, bottomBlue, _ := pixels.At(col-rect.X, (row-rect.Y)*2+1).RGBA()
			canvas.Set(col, row, "▀", 1, cell.Style{Fg: cell.RGB(uint8(topRed>>8), uint8(topGreen>>8), uint8(topBlue>>8)), Bg: cell.RGB(uint8(bottomRed>>8), uint8(bottomGreen>>8), uint8(bottomBlue>>8))})
		}
	}
}
