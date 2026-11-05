package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"strings"
	"testing"
)

func TestMicrophonePurposeIsExplicitAndEscaped(t *testing.T) {
	application := notesApp(t).forIOS()
	for _, purpose := range []string{"", `Record a voice note for "my notes" & keep it private.`} {
		application.MicrophoneUsage = purpose
		plist, err := render("templates/ios/Info.plist.tmpl", application)
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, "microphone purpose", plist)
		if bytes.Contains(plist, []byte("NSMicrophoneUsageDescription")) != (purpose != "") {
			t.Fatalf("incorrect microphone permission packaging for %q", purpose)
		}
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(purpose)); err != nil {
			t.Fatal(err)
		}
		if purpose != "" && !bytes.Contains(plist, escaped.Bytes()) {
			t.Fatal("microphone purpose is not XML escaped")
		}
	}
}

func TestMicrophonePurposeRejectsUnsafeOrUnsupportedBuilds(t *testing.T) {
	for _, purpose := range []string{strings.Repeat("x", 513), "bad\x00purpose", "bad\npurpose", string([]byte{0xff})} {
		var stdout, stderr bytes.Buffer
		code := run(context.Background(), []string{"build", "--platform", "ios-sim", "--pkg", "./examples/notes/notesnative", "--microphone-usage", purpose, "--dry-run"}, &stdout, &stderr)
		if code != 2 || !strings.Contains(stderr.String(), "--microphone-usage") || stdout.Len() != 0 {
			t.Fatalf("unsafe microphone purpose reached a build: %d %s %s", code, &stdout, &stderr)
		}
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"build", "--platform", "android-emu", "--pkg", notesPkg, "--microphone-usage", "Record voice notes.", "--dry-run"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--microphone-usage") || stdout.Len() != 0 {
		t.Fatalf("unsupported microphone build reached Android tooling: %d %s %s", code, &stdout, &stderr)
	}
}
