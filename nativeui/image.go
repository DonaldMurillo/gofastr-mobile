package nativeui

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
)

// Image is a bounded JPEG preview, never a filesystem path or remote URL.
// Destination optionally opens a registered screen with the original attachment.
// Images supplement a screen's form or text; they are not editor values.
type Image struct {
	ID          string `json:"id"`
	Alt         string `json:"alt"`
	Data        []byte `json:"data"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Destination string `json:"destination,omitempty"`
}

const maxPreviewBytes = 128 << 10

// NewImage decodes a bounded JPEG/PNG and reencodes a metadata-free preview.
// maxDimension must be between 1 and 1024; sources are limited to 1 MiB and
// 2048 pixels per edge. Original attachment bytes remain in application storage.
func NewImage(id, alt string, data []byte, maxDimension int) (Image, error) {
	if id == "" || alt == "" || len(data) > 1<<20 || maxDimension < 1 || maxDimension > 1024 {
		return Image{}, errors.New("nativeui: invalid image preview request")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") || config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
		return Image{}, errors.New("nativeui: unsupported or oversized image")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, errors.New("nativeui: incomplete image")
	}
	width, height := config.Width, config.Height
	if width > maxDimension || height > maxDimension {
		if width >= height {
			height, width = max(1, height*maxDimension/width), maxDimension
		} else {
			width, height = max(1, width*maxDimension/height), maxDimension
		}
	}
	preview := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(preview, preview.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(preview, preview.Bounds(), source, source.Bounds(), draw.Over, nil)
	for _, quality := range []int{82, 65, 45, 25} {
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, preview, &jpeg.Options{Quality: quality}); err != nil {
			return Image{}, err
		}
		if encoded.Len() <= maxPreviewBytes {
			return Image{ID: id, Alt: alt, Data: encoded.Bytes(), Width: width, Height: height}, nil
		}
	}
	return Image{}, errors.New("nativeui: image preview exceeds byte limit")
}

func validateImages(images []Image) error {
	if len(images) > 64 {
		return errors.New("nativeui: too many image previews")
	}
	seen := make(map[string]bool, len(images))
	for _, preview := range images {
		if preview.ID == "" || preview.Alt == "" || seen[preview.ID] || len(preview.Data) > maxPreviewBytes || preview.Width < 1 || preview.Height < 1 || preview.Width > 1024 || preview.Height > 1024 {
			return errors.New("nativeui: invalid image preview")
		}
		config, err := jpeg.DecodeConfig(bytes.NewReader(preview.Data))
		if err != nil || config.Width != preview.Width || config.Height != preview.Height {
			return errors.New("nativeui: invalid image preview data")
		}
		seen[preview.ID] = true
	}
	return nil
}
