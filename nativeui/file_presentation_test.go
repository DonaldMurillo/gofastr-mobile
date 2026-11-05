package nativeui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFilePresentationMetadata(t *testing.T) {
	for _, example := range []struct {
		mediaType string
		size      int
		detail    string
		icon      Icon
	}{
		{"text/markdown", 85, "Markdown · 85 B", IconFile},
		{"application/pdf", 4096, "PDF document · 4 KB", IconFile},
		{"image/jpeg", 1 << 20, "Photo · 1.0 MB", IconPhoto},
		{"audio/mp4", 56082, "Audio · 55 KB", IconAudio},
	} {
		attachment := Attachment{ID: "fixture", Name: "Fixture", MediaType: example.mediaType, Size: example.size, Destination: "/files/fixture"}
		encoded, err := json.Marshal(attachment)
		if err != nil {
			t.Fatal(err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(encoded, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata["detail"] != example.detail || metadata["icon"] != string(example.icon) {
			t.Fatalf("metadata: %s", encoded)
		}
	}
}

func TestFilePreviewBounds(t *testing.T) {
	file := Attachment{ID: "fixture", Name: "Thoughts.md", MediaType: "text/markdown", Size: 16384, Destination: "/files/fixture"}
	if err := validateFilePreview(FilePreview{File: file, Text: strings.Repeat("a", 16384)}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{strings.Repeat("a", 16385), "invalid\xff", "null\x00text"} {
		if err := validateFilePreview(FilePreview{File: file, Text: text}); err == nil {
			t.Fatalf("accepted invalid preview of %d bytes", len(text))
		}
	}
}
