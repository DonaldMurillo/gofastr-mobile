package extension_test

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

func componentDefinition() extension.Definition {
	manifest := extension.Manifest{Name: "example.text/v1", Kind: extension.ComponentKind, Capabilities: []string{"example.text/v1"}, IOS: extension.IOS{Factory: "ExampleTextComponent", Sources: []string{"Text.swift"}}, WebView: extension.WebView{Script: "text.js"}}
	encoded, _ := json.Marshal(manifest)
	return extension.Definition{Manifest: manifest, Assets: fstest.MapFS{
		extension.ManifestName: {Data: encoded}, "Text.swift": {Data: []byte("import UIKit")}, "text.js": {Data: []byte("/* compiled test fixture */")},
	}, ValidateComponent: func(component extension.Component) error {
		var payload struct {
			Text string `json:"text"`
		}
		return extension.DecodeComponent(component.Payload, &payload)
	}}
}

func TestRenderingRegistrationAndBounds(t *testing.T) {
	definition := componentDefinition()
	registry, err := extension.New(definition)
	if err != nil {
		t.Fatal(err)
	}
	valid := extension.Component{ID: "message", Name: definition.Manifest.Name, Payload: `{"text":"Hello"}`, Destinations: map[string]string{"detail": "/detail"}}
	if err := registry.ValidateComponent(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*extension.Component){
		func(component *extension.Component) { component.ID = "../bad" },
		func(component *extension.Component) { component.Name = "missing/v1" },
		func(component *extension.Component) { component.Payload = `{"text":"ok","html":"<script>"}` },
		func(component *extension.Component) { component.Payload += ` {}` },
		func(component *extension.Component) {
			component.Payload = strings.Repeat("x", extension.MaxComponentPayload+1)
		},
		func(component *extension.Component) { component.Destinations = map[string]string{"../bad": "/detail"} },
	} {
		candidate := valid
		change(&candidate)
		if err := registry.ValidateComponent(candidate); err == nil {
			t.Fatal("invalid rendering component accepted")
		}
	}
	if _, err := registry.Validate(extension.FileOperation{Name: valid.Name, Payload: valid.Payload}); err == nil {
		t.Fatal("component dispatched as file acquisition")
	}
	definition.ValidateComponent = nil
	if _, err := extension.New(definition); err == nil {
		t.Fatal("component without validator accepted")
	}
}

func TestRenderingManifestRequiresDistinctCapabilityAndKind(t *testing.T) {
	for _, change := range []func(*extension.Manifest){
		func(manifest *extension.Manifest) { manifest.Kind = "unknown" },
		func(manifest *extension.Manifest) { manifest.Capabilities = []string{"files/v1"} },
		func(manifest *extension.Manifest) { manifest.Capabilities = append(manifest.Capabilities, "other/v1") },
	} {
		manifest := componentDefinition().Manifest
		change(&manifest)
		if err := manifest.Validate(); err == nil {
			t.Fatal("invalid rendering manifest accepted")
		}
	}
}
