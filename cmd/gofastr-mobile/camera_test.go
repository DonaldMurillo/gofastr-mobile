package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestCameraPurposeIsPackagedAndEscaped(t *testing.T) {
	application := notesApp(t).forIOS()
	for _, purpose := range []string{defaultCameraUsage, `Attach a photo to "my notes" & keep it private.`, ""} {
		application.CameraUsage = purpose
		plist, err := render("templates/ios/Info.plist.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		decoder := xml.NewDecoder(bytes.NewReader(plist))
		found := false
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("invalid plist XML: %v", err)
			}
			if text, ok := token.(xml.CharData); ok && string(text) == "NSCameraUsageDescription" {
				found = true
			}
		}
		if found != (purpose != "") || bytes.Contains(plist, []byte("NSPhotoLibraryUsageDescription")) {
			t.Fatalf("incorrect camera/library permission packaging for %q", purpose)
		}
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(purpose)); err != nil {
			t.Fatal(err)
		}
		if purpose != "" && !bytes.Contains(plist, escaped.Bytes()) {
			t.Fatal("purpose text was changed or not XML escaped")
		}
	}
}

func TestCameraPurposeCLIValidation(t *testing.T) {
	for _, purpose := range []string{strings.Repeat("x", 513), "bad\x00purpose", "bad\npurpose", string([]byte{0xff})} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--pkg", "./examples/notes/notesnative", "--camera-usage", purpose, "--dry-run"}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "--camera-usage") || stdout.Len() != 0 {
			t.Fatalf("invalid purpose reached a build: %d %s %s", code, &stdout, &stderr)
		}
	}
}
