// Package photos registers the compiled photo acquisition extension.
package photos

import (
	"embed"
	"encoding/json"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	policy "github.com/DonaldMurillo/gofastr-native/device/photos"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/webassets"
)

//go:embed gofastr-native.extension.json *.swift *.java photos.js
var assets embed.FS

func init() { webassets.Register(Definition()) }

type Request = policy.Request
type Source = policy.Source

const (
	Library      = policy.Library
	Camera       = policy.Camera
	MaxDimension = policy.MaxDimension
)

// Acquire builds a typed operation; application code never supplies script names.
func Acquire(request Request) (*extension.FileOperation, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return &extension.FileOperation{Name: "gofastr.photos/v1", Payload: string(payload)}, nil
}

func request(operation extension.FileOperation) (Request, error) {
	var request Request
	if err := extension.Decode(operation.Payload, &request); err != nil {
		return request, err
	}
	return request, request.Validate()
}

// Definition supplies immutable bundled assets and authoritative Go validators.
func Definition() extension.Definition {
	content, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(content)
	return extension.Definition{Manifest: manifest, Assets: assets,
		Validate: func(operation extension.FileOperation) (string, error) {
			request, err := request(operation)
			if request.Source == Camera {
				return "camera/v1", err
			}
			return "photos/v1", err
		},
		Accept: func(operation extension.FileOperation, document files.Document) error {
			request, err := request(operation)
			if err != nil {
				return err
			}
			return request.Accept(document)
		},
	}
}
