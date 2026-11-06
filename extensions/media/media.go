// Package media provides a Go-authored attachment rendering component with
// compiled UIKit and WebView adapters. Applications never author adapter code.
package media

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	"github.com/DonaldMurillo/gofastr-native/behavior"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/webassets"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

const Name = "gofastr.media/v1"

// Content combines private thumbnails, attachment rows, and an optional detail
// preview. It is a snapshot, not editor state or an arbitrary layout tree.
type Content = nativeui.Media

//go:embed gofastr-native.extension.json GofastrMedia.swift *.java media.js
var assets embed.FS

func init() { webassets.Register(Definition()) }

// Block freezes typed content and a local navigation allowlist into a keyed
// rendering component. Renderers preserve the same ID while content updates.
func Block(id string, content Content) (extension.Component, error) {
	payload, err := json.Marshal(content)
	if err != nil {
		return extension.Component{}, err
	}
	component := extension.Component{ID: id, Name: Name, Payload: string(payload), Destinations: destinations(content)}
	if err := validate(component); err != nil {
		return extension.Component{}, err
	}
	return component, nil
}

// Definition registers library-owned source and authoritative content policy.
func Definition() extension.Definition {
	content, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(content)
	return extension.Definition{Manifest: manifest, Assets: assets, ValidateComponent: validate}
}

func validate(component extension.Component) error {
	if component.Name != Name || !behavior.SafeToken(component.ID) {
		return errors.New("media: invalid component identity")
	}
	var content Content
	if err := extension.DecodeComponent(component.Payload, &content); err != nil {
		return err
	}
	if len(content.Images)+len(content.Attachments) > 64 || len(content.Images)+len(content.Attachments) == 0 && content.Preview == nil {
		return errors.New("media: expected 1 to 64 media items or a detail preview")
	}
	if err := content.Validate(); err != nil {
		return err
	}
	if !maps.Equal(component.Destinations, destinations(content)) {
		return errors.New("media: navigation events do not match the content")
	}
	return nil
}

func destinations(content Content) map[string]string {
	result := map[string]string{}
	for index, image := range content.Images {
		if image.Destination != "" {
			result[fmt.Sprintf("image.%d", index)] = image.Destination
		}
	}
	for index, attachment := range content.Attachments {
		result[fmt.Sprintf("file.%d", index)] = attachment.Destination
	}
	if content.Preview != nil {
		result["preview"] = content.Preview.File.Destination
	}
	return result
}
