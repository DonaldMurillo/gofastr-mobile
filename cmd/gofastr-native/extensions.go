package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

type compiledExtension struct {
	manifest  extension.Manifest
	directory string
}

func discoverExtensions(r *runner, packagePath string) ([]compiledExtension, error) {
	return discoverPlatformExtensions(r, packagePath, "ios")
}

func discoverPlatformExtensions(r *runner, packagePath, target string) ([]compiledExtension, error) {
	if packagePath == "" || strings.HasPrefix(packagePath, "-") {
		return nil, errors.New("--pkg must name a Go package, not a go list flag")
	}
	probe := *r
	probe.env = append(append([]string(nil), r.env...), "GOOS="+target, "GOARCH=arm64", "CGO_ENABLED=1")
	content, err := probe.outputBytes("discover compiled native extensions", nil, "go", "list", "-deps", "-json", packagePath)
	r.step = probe.step
	if err != nil {
		return nil, err
	}
	return discoverExtensionManifests(content)
}

func discoverExtensionManifests(content []byte) ([]compiledExtension, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	var directories []string
	for {
		var dependency struct{ Dir string }
		if err := decoder.Decode(&dependency); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode compiled dependency graph: %w", err)
		}
		if dependency.Dir != "" {
			directories = append(directories, dependency.Dir)
		}
	}
	sort.Strings(directories)
	var result []compiledExtension
	seen, capabilities, factories := map[string]string{}, map[string]string{}, map[string]string{}
	for _, directory := range directories {
		path := filepath.Join(directory, extension.ManifestName)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		content, err := extensionAsset(directory, extension.ManifestName, extension.MaxPayload)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		manifest, err := extension.ParseManifest(content)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if previous, exists := seen[manifest.Name]; exists {
			return nil, fmt.Errorf("%s: extension identity %q also declared in %s; use distinct versioned names", path, manifest.Name, previous)
		}
		if previous, exists := factories[manifest.IOS.Factory]; exists {
			return nil, fmt.Errorf("%s: Swift factory %q also owned by %s; rename one factory and its manifest entry", path, manifest.IOS.Factory, previous)
		}
		if len(result) >= 16 {
			return nil, fmt.Errorf("%s: compiled extension limit is 16; remove unused extension imports", path)
		}
		seen[manifest.Name], factories[manifest.IOS.Factory] = path, manifest.Name
		for _, capability := range manifest.Capabilities {
			if previous, exists := capabilities[capability]; exists {
				return nil, fmt.Errorf("%s: capability %q also owned by %s; each capability must have one compiled owner", path, capability, previous)
			}
			capabilities[capability] = manifest.Name
		}
		assets := append(append([]string(nil), manifest.IOS.Sources...), manifest.WebView.Script)
		if manifest.Android != nil {
			assets = append(assets, manifest.Android.Sources...)
		}
		for _, source := range assets {
			if _, err := extensionAsset(directory, source, 512*1024); err != nil {
				return nil, fmt.Errorf("%s: asset %q for %s: %w; bundle a nonempty regular asset (at most 512 KiB) without symlinks", path, source, manifest.Name, err)
			}
		}
		result = append(result, compiledExtension{manifest, directory})
	}
	sort.Slice(result, func(first, second int) bool { return result[first].manifest.Name < result[second].manifest.Name })
	return result, nil
}

func extensionAsset(directory, name string, limit int64) ([]byte, error) {
	if !fs.ValidPath(name) || strings.Contains(name, "\\") {
		return nil, errors.New("extension asset path must be package-relative")
	}
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if real != path || !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return nil, errors.New("extension assets must stay in their package without symlinks")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("extension asset must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("extension asset must be a regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(content) == 0 || int64(len(content)) > limit {
		return nil, errors.New("extension asset is empty or exceeds its bound")
	}
	return content, nil
}

func copyIOSExtensions(r *runner, definitions []compiledExtension, directory string) ([]string, error) {
	definitions = append([]compiledExtension(nil), definitions...)
	sort.Slice(definitions, func(first, second int) bool {
		return definitions[first].manifest.Name < definitions[second].manifest.Name
	})
	var sources, fileFactories, componentFactories, guards []string
	for _, definition := range definitions {
		if definition.manifest.Kind == extension.ComponentKind {
			componentFactories = append(componentFactories, definition.manifest.IOS.Factory+".self")
		} else {
			fileFactories = append(fileFactories, definition.manifest.IOS.Factory+".self")
		}
		var capabilities []string
		for _, capability := range definition.manifest.Capabilities {
			capabilities = append(capabilities, strconv.Quote(capability))
		}
		guards = append(guards, "        precondition("+definition.manifest.IOS.Factory+".name == "+strconv.Quote(definition.manifest.Name)+")\n",
			"        precondition(Set("+definition.manifest.IOS.Factory+".capabilities().keys) == Set(["+strings.Join(capabilities, ", ")+"]))\n")
		digest := sha256.Sum256([]byte(definition.manifest.Name))
		for index, source := range definition.manifest.IOS.Sources {
			destination := filepath.Join(directory, fmt.Sprintf("ext-%x-%d.swift", digest[:8], index))
			if err := r.do("copy compiled extension "+definition.manifest.Name+" "+source, func() error {
				content, err := extensionAsset(definition.directory, source, 512*1024)
				if err != nil {
					return err
				}
				return writeOwnerOnly(destination, content)
			}); err != nil {
				return nil, err
			}
			sources = append(sources, destination)
		}
	}
	catalog := filepath.Join(directory, "GofastrCompiledExtensions.swift")
	if err := r.do("generate the compiled extension factory catalog", func() error {
		return writeOwnerOnly(catalog, []byte("enum GofastrCompiledExtensions {\n    static func install() {\n"+strings.Join(guards, "")+"        MobileFileExtensions.install(["+strings.Join(fileFactories, ", ")+"])\n        MobileComponentExtensions.install(["+strings.Join(componentFactories, ", ")+"])\n    }\n}\n"))
	}); err != nil {
		return nil, err
	}
	return append(sources, catalog), nil
}
