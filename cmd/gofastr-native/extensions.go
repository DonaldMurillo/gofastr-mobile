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
	content, err := r.outputBytes("discover compiled native extensions", nil, "env", "GOOS=ios", "GOARCH=arm64", "CGO_ENABLED=1", "go", "list", "-deps", "-json", packagePath)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	var result []compiledExtension
	seen, capabilities, factories := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for {
		var dependency struct{ Dir string }
		if err := decoder.Decode(&dependency); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
		if dependency.Dir == "" {
			continue
		}
		path := filepath.Join(dependency.Dir, extension.ManifestName)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		content, err := extensionAsset(dependency.Dir, extension.ManifestName, extension.MaxPayload)
		if err != nil {
			return nil, err
		}
		manifest, err := extension.ParseManifest(content)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if seen[manifest.Name] || factories[manifest.IOS.Factory] || len(result) >= 16 {
			return nil, errors.New("duplicate or excessive compiled extensions")
		}
		seen[manifest.Name], factories[manifest.IOS.Factory] = true, true
		for _, capability := range manifest.Capabilities {
			if capabilities[capability] {
				return nil, errors.New("compiled extension capabilities overlap")
			}
			capabilities[capability] = true
		}
		for _, source := range append(append([]string(nil), manifest.IOS.Sources...), manifest.WebView.Script) {
			if _, err := extensionAsset(dependency.Dir, source, 512*1024); err != nil {
				return nil, err
			}
		}
		result = append(result, compiledExtension{manifest, dependency.Dir})
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
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
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
	var sources, factories, guards []string
	for _, definition := range definitions {
		factories = append(factories, definition.manifest.IOS.Factory+".self")
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
		return writeOwnerOnly(catalog, []byte("enum GofastrCompiledExtensions {\n    static func install() {\n"+strings.Join(guards, "")+"        MobileFileExtensions.install(["+strings.Join(factories, ", ")+"])\n    }\n}\n"))
	}); err != nil {
		return nil, err
	}
	return append(sources, catalog), nil
}
