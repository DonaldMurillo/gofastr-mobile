package main

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/DonaldMurillo/gofastr-native/extension"
)

//go:embed scaffolds/*
var extensionScaffolds embed.FS

func scaffoldExtension(r *runner, name, kind, directory string) error {
	files, err := scaffoldFiles(name, kind)
	if err != nil {
		return err
	}
	if directory == "" {
		return errors.New("extension init requires --out <new-directory> inside your Go module")
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(directory))
	if err != nil {
		return fmt.Errorf("--out parent must already exist: %w", err)
	}
	directory = filepath.Join(parent, filepath.Base(directory))
	if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("refusing existing or inaccessible output %s; choose a new directory", directory)
	}
	module := parent
	for {
		if info, err := os.Stat(filepath.Join(module, "go.mod")); err == nil && info.Mode().IsRegular() {
			break
		}
		if filepath.Dir(module) == module {
			return errors.New("--out must be inside an existing Go module; no module or dependencies are created")
		}
		module = filepath.Dir(module)
	}
	if err := r.do("create extension library "+name+" in "+directory, func() error {
		return writeScaffold(directory, files, writeNewScaffoldFile)
	}); err != nil {
		return err
	}
	for _, filename := range sortedScaffoldFiles(files) {
		fmt.Fprintf(r.out, "  %s\n", filepath.Join(directory, filename))
	}
	if r.dryRun {
		fmt.Fprintln(r.out, "NOT RUN: no files or directories created (dry run)")
	} else {
		fmt.Fprintln(r.out, "Created library; import it and explicitly select Definition() in your app. See README.md; no dependencies or app templates added.")
	}
	return nil
}

func scaffoldFiles(name, kind string) (map[string][]byte, error) {
	digest := sha256.Sum256([]byte(name))
	factory := fmt.Sprintf("GofastrAdapter%x", digest[:8])
	manifest := extension.Manifest{Name: name, Kind: extension.Kind(kind), Capabilities: []string{name},
		IOS: extension.IOS{Factory: factory, Sources: []string{"Adapter.swift"}}, Android: &extension.Android{Factory: factory, Sources: []string{factory + ".java"}}, WebView: extension.WebView{Script: "adapter.js"}}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("--name/--kind: %w (use a lowercase versioned name such as example.documents/v1)", err)
	}
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{extension.ManifestName: append(content, '\n')}
	data := struct{ Name, Factory string }{strconv.Quote(name), factory}
	for _, entry := range []struct{ source, target string }{
		{"file.go.tmpl", "adapter.go"}, {"file.swift.tmpl", "Adapter.swift"}, {"file.js.tmpl", "adapter.js"}, {"file_test.go.tmpl", "adapter_test.go"}, {"file-readme.tmpl", "README.md"},
		{"file.java.tmpl", factory + ".java"},
	} {
		source := entry.source
		if kind == "component" {
			source = "component" + source[len("file"):]
		}
		content, err := extensionScaffolds.ReadFile("scaffolds/" + source)
		if err != nil {
			return nil, err
		}
		parsed, err := template.New(entry.source).Parse(string(content))
		if err != nil {
			return nil, err
		}
		var rendered bytes.Buffer
		if err := parsed.Execute(&rendered, data); err != nil {
			return nil, err
		}
		content = rendered.Bytes()
		if strings.HasSuffix(entry.target, ".go") {
			content, err = format.Source(content)
			if err != nil {
				return nil, err
			}
		}
		files[entry.target] = content
	}
	return files, nil
}

func sortedScaffoldFiles(files map[string][]byte) []string {
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// writeScaffold creates directory and every file in files. Names are
// slash-separated relative paths; each parent directory is created with
// os.Mkdir, and each file must be new, so nothing that already exists is
// followed or replaced. On failure it removes what it created, newest first.
func writeScaffold(directory string, files map[string][]byte, write func(string, []byte) error) (err error) {
	for name := range files {
		if !filepath.IsLocal(filepath.FromSlash(name)) || strings.Contains(name, `\`) {
			return fmt.Errorf("scaffold file %q is not a plain relative path", name)
		}
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	var created []string // files and directories, in creation order
	made := map[string]bool{}
	defer func() {
		if err == nil {
			return
		}
		for i := len(created) - 1; i >= 0; i-- {
			err = errors.Join(err, os.Remove(created[i]))
		}
		err = errors.Join(err, os.Remove(directory))
	}()
	for _, name := range sortedScaffoldFiles(files) {
		parts := strings.Split(name, "/")
		parent := directory
		for _, part := range parts[:len(parts)-1] {
			parent = filepath.Join(parent, part)
			if made[parent] {
				continue
			}
			if err := os.Mkdir(parent, 0700); err != nil {
				return err
			}
			made[parent] = true
			created = append(created, parent)
		}
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := write(path, files[name]); err != nil {
			return err
		}
		created = append(created, path)
	}
	return nil
}

func writeNewScaffoldFile(path string, content []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(path))
		}
	}()
	_, err = file.Write(content)
	return errors.Join(err, file.Close())
}
