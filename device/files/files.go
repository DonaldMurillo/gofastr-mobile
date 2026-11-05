// Package files supplies bounded documents and private, opaque attachment storage.
package files

import (
	"encoding/json"
	"errors"
	"mime"
	"path/filepath"
	"strings"
	"unicode"
)

const MaxBytes = 1 << 20

// Document is a small transfer value, never a filesystem path or remote URL.
type Document struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Data      []byte `json:"data"`
}

// MarshalJSON keeps empty bytes a base64 string rather than a JSON null.
func (document Document) MarshalJSON() ([]byte, error) {
	type wireDocument Document
	if document.Data == nil {
		document.Data = []byte{}
	}
	return json.Marshal(wireDocument(document))
}

// Pick describes a single-file request. Extensions include their leading dot.
type Pick struct {
	Title      string   `json:"title"`
	Extensions []string `json:"extensions,omitempty"`
	MaxBytes   int      `json:"maxBytes"`
}

// File describes an app-private attachment. Handle is not an OS path.
type File struct {
	Handle    string `json:"handle"`
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
}

// Validate rejects unsafe labels and transfers outside the prototype limit.
func (document Document) Validate() error {
	if document.Name == "" || len(document.Name) > 200 || document.Name == "." || document.Name == ".." || strings.ContainsAny(document.Name, `/\\`) || strings.ContainsFunc(document.Name, unicode.IsControl) {
		return errors.New("files: a plain filename is required")
	}
	if len(document.Data) > MaxBytes {
		return errors.New("files: documents must be 1 MiB or smaller")
	}
	if len(document.MediaType) > 128 {
		return errors.New("files: invalid media type")
	}
	if mediaType, _, err := mime.ParseMediaType(document.MediaType); err != nil || !strings.Contains(mediaType, "/") {
		return errors.New("files: invalid media type")
	}
	return nil
}

// Validate checks bounded, extension-based picker policy.
func (pick Pick) Validate() error {
	if pick.Title == "" || len(pick.Title) > 200 || pick.MaxBytes < 1 || pick.MaxBytes > MaxBytes || len(pick.Extensions) > 16 {
		return errors.New("files: invalid picker limits")
	}
	for _, extension := range pick.Extensions {
		if len(extension) < 2 || len(extension) > 16 || extension[0] != '.' || strings.ContainsAny(extension[1:], `./\\,; `) || strings.ContainsFunc(extension, unicode.IsControl) {
			return errors.New("files: invalid picker extension")
		}
	}
	return nil
}

// Accept verifies returned bytes and filename; a picker filter is not validation.
func (pick Pick) Accept(document Document) error {
	if err := document.Validate(); err != nil {
		return err
	}
	if len(document.Data) > pick.MaxBytes {
		return errors.New("files: the selected file is too large")
	}
	if len(pick.Extensions) == 0 {
		return nil
	}
	for _, extension := range pick.Extensions {
		if strings.EqualFold(filepath.Ext(document.Name), extension) {
			return nil
		}
	}
	return errors.New("files: the selected file type is not allowed")
}
