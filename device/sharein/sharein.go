// Package sharein defines an explicit inbox for content shared by other apps.
// Reading does not consume an item. Applications acknowledge after they have
// accepted or discarded it, and use Identifier to deduplicate uncertain saves.
package sharein

import (
	"errors"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
)

const (
	Next         = "next"
	Acknowledge  = "acknowledge"
	MaxItems     = 16
	MaxFiles     = sharing.MaxFiles
	MaxBytes     = sharing.MaxBytes
	MaxTextBytes = sharing.MaxTextBytes
)

type Request struct {
	Operation  string `json:"operation"`
	Identifier string `json:"identifier,omitempty"`
}

// Item contains copied transfer values, never a provider URI or filesystem path.
type Item struct {
	Identifier string           `json:"identifier"`
	Title      string           `json:"title,omitempty"`
	Text       string           `json:"text,omitempty"`
	Files      []files.Document `json:"files,omitempty"`
}

func validIdentifier(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (request Request) Validate() error {
	if request.Operation == Next && request.Identifier == "" || request.Operation == Acknowledge && validIdentifier(request.Identifier) {
		return nil
	}
	return errors.New("sharein: next requires no identifier; acknowledge requires an inbox identifier")
}

func (item Item) Validate() error {
	if !validIdentifier(item.Identifier) {
		return errors.New("sharein: invalid inbox identifier")
	}
	return (sharing.Request{Title: item.Title, Text: item.Text, Files: item.Files}).Validate()
}

// Accept rejects a payload on acknowledgement. A nil next result means empty.
func (request Request) Accept(item *Item) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if item == nil {
		return nil
	}
	if request.Operation != Next {
		return errors.New("sharein: acknowledgement cannot return content")
	}
	return item.Validate()
}
