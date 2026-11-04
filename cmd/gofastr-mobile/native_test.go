package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCallerOwnedNativeHostTemplate(t *testing.T) {
	application, err := newApp("github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesnative", "notesnative", "dev.gofastr.notesnative", "Native Notes", "")
	if err != nil {
		t.Fatal(err)
	}
	application.HostTemplate = filepath.Join("..", "..", "examples", "notes", "native", "ios", "App.swift.tmpl")
	swift, err := render("templates/ios/App.swift.tmpl", application.forIOS())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"import UIKit", "import Notesnative", "NotesnativeList(", "NotesnativeSave(", "UINavigationController", "UITextView", "UIActivityViewController", "keyboardLayoutGuide", "adjustsFontForContentSizeCategory"} {
		if !bytes.Contains(swift, []byte(expected)) {
			t.Errorf("native host lacks %s", expected)
		}
	}
	for _, forbidden := range []string{"WebKit", "WKWebView", "{{", "http://", "https://"} {
		if bytes.Contains(swift, []byte(forbidden)) {
			t.Errorf("native host contains %s", forbidden)
		}
	}
	plist, err := render("templates/ios/Info.plist.tmpl", application.forIOS())
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "custom host Info.plist", plist)
	if bytes.Contains(plist, []byte("import UIKit")) {
		t.Fatal("host override also replaced Info.plist")
	}
}

func TestNativeHostTemplateValidationAndDryRun(t *testing.T) {
	for _, source := range []string{filepath.Join(t.TempDir(), "missing.swift"), t.TempDir()} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--pkg", notesPkg, "--host-template", source, "--dry-run"}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "--host-template") {
			t.Fatalf("invalid source: exit %d, %s", code, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	output := t.TempDir()
	code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--pkg", "../../examples/notes/notesnative",
		"--host-template", "../../examples/notes/native/ios/App.swift.tmpl", "--out", output, "--dry-run"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "-framework Notesnative") {
		t.Fatalf("native build plan: exit %d, %s\n%s", code, stdout.String(), stderr.String())
	}
	if entries, _ := os.ReadDir(output); len(entries) != 0 {
		t.Fatal("dry run wrote build output")
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
}
