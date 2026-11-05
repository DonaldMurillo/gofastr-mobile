package extension_test

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extensions/photos"
)

func TestRegistryCopiesAssetsAndMetadata(t *testing.T) {
	definition := photos.Definition()
	registry, err := extension.New(definition)
	if err != nil {
		t.Fatal(err)
	}
	definition.Manifest.Capabilities[0] = "changed/v1"
	returned := registry.Definitions()
	returned[0].Manifest.IOS.Sources[0] = "changed.swift"
	content, _ := returned[0].Assets.ReadFile("photos.js")
	content[0] = 0
	current, _ := registry.Definitions()[0].Assets.ReadFile("photos.js")
	opened, err := returned[0].Assets.Open("photos.js")
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	read, err := io.ReadAll(opened)
	if err != nil || string(read) != string(current) {
		t.Fatal("copied assets do not implement the read-only FS contract", err)
	}
	if !registry.Allows("photos/v1") || registry.Allows("changed/v1") || current[0] == 0 || registry.Definitions()[0].Manifest.IOS.Sources[0] == "changed.swift" {
		t.Fatal("registry exposes mutable compiled registration")
	}
}

func TestRegistryRejectsInvalidRegistrationsAndOperations(t *testing.T) {
	definition := photos.Definition()
	if _, err := extension.New(definition, definition); err == nil {
		t.Fatal("duplicate accepted")
	}
	for _, mutate := range []func(*extension.Definition){
		func(value *extension.Definition) { value.Manifest.Name = "../injected/v1" },
		func(value *extension.Definition) { value.Manifest.IOS.Factory = "Injected.self);exit(0)//" },
		func(value *extension.Definition) { value.Manifest.IOS.Sources = []string{"../escape.swift"} },
		func(value *extension.Definition) { value.Manifest.IOS.Sources = []string{"photos.js"} },
		func(value *extension.Definition) { value.Manifest.WebView.Script = "https://remote/script.js" },
		func(value *extension.Definition) { value.Manifest.Capabilities = []string{"photos/v1", "photos/v1"} },
		func(value *extension.Definition) { value.Validate = nil },
		func(value *extension.Definition) { value.Assets = fstest.MapFS{} },
	} {
		candidate := photos.Definition()
		mutate(&candidate)
		if _, err := extension.New(candidate); err == nil {
			t.Fatal("invalid definition accepted")
		}
	}
	registry, _ := extension.New(photos.Definition())
	valid, _ := photos.Acquire(photos.Request{Title: "Photo", Source: photos.Library, MaxBytes: files.MaxBytes, MaxDimension: 2048})
	if capability, err := registry.Validate(*valid); err != nil || capability != "photos/v1" {
		t.Fatal(capability, err)
	}
	for _, operation := range []extension.FileOperation{
		{Name: "unregistered/v1", Payload: valid.Payload},
		{Name: valid.Name, Payload: strings.Repeat(" ", extension.MaxPayload+1)},
		{Name: valid.Name, Payload: valid.Payload + " {}"},
		{Name: valid.Name, Payload: `{"title":"Photo","source":"library","maxBytes":100,"maxDimension":20,"arbitrary":true}`},
	} {
		if _, err := registry.Validate(operation); err == nil {
			t.Fatal("invalid operation accepted")
		}
	}
	if err := registry.Accept(*valid, files.Document{Name: "fake.jpg", MediaType: "image/jpeg", Data: []byte("fake")}); err == nil {
		t.Fatal("extension result validator bypassed")
	}
}

func TestManifestCannotDisagreeWithEmbeddedRegistration(t *testing.T) {
	definition := photos.Definition()
	definition.Manifest.Capabilities = []string{"invented/v1"}
	if _, err := extension.New(definition); err == nil {
		t.Fatal("manifest disagreement accepted")
	}
	content, _ := json.Marshal(photos.Definition().Manifest)
	if _, err := extension.ParseManifest(append(content, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing manifest content accepted")
	}
}
