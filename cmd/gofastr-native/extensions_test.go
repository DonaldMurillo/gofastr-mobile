package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

func TestCompiledExtensionDiscoveryAndPackaging(t *testing.T) {
	runner := &runner{ctx: context.Background(), out: io.Discard}
	definitions, err := discoverExtensions(runner, "github.com/DonaldMurillo/gofastr-native/examples/notes/notesnative")
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 2 || definitions[0].manifest.Name != "gofastr.media/v1" || definitions[0].manifest.Kind != extension.ComponentKind || definitions[1].manifest.Name != "gofastr.photos/v1" {
		t.Fatalf("unexpected compiled imports: %+v", definitions)
	}
	application := nativeApp(t)
	application.Extensions = definitions
	directory := filepath.Join(t.TempDir(), "ios")
	sources, err := prepareIOSHost(runner, application, directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		for _, name := range definition.manifest.IOS.Sources {
			original, err := extensionAsset(definition.directory, name, 512*1024)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, path := range sources {
				content, _ := os.ReadFile(path)
				if bytes.Equal(content, original) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("extension asset %s was not compiled verbatim", name)
			}
		}
	}
	catalog, err := os.ReadFile(filepath.Join(directory, "renderer", "GofastrCompiledExtensions.swift"))
	if err != nil || !bytes.Contains(catalog, []byte("MobileFileExtensions.install([GofastrPhotoExtension.self])")) || !bytes.Contains(catalog, []byte("MobileComponentExtensions.install([GofastrMediaComponent.self])")) || !bytes.Contains(catalog, []byte(`GofastrPhotoExtension.name == "gofastr.photos/v1"`)) {
		t.Fatalf("catalog: %s %v", catalog, err)
	}
	bootstrap, _ := os.ReadFile(sources[0])
	if !bytes.Contains(bootstrap, []byte("GofastrCompiledExtensions.install()")) || bytes.Contains(bootstrap, []byte("Photo")) {
		t.Fatal("bootstrap must register compiled libraries, not contain photo logic")
	}
}

func TestExtensionAssetsRejectSymlinksAndEscape(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.swift")
	if err := os.WriteFile(outside, []byte("import UIKit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, "link.swift")); err != nil {
		t.Fatal(err)
	}
	if _, err := extensionAsset(directory, "link.swift", 512); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := extensionAsset(directory, outside, 512); err == nil {
		t.Fatal("absolute path accepted")
	}
	if err := os.WriteFile(filepath.Join(directory, "large.swift"), []byte(strings.Repeat("x", 513)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := extensionAsset(directory, "large.swift", 512); err == nil {
		t.Fatal("oversized asset accepted")
	}
}

func TestExtensionDiscoveryDryRunDoesNotReadOrWrite(t *testing.T) {
	var output bytes.Buffer
	directory := filepath.Join(t.TempDir(), "not-created")
	runner := &runner{ctx: context.Background(), out: &output, dryRun: true}
	definitions, err := discoverExtensions(runner, "example.invalid/not-installed")
	if err != nil || len(definitions) != 0 {
		t.Fatal(definitions, err)
	}
	if _, err := copyIOSExtensions(runner, []compiledExtension{{manifest: extension.Manifest{Name: "example/v1", IOS: extension.IOS{Factory: "Example", Sources: []string{"missing.swift"}}}, directory: "/not-installed"}}, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote extension assets")
	}
}
