package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	hostios "github.com/DonaldMurillo/gofastr-native/mobilehost/ios"
	renderios "github.com/DonaldMurillo/gofastr-native/render/ios"
)

func nativeApp(t *testing.T) app {
	t.Helper()
	application, err := newApp("github.com/DonaldMurillo/gofastr-native/examples/notes/notesnative", "notesnative", "dev.gofastr.notesnative", "Native Notes", "")
	if err != nil {
		t.Fatal(err)
	}
	application.Renderer = "native"
	return application.forIOS()
}

func TestBundledGoNativeRenderer(t *testing.T) {
	if _, err := templates.ReadFile("templates/ios/Native.swift.tmpl"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the full renderer must not be a template: %v", err)
	}
	entries, err := renderios.Sources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no bundled Swift library sources")
	}
	var swift []byte
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".swift" {
			t.Fatalf("renderer source is not ordinary Swift: %s", entry.Name())
		}
		content, err := renderios.Sources.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(filepath.Join("..", "..", "render", "ios", entry.Name()))
		if err != nil || !bytes.Equal(content, source) {
			t.Fatalf("embedded source differs from library source %s: %v", entry.Name(), err)
		}
		swift = append(swift, content...)
	}
	for _, expected := range []string{"import UIKit", "public struct GofastrNativeBackend", "public final class GofastrNativeRenderer", "UINavigationController", "UITextView", "UIActivityViewController", "keyboardLayoutGuide", "adjustsFontForContentSizeCategory", "try self.backend.render(route, payload)", "try self.backend.dispatch(route, action, payload)", "host.backend.start(directory)", "host.backend.root()", "host.backend.stop()"} {
		if !bytes.Contains(swift, []byte(expected)) {
			t.Errorf("native renderer lacks %s", expected)
		}
	}
	for _, forbidden := range []string{"WebKit", "WKWebView", "{{", "@main", "UIApplicationDelegate", "http://", "https://", "Notesnative", "NotesListController", "NoteEditorController", "New note", "/notes"} {
		if bytes.Contains(swift, []byte(forbidden)) {
			t.Errorf("native renderer contains %s", forbidden)
		}
	}
	for _, expected := range []string{
		"UserDefaults.standard.dictionary(forKey: draft.key)",
		"UserDefaults.standard.set(fieldValues, forKey: draft.key)",
		"UserDefaults.standard.removeObject(forKey: draft.key)",
		"current == self.revision", "pendingSearch?.cancel()",
		"interactivePopGestureRecognizer?.isEnabled = !dirty && !busy",
		"baseline = fieldValues", "setBusy(true)", "private var bufferRevision = 0", `send("checkpoint")`, `send("complete"`, `self.screen.route = saved.route`,
		"button.accessibilityLabel = action.label", `"native.action.\(action.id)"`,
		`"native.row.\(row.id)"`, `"native.field.\(field.key)"`,
		`"native.search"`, `"native.status"`,
		"UIAccessibility.post(notification: .announcement", ".medium(), .large()",
	} {
		if !bytes.Contains(swift, []byte(expected)) {
			t.Errorf("native renderer lost local state or accessibility behavior: %s", expected)
		}
	}
	application := nativeApp(t)
	plist, err := render("templates/ios/Info.plist.tmpl", application.forIOS())
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "custom host Info.plist", plist)
	if bytes.Contains(plist, []byte("import UIKit")) {
		t.Fatal("host override also replaced Info.plist")
	}
}

func TestNativeBindingBootstrap(t *testing.T) {
	for _, name := range []string{"notesnative", "inventory"} {
		t.Run(name, func(t *testing.T) {
			application, err := newApp("example/"+name, name, "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			application.Renderer = "native"
			swift, err := render("templates/ios/App.swift.tmpl", application.forIOS())
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Count(swift, []byte("\n")) > 60 || len(swift) > 4096 {
				t.Fatal("the native bootstrap must contain binding and app entry wiring only")
			}
			for _, expected := range []string{"import " + application.Prefix, "@main", "GofastrNativeRenderer(backend: GofastrNativeBackend(", "renderer.makeWindow()", "renderer.stop()", "throw error"} {
				if !bytes.Contains(swift, []byte(expected)) {
					t.Errorf("bootstrap lacks %s", expected)
				}
			}
			for _, function := range []string{"Start(", "Stop(", "Root()", "Render(", "Dispatch(", "Command("} {
				if !bytes.Contains(swift, []byte(application.Prefix+function)) {
					t.Errorf("bootstrap lacks binding call %s%s", application.Prefix, function)
				}
			}
			for _, forbidden := range []string{"{{", "WebKit", "WKWebView", "UINavigationController", "UITextView", "Decodable", "JSONDecoder", "UserDefaults", "/notes"} {
				if bytes.Contains(swift, []byte(forbidden)) {
					t.Errorf("bootstrap contains renderer or application behavior: %s", forbidden)
				}
			}
		})
	}
}

func TestNativeBootstrapMatchesGeneratedObjC(t *testing.T) {
	application := nativeApp(t)
	output := gobindPackageOut(t, application.PkgPath)
	header, err := os.ReadFile(filepath.Join(output, "src", "gobind", application.Prefix+".objc.h"))
	if err != nil {
		t.Fatal(err)
	}
	swift, err := render("templates/ios/App.swift.tmpl", application)
	if err != nil {
		t.Fatal(err)
	}
	for _, function := range []string{"Start", "Stop", "Root", "Render", "Dispatch", "Command"} {
		if !bytes.Contains(header, []byte(" "+application.Prefix+function+"(")) {
			t.Errorf("gomobile no longer exports %s%s", application.Prefix, function)
		}
		if !bytes.Contains(swift, []byte(application.Prefix+function+"(")) {
			t.Errorf("the bootstrap never calls %s%s", application.Prefix, function)
		}
	}
}

func TestPrepareIOSHostCopiesLibrarySourcesUnchanged(t *testing.T) {
	application := nativeApp(t)
	directory := filepath.Join(t.TempDir(), "ios")
	runner := &runner{ctx: context.Background(), out: io.Discard}
	sources, err := prepareIOSHost(runner, application, directory)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := renderios.Sources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != len(entries)+1 || sources[0] != filepath.Join(directory, "App.swift") {
		t.Fatalf("unexpected Swift compilation inputs: %v", sources)
	}
	for index, entry := range entries {
		destination := filepath.Join(directory, "renderer", entry.Name())
		if sources[index+1] != destination {
			t.Fatalf("compile inputs omit copied source %s: %v", destination, sources)
		}
		want, err := renderios.Sources.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(destination)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("renderer must be copied verbatim, not templated: %s: %v", destination, err)
		}
		if err := os.Chmod(destination, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := prepareIOSHost(runner, application, directory); err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("source permissions: %s: %v", source, info.Mode())
		}
	}
	for _, mode := range []string{"webview", "custom"} {
		t.Run(mode, func(t *testing.T) {
			application.Renderer = "webview"
			if mode == "custom" {
				application.HostTemplate = filepath.Join(t.TempDir(), "custom.swift.tmpl")
				if err := os.WriteFile(application.HostTemplate, []byte("import {{.Prefix}}\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			sources, err := prepareIOSHost(runner, application, directory)
			if err != nil || len(sources) == 0 || sources[0] != filepath.Join(directory, "App.swift") {
				t.Fatalf("incorrect host compilation inputs: %v, %v", sources, err)
			}
			if mode == "custom" && len(sources) != 1 {
				t.Fatalf("custom host compiled bundled sources: %v", sources)
			}
			if mode == "webview" {
				hostEntries, err := hostios.Sources.ReadDir(".")
				if err != nil || len(sources) != len(entries)+len(hostEntries)+1 {
					t.Fatalf("WebView service host omitted library sources: %v, %v", sources, err)
				}
				for _, entry := range hostEntries {
					want, err := hostios.Sources.ReadFile(entry.Name())
					if err != nil {
						t.Fatal(err)
					}
					got, err := os.ReadFile(filepath.Join(directory, "host", entry.Name()))
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("WebView service source changed during copying: %s: %v", entry.Name(), err)
					}
				}
			}
		})
	}
}

func TestNativeSwiftLibraryTypechecksWithoutBindings(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the UIKit SDK requires Xcode on macOS")
	}
	sdk, err := exec.Command("xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		t.Skipf("no iOS simulator SDK: %v", err)
	}
	directory := t.TempDir()
	sources, err := prepareIOSHost(&runner{ctx: context.Background(), out: io.Discard}, nativeApp(t), directory)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--sdk", "iphonesimulator", "swiftc", "-parse-as-library", "-typecheck", "-module-name", "GofastrNative",
		"-target", simArch() + "-apple-ios" + minIOS + "-simulator", "-sdk", strings.TrimSpace(string(sdk)),
		"-module-cache-path", filepath.Join(directory, "module-cache")}
	args = append(args, sources[1:]...)
	if output, err := exec.Command("xcrun", args...).CombinedOutput(); err != nil {
		t.Fatalf("the UIKit library does not compile independently of Go bindings: %v\n%s", err, output)
	}
}

func TestCallerOwnedNativeHostTemplate(t *testing.T) {
	application, err := newApp("example/custom", "custom", "dev.gofastr.custom", "Custom", "")
	if err != nil {
		t.Fatal(err)
	}
	application.HostTemplate = filepath.Join(t.TempDir(), "App.swift.tmpl")
	if err := os.WriteFile(application.HostTemplate, []byte("import UIKit\nimport {{.Prefix}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	encoded, err := render("templates/ios/App.swift.tmpl", application.forIOS())
	if err != nil || string(encoded) != "import UIKit\nimport Custom\n" {
		t.Fatalf("custom host: %s, %v", encoded, err)
	}
}

func TestNativeHostTemplateValidationAndDryRun(t *testing.T) {
	oversized := filepath.Join(t.TempDir(), "large.swift.tmpl")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte("x"), 1<<20+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{filepath.Join(t.TempDir(), "missing.swift"), t.TempDir(), oversized} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--pkg", notesPkg, "--host-template", source, "--dry-run"}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "--host-template") {
			t.Fatalf("invalid source: exit %d, %s", code, stderr.String())
		}
	}
	custom := filepath.Join(t.TempDir(), "App.swift.tmpl")
	if err := os.WriteFile(custom, []byte("import UIKit\nimport {{.Prefix}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		pkg    string
		prefix string
		flags  []string
		native bool
	}{
		{"native", "../../examples/notes/notesnative", "Notesnative", []string{"--renderer", "native"}, true},
		{"default", notesPkg, "Notesmobile", nil, false},
		{"webview", notesPkg, "Notesmobile", []string{"--renderer", "webview"}, false},
		{"custom", notesPkg, "Notesmobile", []string{"--host-template", custom}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			output := filepath.Join(t.TempDir(), "unwritten")
			args := append([]string{"build", "--platform", "ios-sim", "--pkg", test.pkg, "--out", output, "--dry-run"}, test.flags...)
			code := run(context.Background(), args, &stdout, &stderr)
			if code != 0 || !strings.Contains(stdout.String(), "-framework "+test.prefix) {
				t.Fatalf("build plan: exit %d, %s\n%s", code, stdout.String(), stderr.String())
			}
			var compilation string
			for _, line := range strings.Split(stdout.String(), "\n") {
				if strings.Contains(line, " swiftc ") {
					compilation = line
				}
			}
			if !strings.Contains(compilation, filepath.Join(output, "work", "ios", "App.swift")) {
				t.Fatalf("Swift compilation omits the bootstrap: %s", compilation)
			}
			entries, err := renderios.Sources.ReadDir(".")
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				source := filepath.Join(output, "work", "ios", "renderer", entry.Name())
				if strings.Contains(compilation, source) != (test.name != "custom") {
					t.Errorf("mode=%s: wrong Swift compilation inputs: %s", test.name, compilation)
				}
			}
			if strings.Contains(stdout.String(), "copy bundled UIKit renderer") != (test.name != "custom") {
				t.Fatalf("mode=%s: wrong source copying plan: %s", test.name, stdout.String())
			}
			if strings.Contains(stdout.String(), "copy bundled WebView service adapter") != (!test.native && test.name != "custom") {
				t.Fatalf("mode=%s: wrong service host copying plan: %s", test.name, stdout.String())
			}
			if _, err := os.Stat(output); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("dry run touched its output directory: %v", err)
			}
		})
	}
}

func TestNativeRendererRejectsUnsupportedConfigurations(t *testing.T) {
	for _, test := range []struct {
		flags   []string
		message string
	}{
		{[]string{"--platform", "android-emu", "--renderer", "native"}, "Android native rendering is not implemented"},
		{[]string{"--platform", "ios-sim", "--renderer", "canvas"}, "--renderer must be webview or native"},
		{[]string{"--platform", "ios-sim", "--renderer", "native", "--host-template", "custom.tmpl"}, "cannot use --host-template"},
	} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"build", "--pkg", notesPkg, "--dry-run"}, test.flags...)
		if code := run(context.Background(), args, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), test.message) {
			t.Fatalf("unsupported native configuration: %d %s", code, stderr.String())
		}
	}
}

func TestNativeHostRunHints(t *testing.T) {
	application, err := newApp("example/notesnative", "notesnative", "dev.gofastr.notesnative", "Native Notes", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []struct {
		name   string
		launch func(*runner, app, string, runOpts) error
	}{
		{"ios", runIOS},
		{"android", runAndroid},
	} {
		t.Run(platform.name, func(t *testing.T) {
			for _, custom := range []bool{false, true} {
				application.HostTemplate = ""
				if custom {
					application.HostTemplate = "caller-owned.tmpl"
				}
				var output bytes.Buffer
				runner := &runner{ctx: context.Background(), out: &output, dryRun: true}
				if err := platform.launch(runner, application, "artifact", runOpts{}); err != nil {
					t.Fatal(err)
				}
				hasInspector := strings.Contains(output.String(), "Inspector:") || strings.Contains(output.String(), "DevTools:")
				if hasInspector == custom {
					t.Fatalf("custom=%t: unexpected inspector hint: %s", custom, output.String())
				}
			}
		})
	}
	application.Renderer, application.HostTemplate = "native", ""
	var output bytes.Buffer
	if err := runIOS(&runner{ctx: context.Background(), out: &output, dryRun: true}, application, "artifact", runOpts{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Inspector:") {
		t.Fatal("native renderer advertised a WebView inspector")
	}
}
