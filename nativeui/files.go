package nativeui

import (
	"context"
	"errors"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/photos"
)

// FileRequest asks a compiled renderer to acquire or export one bounded document.
type FileRequest struct {
	ID     string          `json:"id"`
	Pick   *files.Pick     `json:"pick,omitempty"`
	Photo  *photos.Request `json:"photo,omitempty"`
	Export *files.Document `json:"export,omitempty"`
	Commit bool            `json:"commit,omitempty"`
}

// FileResult reports an observed picker/export outcome, never an OS path.
type FileResult struct {
	Status EffectStatus    `json:"status"`
	File   *files.Document `json:"file,omitempty"`
}

// FileRespond handles a completed file/photo pick. Cancellation never invokes it.
type FileRespond func(context.Context, Request, FileResult) (Result, error)

func validateFiles(request FileRequest, continuations map[string]FileRespond) error {
	operations := 0
	for _, present := range []bool{request.Pick != nil, request.Photo != nil, request.Export != nil} {
		if present {
			operations++
		}
	}
	if request.ID == "" || len(request.ID) > 128 || operations != 1 {
		return errors.New("nativeui: exactly one file operation is required")
	}
	if request.Pick != nil || request.Photo != nil {
		if continuations[request.ID] == nil {
			return errors.New("nativeui: file continuation is not registered")
		}
		if request.Photo != nil {
			return request.Photo.Validate()
		}
		return request.Pick.Validate()
	}
	return request.Export.Validate()
}

func (request FileRequest) capability() Capability {
	if request.Photo != nil {
		if request.Photo.Source == photos.Camera {
			return CapabilityCamera
		}
		return CapabilityPhotos
	}
	return CapabilityFiles
}
