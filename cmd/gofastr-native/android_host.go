package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	renderandroid "github.com/DonaldMurillo/gofastr-native/render/android"
)

func prepareAndroidHost(r *runner, a app, dir string) ([]string, error) {
	activity := filepath.Join(dir, "src", filepath.FromSlash(strings.ReplaceAll(a.ID, ".", "/")), "MainActivity.java")
	if err := r.do("generate Android binding bootstrap", func() error { return writeRendered("templates/android/MainActivity.java.tmpl", activity, a) }); err != nil {
		return nil, err
	}
	sources := []string{activity}
	if a.Renderer != "native" {
		return sources, nil
	}
	entries, err := renderandroid.Sources.ReadDir(".")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		dst := filepath.Join(dir, "renderer", entry.Name())
		if err := r.do("copy native Android renderer "+entry.Name(), func() error {
			content, err := renderandroid.Sources.ReadFile(entry.Name())
			if err != nil {
				return err
			}
			return writeSource(dst, content)
		}); err != nil {
			return nil, err
		}
		sources = append(sources, dst)
	}
	var factories []string
	seen := map[string]bool{}
	for index, definition := range a.Extensions {
		adapter := definition.manifest.Android
		if adapter == nil {
			return nil, fmt.Errorf("extension %s has no native Android adapter", definition.manifest.Name)
		}
		if seen[adapter.Factory] {
			return nil, fmt.Errorf("duplicate Android extension factory %s", adapter.Factory)
		}
		seen[adapter.Factory] = true
		factories = append(factories, "        Extensions.install("+strconv.Quote(definition.manifest.Name)+", new dev.gofastr.nativeui."+adapter.Factory+"());\n")
		for _, source := range adapter.Sources {
			dst := filepath.Join(dir, fmt.Sprintf("extension-%d", index), filepath.Base(source))
			if err := r.do("copy Android extension "+source, func() error {
				content, err := extensionAsset(definition.directory, source, 512*1024)
				if err != nil {
					return err
				}
				return writeSource(dst, content)
			}); err != nil {
				return nil, err
			}
			sources = append(sources, dst)
		}
	}
	catalog := filepath.Join(dir, "src", filepath.FromSlash(strings.ReplaceAll(a.ID, ".", "/")), "CompiledExtensions.java")
	if err := r.do("generate Android extension catalog", func() error {
		return writeSource(catalog, []byte("package "+a.ID+";\nimport dev.gofastr.nativeui.Extensions;\nfinal class CompiledExtensions { static void install() {\n"+strings.Join(factories, "")+"} }\n"))
	}); err != nil {
		return nil, err
	}
	return append(sources, catalog), nil
}

func writeSource(path string, content []byte) error {
	// Reuse the template writer's directory and permission policy.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeOwnerOnly(path, content)
}

func prepareAndroidFeatureResources(r *runner, sdk androidSDK, a app, dir string) ([]string, error) {
	if !a.Widgets {
		return nil, nil
	}
	resource := filepath.Join(dir, "res", "xml", "gofastr_widget.xml")
	layout := filepath.Join(dir, "res", "layout", "gofastr_widget.xml")
	if err := r.do("generate widget provider resources", func() error {
		content, err := renderandroid.Resources.ReadFile("resources/layout/gofastr_widget.xml")
		if err != nil {
			return err
		}
		if err := writeSource(layout, content); err != nil {
			return err
		}
		return writeRendered("templates/android/widget.xml.tmpl", resource, a)
	}); err != nil {
		return nil, err
	}
	compiled := filepath.Join(dir, "widget-resources.zip")
	if err := r.run("compile widget provider resources", sdk.tool("aapt2"), "compile", "-o", compiled, resource, layout); err != nil {
		return nil, err
	}
	return []string{"-R", compiled}, nil
}
