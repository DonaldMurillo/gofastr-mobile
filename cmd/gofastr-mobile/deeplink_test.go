package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestURLSchemePackagingIsOptIn(t *testing.T) {
	application := nativeApp(t)
	for _, scheme := range []string{"", "notes-demo", "dev.gofastr.notes"} {
		application.URLScheme = scheme
		plist, err := render("templates/ios/Info.plist.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, "deep-link Info.plist", plist)
		swift, err := render("templates/ios/App.swift.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(swift, []byte("applicationDidEnterBackground")) || !bytes.Contains(swift, []byte("renderer.checkpoint()")) {
			t.Fatal("native lifecycle does not checkpoint Go-backed editor state")
		}
		if bytes.Contains(plist, []byte("CFBundleURLTypes")) != (scheme != "") || bytes.Contains(swift, []byte("renderer.openURL(url)")) != (scheme != "") {
			t.Fatalf("deep-link registration is not opt-in for %q", scheme)
		}
		if scheme != "" {
			for _, expected := range []string{"options?[.url]", "guard url.scheme == \"" + scheme + "\"", "renderer.openURL(url)"} {
				if !bytes.Contains(swift, []byte(expected)) {
					t.Fatalf("deep-link bootstrap lacks %s", expected)
				}
			}
			if !bytes.Contains(plist, []byte("<string>"+scheme+"</string>")) || bytes.Count(swift, []byte("\n")) > 75 {
				t.Fatal("deep-link packaging lost its scheme or introduced screen behavior into bootstrap")
			}
		}
	}
}

func TestURLSchemeCLIValidation(t *testing.T) {
	for _, scheme := range []string{"https", "file", "javascript", "gofastr", "BadScheme", "notes://", "notes_demo", "a\x00b", strings.Repeat("n", 65)} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--renderer", "native", "--pkg", "./examples/notes/notesnative", "--url-scheme", scheme, "--dry-run"}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "--url-scheme") || stdout.Len() != 0 {
			t.Fatalf("unsafe scheme reached build: %q: %d %s %s", scheme, code, &stdout, &stderr)
		}
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--pkg", "./examples/notes/notesmobile", "--url-scheme", "notes-demo", "--dry-run"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "--renderer native") {
		t.Fatalf("unsupported host silently accepted deep links: %d %s", code, &stderr)
	}
}
