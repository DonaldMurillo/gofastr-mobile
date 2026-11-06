package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
	renderios "github.com/DonaldMurillo/gofastr-native/render/ios"
)

func toolingManifest(name string, kind extension.Kind, factory string) extension.Manifest {
	return extension.Manifest{Name: name, Kind: kind, Capabilities: []string{name}, IOS: extension.IOS{Factory: factory, Sources: []string{"Adapter.swift"}}, WebView: extension.WebView{Script: "adapter.js"}}
}

func toolingPackage(t *testing.T, manifest extension.Manifest) string {
	t.Helper()
	directory := t.TempDir()
	content, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string][]byte{extension.ManifestName: content, "Adapter.swift": []byte("import UIKit"), "adapter.js": []byte("void 0;")} {
		if err := os.WriteFile(filepath.Join(directory, name), value, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func toolingGraph(directories ...string) []byte {
	var content bytes.Buffer
	for _, directory := range directories {
		json.NewEncoder(&content).Encode(struct{ Dir string }{directory})
	}
	return content.Bytes()
}

func TestExtensionCheckCatalogAndCollisions(t *testing.T) {
	for _, conflict := range []string{"identity", "factory", "capability", "none"} {
		t.Run(conflict, func(t *testing.T) {
			first := toolingManifest("example.file/v1", "", "First")
			second := toolingManifest("example.component/v1", extension.ComponentKind, "Second")
			switch conflict {
			case "identity":
				second.Name = first.Name
				second.Capabilities = []string{first.Name}
			case "factory":
				second.IOS.Factory = first.IOS.Factory
			case "capability":
				second.Kind = extension.FileKind
				second.Capabilities = first.Capabilities
			}
			firstDir, secondDir := toolingPackage(t, first), toolingPackage(t, second)
			definitions, err := discoverExtensionManifests(toolingGraph(firstDir, secondDir))
			if conflict != "none" {
				if err == nil || !strings.Contains(err.Error(), conflict) || !strings.Contains(err.Error(), "example.file/v1") {
					t.Fatalf("%s: %v", conflict, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			other, err := discoverExtensionManifests(toolingGraph(secondDir, firstDir))
			if err != nil {
				t.Fatal(err)
			}
			var report, reordered bytes.Buffer
			printExtensionReport(&report, definitions)
			printExtensionReport(&reordered, other)
			if report.String() != reordered.String() || !strings.Contains(report.String(), "kind=file") || !strings.Contains(report.String(), "kind=component") || !strings.Contains(report.String(), "no identity/capability/factory collisions") {
				t.Fatal(report.String(), reordered.String())
			}
		})
	}
}

func TestExtensionCheckBoundsAndInvalidAssets(t *testing.T) {
	for _, issue := range []string{"missing", "empty", "oversized", "symlink", "directory", "unknown-kind", "component-capability", "trailing-json"} {
		t.Run(issue, func(t *testing.T) {
			manifest := toolingManifest("example.test/v1", extension.FileKind, "TestAdapter")
			if issue == "unknown-kind" {
				manifest.Kind = "dynamic"
			}
			if issue == "component-capability" {
				manifest.Kind = extension.ComponentKind
				manifest.Capabilities = []string{"other/v1"}
			}
			directory := toolingPackage(t, manifest)
			asset := filepath.Join(directory, "Adapter.swift")
			switch issue {
			case "missing":
				os.Remove(asset)
			case "empty":
				os.WriteFile(asset, nil, 0600)
			case "oversized":
				os.WriteFile(asset, bytes.Repeat([]byte("x"), 512*1024+1), 0600)
			case "symlink":
				os.Remove(asset)
				os.Symlink(filepath.Join(directory, "adapter.js"), asset)
			case "directory":
				os.Remove(asset)
				os.Mkdir(asset, 0700)
			case "trailing-json":
				content, _ := os.ReadFile(filepath.Join(directory, extension.ManifestName))
				os.WriteFile(filepath.Join(directory, extension.ManifestName), append(content, []byte(" {}")...), 0600)
			}
			if _, err := discoverExtensionManifests(toolingGraph(directory)); err == nil {
				t.Fatal("accepted invalid manifest/asset")
			}
		})
	}
	var directories []string
	for index := range 17 {
		directories = append(directories, toolingPackage(t, toolingManifest(fmt.Sprintf("example.item%d/v1", index), extension.FileKind, fmt.Sprintf("Factory%d", index))))
	}
	if _, err := discoverExtensionManifests(toolingGraph(directories...)); err == nil || !strings.Contains(err.Error(), "limit is 16") {
		t.Fatal(err)
	}
	if _, err := discoverExtensionManifests([]byte("not JSON")); err == nil {
		t.Fatal("accepted malformed dependency graph")
	}
}

func TestCompiledExtensionCoreOnlyCatalog(t *testing.T) {
	directory := t.TempDir()
	runner := &runner{ctx: context.Background(), out: io.Discard}
	sources, err := copyIOSExtensions(runner, nil, directory)
	if err != nil || len(sources) != 1 {
		t.Fatal(sources, err)
	}
	content, err := os.ReadFile(sources[0])
	if err != nil || !bytes.Contains(content, []byte("MobileFileExtensions.install([])")) || !bytes.Contains(content, []byte("MobileComponentExtensions.install([])")) {
		t.Fatal(string(content), err)
	}
	if bytes.Contains(content, []byte("GofastrMedia")) || bytes.Contains(content, []byte("GofastrPhoto")) {
		t.Fatal("core-only catalog contains pilot adapters")
	}
}

func TestExtensionCheckCommandBoundary(t *testing.T) {
	directory := toolingPackage(t, toolingManifest("example.test/v1", extension.FileKind, "TestAdapter"))
	bin := t.TempDir()
	graphPath := filepath.Join(bin, "graph.json")
	if err := os.WriteFile(graphPath, toolingGraph(directory), 0600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$*\" = 'list -deps -json ./consumer' ] || exit 27\ncat \"$EXTENSION_GRAPH\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		source := filepath.Join(t.TempDir(), "fakego.go")
		program := "package main\nimport(\"os\";\"strings\")\nfunc main(){if strings.Join(os.Args[1:],\" \")!=\"list -deps -json ./consumer\"{os.Exit(27)};b,e:=os.ReadFile(os.Getenv(\"EXTENSION_GRAPH\"));if e!=nil{os.Exit(28)};os.Stdout.Write(b)}"
		if err := os.WriteFile(source, []byte(program), 0600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("go", "build", "-o", filepath.Join(bin, "go.exe"), source).CombinedOutput(); err != nil {
			t.Fatalf("fake go: %v %s", err, output)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EXTENSION_GRAPH", graphPath)
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"extension", "check", "--pkg", "./consumer"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "example.test/v1 kind=file") {
		t.Fatalf("%d %s %s", code, &stdout, &stderr)
	}
	for _, args := range [][]string{{"extension", "check"}, {"extension", "check", "--pkg=-exec=oops"}, {"extension", "wrong"}, {"extension", "check", "--pkg", "./consumer", "extra"}} {
		if code := run(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Fatalf("%v: %d", args, code)
		}
	}
}

func TestExtensionScaffoldCLIAndRollback(t *testing.T) {
	module := t.TempDir()
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.test/author\n\ngo 1.27.3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"file", "component"} {
		directory := filepath.Join(module, kind)
		var stdout, stderr bytes.Buffer
		args := []string{"extension", "init", "--name", "example." + kind + "/v1", "--kind", kind, "--out", directory}
		if code := run(context.Background(), append(args, "--dry-run"), &stdout, &stderr); code != 0 {
			t.Fatal(code, &stderr)
		}
		if _, err := os.Stat(directory); !os.IsNotExist(err) {
			t.Fatal("dry run wrote files")
		}
		if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
			t.Fatal(code, &stderr)
		}
		before, err := os.ReadFile(filepath.Join(directory, "adapter.go"))
		if err != nil {
			t.Fatal(err)
		}
		if code := run(context.Background(), args, &stdout, &stderr); code == 0 {
			t.Fatal("overwrote existing directory")
		}
		after, _ := os.ReadFile(filepath.Join(directory, "adapter.go"))
		if !bytes.Equal(before, after) {
			t.Fatal("existing source changed")
		}
		definitions, err := discoverExtensionManifests(toolingGraph(directory))
		if err != nil || len(definitions) != 1 {
			t.Fatal(definitions, err)
		}
		command := exec.Command("go", "test", filepath.Join(directory, "adapter.go"), filepath.Join(directory, "adapter_test.go"))
		if content, err := command.CombinedOutput(); err != nil {
			t.Fatalf("generated Go library: %v\n%s", err, content)
		}
	}
	files, err := scaffoldFiles("example.rollback/v1", "file")
	if err != nil {
		t.Fatal(err)
	}
	failedDirectory := filepath.Join(module, "rollback")
	writes := 0
	err = writeScaffold(failedDirectory, files, func(path string, content []byte) error {
		writes++
		if writes == 3 {
			return errors.New("disk failure")
		}
		return writeNewScaffoldFile(path, content)
	})
	if err == nil {
		t.Fatal("injected write did not fail")
	}
	if _, err := os.Stat(failedDirectory); !os.IsNotExist(err) {
		t.Fatal("partial scaffold was not rolled back", err)
	}
	for _, name := range []string{"../escape/v1", "safe/v1\nattack", `bad"/v1`, "safe;exec/v1", "Bad/v1", "no-version", strings.Repeat("a", 81) + "/v1"} {
		if _, err := scaffoldFiles(name, "file"); err == nil {
			t.Fatalf("unsafe name accepted %q", name)
		}
	}
	if _, err := scaffoldFiles("example/v1", "remote"); err == nil {
		t.Fatal("invalid kind accepted")
	}
	if err := scaffoldExtension(&runner{ctx: context.Background(), out: io.Discard}, "example/v1", "file", filepath.Join(t.TempDir(), "no-module")); err == nil {
		t.Fatal("scaffold outside module accepted")
	}
	symlink := filepath.Join(module, "link")
	if err := os.Symlink(t.TempDir(), symlink); err != nil {
		t.Fatal(err)
	}
	if err := scaffoldExtension(&runner{ctx: context.Background(), out: io.Discard}, "example/v1", "file", symlink); err == nil {
		t.Fatal("existing symlink accepted")
	}
}

func TestExtensionScaffoldUIKitAdaptersTypecheck(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("UIKit typechecking needs Xcode")
	}
	sdk, err := exec.Command("xcrun", "--sdk", "iphoneos", "--show-sdk-path").Output()
	if err != nil {
		t.Skipf("no physical iOS SDK: %v", err)
	}
	directory := t.TempDir()
	runner := &runner{ctx: context.Background(), out: io.Discard}
	sources, err := copyIOSLibrary(runner, renderios.Sources, filepath.Join(directory, "renderer"), "renderer")
	if err != nil {
		t.Fatal(err)
	}
	var definitions []compiledExtension
	for _, kind := range []string{"file", "component"} {
		files, err := scaffoldFiles("example."+kind+"/v1", kind)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, kind)
		if err := writeScaffold(path, files, writeNewScaffoldFile); err != nil {
			t.Fatal(err)
		}
		manifest, err := extension.ParseManifest(files[extension.ManifestName])
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, compiledExtension{manifest, path})
	}
	extensions, err := copyIOSExtensions(runner, definitions, filepath.Join(directory, "renderer"))
	if err != nil {
		t.Fatal(err)
	}
	arguments := []string{"--sdk", "iphoneos", "swiftc", "-parse-as-library", "-typecheck", "-module-name", "ScaffoldChecks", "-target", "arm64-apple-ios16.0", "-sdk", strings.TrimSpace(string(sdk)), "-module-cache-path", filepath.Join(directory, "module-cache")}
	arguments = append(arguments, sources...)
	arguments = append(arguments, extensions...)
	if content, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("physical arm64 Swift adapter typecheck: %v\n%s", err, content)
	}
}

func TestExtensionScaffoldJavaScriptAdapters(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node is needed to exercise ordinary JavaScript adapters")
	}
	directory := t.TempDir()
	for _, kind := range []string{"file", "component"} {
		files, err := scaffoldFiles("example."+kind+"/v1", kind)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, kind+".js"), files["adapter.js"], 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(directory, "checks.cjs")
	if err := os.WriteFile(path, []byte(scaffoldJSChecks), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("node", path, directory)
	if content, err := command.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript adapter contract: %v\n%s", err, content)
	}
}

const scaffoldJSChecks = `const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const directory = process.argv[2];
const window = {};
const element = () => ({ style: {}, textContent: '', children: [], replaceChildren(...children) { this.children = children; } });
const context = vm.createContext({ window, File, Blob, btoa, TextEncoder, Uint8Array, document: { createElement: element } });
for (const kind of ['file', 'component']) vm.runInContext(fs.readFileSync(path.join(directory, kind + '.js'), 'utf8'), context);
(async () => {
  const file = window.__gofastr_file_extension_definitions[0];
  assert.equal(file.capabilities()['example.file/v1'], 'fallback');
  assert.equal(file.prepare({ title: 'Pick', maxBytes: 0 }), null);
  const prepared = file.prepare({ title: 'Pick', maxBytes: 8, extensions: ['.txt'] });
  const result = await prepared.normalize(new File(['hi'], 'note.txt'), prepared.job());
  assert.equal(result.data, 'aGk=');
  await assert.rejects(prepared.normalize(new File(['hi'], 'note.exe'), prepared.job()));
  await assert.rejects(prepared.normalize(new File(['123456789'], 'note.txt'), prepared.job()));
  const cancelled = prepared.job(); cancelled.cancel();
  await assert.rejects(prepared.normalize(new File(['hi'], 'note.txt'), cancelled), error => error.name === 'AbortError');
  const component = window.__gofastr_component_extension_definitions[0];
  const container = element();
  const snapshot = { id: 'text', name: 'example.component/v1', payload: JSON.stringify({ text: '<b>plain text</b>' }) };
  const instance = component.mount(container, snapshot, {});
  assert.equal(container.children[0].textContent, '<b>plain text</b>');
  instance.update({ ...snapshot, payload: JSON.stringify({ text: 'updated' }) });
  assert.equal(container.children[0].textContent, 'updated');
  assert.throws(() => instance.update({ ...snapshot, destinations: { bad: '/arbitrary' } }));
  instance.dispose(); instance.dispose();
  assert.equal(container.children.length, 0);
  instance.update(snapshot); assert.equal(container.children.length, 0);
})().catch(error => { console.error(error); process.exitCode = 1; });
`
