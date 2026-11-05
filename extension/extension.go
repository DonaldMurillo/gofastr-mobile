// Package extension defines compiled, versioned file-acquisition extensions.
package extension

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/files"
)

const ManifestName = "gofastr-native.extension.json"
const MaxPayload = 8192

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9.-]{0,79}/v[1-9][0-9]{0,2}$`)
var factoryPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,99}$`)

// FileOperation carries bounded JSON as a string so every renderer can decode it
// without introducing an untyped object tree into the shared wire contract.
type FileOperation struct {
	Name    string `json:"name"`
	Payload string `json:"payload"`
}

// Manifest identifies library-owned adapters compiled into the application.
type Manifest struct {
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	IOS          IOS      `json:"ios"`
	WebView      WebView  `json:"webview"`
}

// IOS names ordinary Swift sources and their compiled factory type.
type IOS struct {
	Factory string   `json:"factory"`
	Sources []string `json:"sources"`
}

// WebView names a bundled adapter, never a remote script URL.
type WebView struct {
	Script string `json:"script"`
}

// Definition binds typed request/result validation to bundled renderer assets.
// Validate returns one of the manifest's capabilities for this request.
type Definition struct {
	Manifest Manifest
	Assets   fs.ReadFileFS
	Validate func(FileOperation) (string, error)
	Accept   func(FileOperation, files.Document) error
}

// Decode rejects unknown fields and extra JSON values in a bounded payload.
func Decode(payload string, target any) error {
	if len(payload) == 0 || len(payload) > MaxPayload {
		return errors.New("extension: invalid payload size")
	}
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("extension: trailing payload content")
	}
	return nil
}

func ParseManifest(content []byte) (Manifest, error) {
	var manifest Manifest
	if err := Decode(string(content), &manifest); err != nil {
		return manifest, err
	}
	return manifest, manifest.Validate()
}

// Validate checks names and package-relative asset paths, not OS permissions.
func (manifest Manifest) Validate() error {
	if !namePattern.MatchString(manifest.Name) || len(manifest.Capabilities) == 0 || len(manifest.Capabilities) > 8 ||
		!factoryPattern.MatchString(manifest.IOS.Factory) || len(manifest.IOS.Sources) == 0 || len(manifest.IOS.Sources) > 16 {
		return errors.New("extension: invalid manifest")
	}
	seen := map[string]bool{}
	for _, capability := range manifest.Capabilities {
		if !namePattern.MatchString(capability) || seen[capability] {
			return errors.New("extension: invalid or duplicate capability")
		}
		seen[capability] = true
	}
	seen = map[string]bool{}
	for _, source := range manifest.IOS.Sources {
		if !strings.HasSuffix(source, ".swift") {
			return errors.New("extension: iOS assets must be Swift sources")
		}
	}
	if !strings.HasSuffix(manifest.WebView.Script, ".js") {
		return errors.New("extension: WebView asset must be JavaScript")
	}
	for _, source := range append(append([]string(nil), manifest.IOS.Sources...), manifest.WebView.Script) {
		if !fs.ValidPath(source) || strings.Contains(source, "\\") || seen[source] ||
			!(strings.HasSuffix(source, ".swift") || source == manifest.WebView.Script && strings.HasSuffix(source, ".js")) {
			return errors.New("extension: invalid or duplicate asset path")
		}
		seen[source] = true
	}
	return nil
}

// Registry is an immutable per-application allowlist, not a global plugin loader.
type Registry struct {
	definitions map[string]Definition
	ordered     []Definition
}

func New(definitions ...Definition) (*Registry, error) {
	if len(definitions) > 16 {
		return nil, errors.New("extension: too many compiled definitions")
	}
	registry := &Registry{definitions: map[string]Definition{}}
	capabilities := map[string]bool{}
	for _, definition := range definitions {
		manifest := definition.Manifest
		if err := manifest.Validate(); err != nil {
			return nil, err
		}
		if registry.definitions[manifest.Name].Validate != nil || definition.Assets == nil || definition.Validate == nil || definition.Accept == nil {
			return nil, errors.New("extension: duplicate or incomplete definition")
		}
		for _, capability := range manifest.Capabilities {
			if capabilities[capability] {
				return nil, errors.New("extension: capability has multiple owners")
			}
			capabilities[capability] = true
		}
		stored, err := definition.Assets.ReadFile(ManifestName)
		if err != nil {
			return nil, err
		}
		encoded, _ := json.Marshal(manifest)
		parsed, err := ParseManifest(stored)
		actual, _ := json.Marshal(parsed)
		if err != nil || !bytes.Equal(encoded, actual) {
			return nil, errors.New("extension: embedded manifest does not match definition")
		}
		assets := make(assetFiles)
		for _, path := range append(append([]string(nil), manifest.IOS.Sources...), manifest.WebView.Script) {
			content, err := definition.Assets.ReadFile(path)
			if err != nil || len(content) == 0 || len(content) > 512*1024 {
				return nil, fmt.Errorf("extension: missing or oversized asset %q", path)
			}
			assets[path] = bytes.Clone(content)
		}
		assets[ManifestName] = bytes.Clone(stored)
		definition.Manifest.Capabilities = append([]string(nil), manifest.Capabilities...)
		definition.Manifest.IOS.Sources = append([]string(nil), manifest.IOS.Sources...)
		definition.Assets = assets
		registry.definitions[manifest.Name] = definition
		registry.ordered = append(registry.ordered, definition)
	}
	return registry, nil
}

type assetFiles map[string][]byte

func (assets assetFiles) Open(name string) (fs.File, error) {
	content, err := assets.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return &assetFile{Reader: bytes.NewReader(content), name: name}, nil
}

type assetFile struct {
	*bytes.Reader
	name string
}

func (file *assetFile) Close() error { return nil }
func (file *assetFile) Stat() (fs.FileInfo, error) {
	return assetInfo{path.Base(file.name), file.Size()}, nil
}

type assetInfo struct {
	name string
	size int64
}

func (info assetInfo) Name() string  { return info.name }
func (info assetInfo) Size() int64   { return info.size }
func (assetInfo) Mode() fs.FileMode  { return 0444 }
func (assetInfo) ModTime() time.Time { return time.Time{} }
func (assetInfo) IsDir() bool        { return false }
func (assetInfo) Sys() any           { return nil }
func (assets assetFiles) ReadFile(path string) ([]byte, error) {
	content, exists := assets[path]
	if !exists {
		return nil, fs.ErrNotExist
	}
	return bytes.Clone(content), nil
}

func (registry *Registry) Definitions() []Definition {
	result := append([]Definition(nil), registry.ordered...)
	for index := range result {
		result[index].Manifest.Capabilities = append([]string(nil), result[index].Manifest.Capabilities...)
		result[index].Manifest.IOS.Sources = append([]string(nil), result[index].Manifest.IOS.Sources...)
	}
	return result
}

func (registry *Registry) Allows(capability string) bool {
	for _, definition := range registry.ordered {
		for _, declared := range definition.Manifest.Capabilities {
			if declared == capability {
				return true
			}
		}
	}
	return false
}

func (registry *Registry) Validate(operation FileOperation) (string, error) {
	definition, exists := registry.definitions[operation.Name]
	if !exists || len(operation.Payload) == 0 || len(operation.Payload) > MaxPayload || !json.Valid([]byte(operation.Payload)) {
		return "", errors.New("extension: unregistered operation or invalid payload")
	}
	capability, err := definition.Validate(operation)
	if err != nil {
		return "", err
	}
	for _, declared := range definition.Manifest.Capabilities {
		if declared == capability {
			return capability, nil
		}
	}
	return "", errors.New("extension: validator returned undeclared capability")
}

func (registry *Registry) Accept(operation FileOperation, document files.Document) error {
	if _, err := registry.Validate(operation); err != nil {
		return err
	}
	if err := document.Validate(); err != nil {
		return err
	}
	return registry.definitions[operation.Name].Accept(operation, document)
}
