package nativeui

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAttachmentWireRoundTripRejectsSpoofedDerivedMetadata(t *testing.T) {
	file := Attachment{ID: "opaque", Name: "brief.txt", MediaType: "text/plain", Size: 18, Destination: "/file"}
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Attachment
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Name != file.Name || decoded.Destination != file.Destination {
		t.Fatal("attachment cannot round trip through a rendering payload", err)
	}
	for _, payload := range []string{
		`{"name":"brief.txt","mediaType":"text/plain","size":18,"detail":"Trusted system file"}`,
		`{"name":"brief.txt","mediaType":"text/plain","size":18,"icon":"photo"}`,
		`{"name":"brief.txt","mediaType":"text/plain","size":18,"html":"<script>"}`,
	} {
		if err := json.Unmarshal([]byte(payload), &decoded); err == nil {
			t.Fatal("invalid attachment presentation payload accepted")
		}
	}
}

func TestAttachmentMetadataAndDestinations(t *testing.T) {
	file := Attachment{ID: "opaque", Name: "brief.txt", MediaType: "text/plain", Size: 18, Destination: "/file"}
	app, err := New("/file", Route{Path: "/file", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Note", Text: "Body", Attachments: []Attachment{file}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Render(context.Background(), "/file", nil); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"", "https://example.com/file", "/missing", "file:///secret"} {
		file.Destination = destination
		if _, err := app.Render(context.Background(), "/file", nil); err == nil {
			t.Fatalf("unregistered attachment destination accepted: %q", destination)
		}
	}
	file.Destination = "/file"
	if err := validateAttachments([]Attachment{file, file}); err == nil {
		t.Fatal("duplicate attachments accepted")
	}
	file.Name = "../secret"
	if err := validateAttachments([]Attachment{file}); err == nil {
		t.Fatal("file paths accepted as metadata")
	}
}
