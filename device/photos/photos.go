// Package photos defines bounded, metadata-free photo acquisition requests.
package photos

import (
	"bytes"
	"errors"
	"image/jpeg"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr-mobile/device/files"
)

// Source identifies a user-initiated photo acquisition service, not a permission.
type Source string

const (
	Library Source = "library"
	Camera  Source = "camera"
)

const MaxDimension = 2048

// Request asks a renderer for one normalized JPEG, never an original asset URL.
type Request struct {
	Title        string `json:"title"`
	Source       Source `json:"source"`
	MaxBytes     int    `json:"maxBytes"`
	MaxDimension int    `json:"maxDimension"`
}

// Validate checks the compiled photo service and transfer/decode limits.
func (request Request) Validate() error {
	if request.Source != Library && request.Source != Camera {
		return errors.New("photos: choose the library or camera source")
	}
	if request.Title == "" || len(request.Title) > 200 || request.MaxBytes < 1 || request.MaxBytes > files.MaxBytes || request.MaxDimension < 1 || request.MaxDimension > MaxDimension {
		return errors.New("photos: invalid title or image limits")
	}
	return nil
}

// Accept verifies JPEG bytes and bounded decoded dimensions, not just a label.
func (request Request) Accept(document files.Document) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := document.Validate(); err != nil {
		return err
	}
	extension := strings.ToLower(filepath.Ext(document.Name))
	if document.MediaType != "image/jpeg" || (extension != ".jpg" && extension != ".jpeg") || len(document.Data) > request.MaxBytes {
		return errors.New("photos: a bounded JPEG photo is required")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(document.Data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > request.MaxDimension || config.Height > request.MaxDimension {
		return errors.New("photos: invalid JPEG or photo dimensions exceed the request")
	}
	if _, err := jpeg.Decode(bytes.NewReader(document.Data)); err != nil {
		return errors.New("photos: the photo content is not a complete JPEG")
	}
	return nil
}
