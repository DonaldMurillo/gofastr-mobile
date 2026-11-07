package cell

import (
	"image"
	"image/color"
	"math"
	"sync/atomic"
)

// ImageProtocol selects a terminal pixel protocol; ImageNone uses text cells.
type ImageProtocol uint8

const (
	ImageNone ImageProtocol = iota
	ImageKitty
	ImageSixel
	ImageITerm
)

func (p ImageProtocol) String() string {
	switch p {
	case ImageKitty:
		return "kitty"
	case ImageSixel:
		return "sixel"
	case ImageITerm:
		return "iTerm2"
	default:
		return "half-block"
	}
}

var imageIDs atomic.Uint32

// Image holds decoded pixels and a stable terminal ID. Keep Pixels immutable
// between SetPixels calls. Like Buffer, Image is owned by the app loop.
type Image struct {
	Pixels  image.Image
	ID      uint32
	Version uint64
}

// NewImage assigns decoded pixels a stable, process-local terminal ID.
func NewImage(pixels image.Image) *Image {
	return &Image{Pixels: pixels, ID: imageIDs.Add(1), Version: 1}
}

// SetPixels replaces the pixels and invalidates cached terminal data.
func (img *Image) SetPixels(pixels image.Image) {
	img.Pixels = pixels
	img.Version++
}

// ImagePlacement is a visible, non-overlapping rectangle in a frame. Rect is
// in screen cells; Crop uses the decoded image's pixel coordinates. Version
// snapshots Image.Version so changes remain detectable across frames.
type ImagePlacement struct {
	Image      *Image
	ID         uint32
	Version    uint64
	Rect, Crop image.Rectangle
}

// Images returns this frame's visible placements. The slice is read-only and
// remains valid only until the next buffer mutation.
func (b *Buffer) Images() []ImagePlacement { return b.images }

// ClipImage clips a cell rectangle and maps its edges into source pixels.
func ClipImage(rect, crop, clip image.Rectangle) (image.Rectangle, image.Rectangle) {
	visible := rect.Intersect(clip)
	if rect.Empty() || crop.Empty() || visible.Empty() {
		return image.Rectangle{}, image.Rectangle{}
	}
	return visible, image.Rect(
		crop.Min.X+(visible.Min.X-rect.Min.X)*crop.Dx()/rect.Dx(),
		crop.Min.Y+(visible.Min.Y-rect.Min.Y)*crop.Dy()/rect.Dy(),
		crop.Min.X+((visible.Max.X-rect.Min.X)*crop.Dx()+rect.Dx()-1)/rect.Dx(),
		crop.Min.Y+((visible.Max.Y-rect.Min.Y)*crop.Dy()+rect.Dy()-1)/rect.Dy())
}

// PlaceImage records an image and reserves its cells. A zero crop selects all
// pixels. Later text or images cut out the covered part of older placements.
func (b *Buffer) PlaceImage(img *Image, rect, crop image.Rectangle) {
	if img == nil || img.Pixels == nil {
		return
	}
	if crop.Empty() {
		crop = img.Pixels.Bounds()
	}
	crop = crop.Intersect(img.Pixels.Bounds())
	rect, crop = ClipImage(rect, crop, image.Rect(0, 0, b.width, b.height))
	if rect.Empty() || crop.Empty() {
		return
	}
	if img.ID == 0 {
		img.ID = imageIDs.Add(1)
	}
	b.Fill(rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy(), Style{})
	for row := rect.Min.Y; row < rect.Max.Y; row++ {
		for col := rect.Min.X; col < rect.Max.X; col++ {
			b.cells[row*b.width+col].Reserved = true
		}
	}
	b.nextImage++
	b.images = append(b.images, ImagePlacement{Image: img, ID: b.nextImage, Version: img.Version, Rect: rect, Crop: crop})
}

func (b *Buffer) cutImages(cut image.Rectangle) {
	for index := 0; index < len(b.images); {
		placement := b.images[index]
		overlap := placement.Rect.Intersect(cut)
		if overlap.Empty() {
			index++
			continue
		}
		copy(b.images[index:], b.images[index+1:])
		b.images[len(b.images)-1] = ImagePlacement{}
		b.images = b.images[:len(b.images)-1]
		pieces := [4]image.Rectangle{
			image.Rect(placement.Rect.Min.X, placement.Rect.Min.Y, placement.Rect.Max.X, overlap.Min.Y),
			image.Rect(placement.Rect.Min.X, overlap.Max.Y, placement.Rect.Max.X, placement.Rect.Max.Y),
			image.Rect(placement.Rect.Min.X, overlap.Min.Y, overlap.Min.X, overlap.Max.Y),
			image.Rect(overlap.Max.X, overlap.Min.Y, placement.Rect.Max.X, overlap.Max.Y),
		}
		for _, piece := range pieces {
			if piece.Empty() {
				continue
			}
			part := placement
			part.Rect, part.Crop = ClipImage(placement.Rect, placement.Crop, piece)
			b.nextImage++
			part.ID = b.nextImage
			b.images = append(b.images, part)
		}
	}
}

// Raster returns an area-averaged crop scaled to width by height pixels.
// Alpha is averaged premultiplied, then converted back to straight alpha.
func (img *Image) Raster(crop image.Rectangle, width, height int) *image.NRGBA {
	result := image.NewNRGBA(image.Rect(0, 0, max(width, 0), max(height, 0)))
	if img == nil || img.Pixels == nil || width <= 0 || height <= 0 {
		return result
	}
	crop = crop.Intersect(img.Pixels.Bounds())
	if crop.Empty() {
		return result
	}
	for row := 0; row < height; row++ {
		top := float64(crop.Min.Y) + float64(row)*float64(crop.Dy())/float64(height)
		bottom := float64(crop.Min.Y) + float64(row+1)*float64(crop.Dy())/float64(height)
		for col := 0; col < width; col++ {
			left := float64(crop.Min.X) + float64(col)*float64(crop.Dx())/float64(width)
			right := float64(crop.Min.X) + float64(col+1)*float64(crop.Dx())/float64(width)
			var red, green, blue, alpha float64
			for sourceY := int(math.Floor(top)); sourceY < int(math.Ceil(bottom)); sourceY++ {
				for sourceX := int(math.Floor(left)); sourceX < int(math.Ceil(right)); sourceX++ {
					weight := (math.Min(right, float64(sourceX+1)) - math.Max(left, float64(sourceX))) * (math.Min(bottom, float64(sourceY+1)) - math.Max(top, float64(sourceY)))
					rr, gg, bb, aa := img.Pixels.At(sourceX, sourceY).RGBA()
					red += float64(rr) * weight
					green += float64(gg) * weight
					blue += float64(bb) * weight
					alpha += float64(aa) * weight
				}
			}
			if alpha > 0 {
				area := (right - left) * (bottom - top)
				result.SetNRGBA(col, row, color.NRGBA{R: uint8(math.Round(red / alpha * 255)), G: uint8(math.Round(green / alpha * 255)), B: uint8(math.Round(blue / alpha * 255)), A: uint8(math.Round(alpha / area / 257))})
			}
		}
	}
	return result
}
