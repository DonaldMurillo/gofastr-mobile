// Package sharing describes bounded user-initiated sharing and clipboard writes.
package sharing

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-mobile/device/files"
)

const (
	MaxFiles     = 4
	MaxTextBytes = 64 << 10
	MaxBytes     = files.MaxBytes
)

// Request contains transfer values, never arbitrary OS paths or remote URLs.
type Request struct {
	Title string           `json:"title,omitempty"`
	Text  string           `json:"text,omitempty"`
	Files []files.Document `json:"files,omitempty"`
}

// Clipboard is write-only and must be admitted by an explicit user action.
type Clipboard struct {
	Text string `json:"text"`
}

// Validate checks text and combined decoded document bytes.
func (request Request) Validate() error {
	if len(request.Title) > 200 || !utf8.ValidString(request.Title) || strings.ContainsFunc(request.Title, unicode.IsControl) || !validText(request.Text) || len(request.Files) > MaxFiles || request.Text == "" && len(request.Files) == 0 {
		return errors.New("sharing: invalid title, text or file count")
	}
	total := 0
	for _, document := range request.Files {
		if err := document.Validate(); err != nil {
			return err
		}
		total += len(document.Data)
	}
	if total > MaxBytes {
		return errors.New("sharing: combined documents must be 1 MiB or smaller")
	}
	return nil
}

// Validate bounds an explicit clipboard write; empty text intentionally clears it.
func (clipboard Clipboard) Validate() error {
	if !validText(clipboard.Text) {
		return errors.New("sharing: clipboard text must be valid UTF-8 and at most 64 KiB")
	}
	return nil
}

func validText(text string) bool {
	return len(text) <= MaxTextBytes && utf8.ValidString(text) && !strings.ContainsRune(text, '\x00')
}
