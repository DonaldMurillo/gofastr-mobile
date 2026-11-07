package cell

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strconv"
)

type imageTransmission struct{ version, used uint64 }

// CellPixels returns configured cell pixels, or the portable 10 by 20 default.
func (o Options) CellPixels() (int, int) {
	if o.CellWidth <= 0 || o.CellHeight <= 0 {
		return 10, 20
	}
	return o.CellWidth, o.CellHeight
}

func sameImages(before, after []ImagePlacement) bool {
	if len(before) != len(after) {
		return false
	}
	for index := range before {
		if before[index] != after[index] {
			return false
		}
	}
	return true
}

func hasPlacement(placements []ImagePlacement, wanted ImagePlacement) bool {
	for _, placement := range placements {
		if placement == wanted {
			return true
		}
	}
	return false
}

func (r *Renderer) kitty(control string) {
	r.out = append(r.out, "\x1b_G"...)
	r.out = append(r.out, control...)
	r.out = append(r.out, "\x1b\\"...)
}

func (r *Renderer) deleteKitty(placement ImagePlacement) {
	r.kitty("a=d,d=i,q=2,i=" + strconv.FormatUint(uint64(placement.Image.ID), 10) + ",p=" + strconv.FormatUint(uint64(placement.ID), 10))
}

func (r *Renderer) prepareImages(back *Buffer) {
	r.imageFrame++
	changedProtocol := r.imageProtocol != r.options.Images
	r.rasterChanged = r.imageFull || changedProtocol || !sameImages(r.placements, back.images)
	if r.imageProtocol == ImageKitty {
		for _, placement := range r.placements {
			if r.imageFull || changedProtocol || !hasPlacement(back.images, placement) {
				r.deleteKitty(placement)
			}
		}
	}
	if r.imageProtocol != ImageKitty && r.imageProtocol != ImageNone && r.rasterChanged && !r.imageFull {
		for _, placement := range r.placements {
			r.eraseImage(placement.Rect)
		}
	}
	if !r.imageFull && r.options.Images != ImageNone {
		for _, placement := range back.images {
			if !hasPlacement(r.placements, placement) {
				r.eraseImage(placement.Rect)
			}
		}
	}
	if r.imageFull || changedProtocol {
		for id := range r.transmitted {
			r.kitty("a=d,d=I,q=2,i=" + strconv.FormatUint(uint64(id), 10))
			delete(r.transmitted, id)
		}
	}
}

func (r *Renderer) eraseImage(rect image.Rectangle) {
	r.setPen(Style{})
	for row := max(rect.Min.Y, 0); row < min(rect.Max.Y, r.front.height); row++ {
		r.moveTo(rect.Min.X, row, nil, nil)
		r.out = append(r.out, "\x1b["...)
		r.out = strconv.AppendInt(r.out, int64(rect.Dx()), 10)
		r.out = append(r.out, 'X')
		for col := rect.Min.X; col < rect.Max.X; col++ {
			r.front.cells[row*r.front.width+col] = Cell{}
		}
	}
}

func (r *Renderer) renderImages(back *Buffer) {
	for _, placement := range back.images {
		switch r.options.Images {
		case ImageKitty:
			if r.transmitted == nil {
				r.transmitted = make(map[uint32]imageTransmission)
			}
			cached, exists := r.transmitted[placement.Image.ID]
			if !exists || cached.version != placement.Version {
				r.transmitKitty(placement.Image)
			}
			r.transmitted[placement.Image.ID] = imageTransmission{placement.Version, r.imageFrame}
			if r.imageFull || r.imageProtocol != ImageKitty || !hasPlacement(r.placements, placement) {
				r.moveTo(placement.Rect.Min.X, placement.Rect.Min.Y, nil, nil)
				r.out = append(r.out, "\x1b_Ga=p,q=2,C=1,z=1"...)
				r.number(",i=", int(placement.Image.ID))
				r.number(",p=", int(placement.ID))
				bounds := placement.Image.Pixels.Bounds()
				r.number(",x=", placement.Crop.Min.X-bounds.Min.X)
				r.number(",y=", placement.Crop.Min.Y-bounds.Min.Y)
				r.number(",w=", placement.Crop.Dx())
				r.number(",h=", placement.Crop.Dy())
				r.number(",c=", placement.Rect.Dx())
				r.number(",r=", placement.Rect.Dy())
				r.out = append(r.out, "\x1b\\"...)
			}
		case ImageSixel, ImageITerm:
			if r.rasterChanged {
				r.inlineImage(placement)
			}
		}
	}
	for id, cached := range r.transmitted {
		if r.imageFrame-cached.used >= 3 {
			r.kitty("a=d,d=I,q=2,i=" + strconv.FormatUint(uint64(id), 10))
			delete(r.transmitted, id)
		}
	}
	clear(r.placements)
	r.placements = append(r.placements[:0], back.images...)
	r.imageFull = false
	r.imageProtocol = r.options.Images
}

func (r *Renderer) number(prefix string, number int) {
	r.out = append(r.out, prefix...)
	r.out = strconv.AppendInt(r.out, int64(number), 10)
}

func imagePNG(pixels image.Image) []byte {
	var encoded bytes.Buffer
	_ = png.Encode(&encoded, pixels)
	return encoded.Bytes()
}

func (r *Renderer) transmitKitty(img *Image) {
	encoded := base64.StdEncoding.EncodeToString(imagePNG(img.Pixels))
	for offset := 0; offset < len(encoded); offset += 4096 {
		end := min(offset+4096, len(encoded))
		r.out = append(r.out, "\x1b_G"...)
		if offset == 0 {
			r.out = append(r.out, "a=t,t=d,f=100,q=2"...)
			r.number(",i=", int(img.ID))
		}
		if offset == 0 {
			r.number(",m=", 0)
		} else {
			r.out = append(r.out, "q=2,"...)
			r.number("m=", 0)
		}
		if end < len(encoded) {
			r.out[len(r.out)-1] = '1'
		}
		r.out = append(r.out, ';')
		r.out = append(r.out, encoded[offset:end]...)
		r.out = append(r.out, "\x1b\\"...)
	}
}

func (r *Renderer) inlineImage(placement ImagePlacement) {
	cellWidth, cellHeight := r.options.CellPixels()
	pixels := placement.Image.Raster(placement.Crop, placement.Rect.Dx()*cellWidth, placement.Rect.Dy()*cellHeight)
	r.moveTo(placement.Rect.Min.X, placement.Rect.Min.Y, nil, nil)
	if r.options.Images == ImageITerm {
		r.out = append(r.out, "\x1b]1337;File=inline=1;preserveAspectRatio=0;doNotMoveCursor=1"...)
		r.number(";width=", placement.Rect.Dx())
		r.number(";height=", placement.Rect.Dy())
		r.out = append(r.out, ':')
		r.out = base64.StdEncoding.AppendEncode(r.out, imagePNG(pixels))
		r.out = append(r.out, '\a')
	} else {
		r.sixel(pixels)
	}
	r.cursorValid, r.last = false, ""
}

func (r *Renderer) sixel(pixels *image.NRGBA) {
	r.out = append(r.out, "\x1bP0;1;0q\"1;1"...)
	r.number(";", pixels.Bounds().Dx())
	r.number(";", pixels.Bounds().Dy())
	var palette [216]bool
	indexAt := func(col, row int) int {
		pixel := pixels.NRGBAAt(col, row)
		if pixel.A < 128 {
			return -1
		}
		return int(pixel.R)*5/255*36 + int(pixel.G)*5/255*6 + int(pixel.B)*5/255
	}
	for row := 0; row < pixels.Bounds().Dy(); row++ {
		for col := 0; col < pixels.Bounds().Dx(); col++ {
			if index := indexAt(col, row); index >= 0 {
				palette[index] = true
			}
		}
	}
	for index, used := range palette {
		if !used {
			continue
		}
		r.number("#", index)
		r.out = append(r.out, ";2"...)
		r.number(";", index/36*20)
		r.number(";", index/6%6*20)
		r.number(";", index%6*20)
	}
	for band := 0; band < pixels.Bounds().Dy(); band += 6 {
		for index, used := range palette {
			if !used {
				continue
			}
			r.number("#", index)
			for col := 0; col < pixels.Bounds().Dx(); col++ {
				bits := byte(0)
				for bit := 0; bit < 6 && band+bit < pixels.Bounds().Dy(); bit++ {
					if indexAt(col, band+bit) == index {
						bits |= 1 << bit
					}
				}
				r.out = append(r.out, '?'+bits)
			}
			r.out = append(r.out, '$')
		}
		if band+6 < pixels.Bounds().Dy() {
			r.out = append(r.out, '-')
		}
	}
	r.out = append(r.out, "\x1b\\"...)
}
