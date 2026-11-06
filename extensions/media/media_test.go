package media_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extensions/media"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestMediaBlockFreezesContentAndEventAllowlist(t *testing.T) {
	content := media.Content{Attachments: []nativeui.Attachment{{ID: "file", Name: "brief.txt", MediaType: "text/plain", Size: 12, Destination: "/file"}}, Preview: &nativeui.FilePreview{File: nativeui.Attachment{ID: "file", Name: "brief.txt", MediaType: "text/plain", Size: 12, Destination: "/file"}, Text: "Private text"}}
	component, err := media.Block("attachments", content)
	if err != nil {
		t.Fatal(err)
	}
	content.Attachments[0].Name = "later.txt"
	registry, err := extension.New(media.Definition())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateComponent(component); err != nil {
		t.Fatal(err)
	}
	if component.Destinations["file.0"] != "/file" || component.Destinations["preview"] != "/file" || strings.Contains(component.Payload, "later.txt") {
		t.Fatal("component was not frozen")
	}
	component.Destinations["file.0"] = "/different"
	if err := registry.ValidateComponent(component); err == nil {
		t.Fatal("forged component event admitted")
	}
}

func TestMediaBlockRejectsInvalidContentAndRemoteDestinations(t *testing.T) {
	if _, err := media.Block("empty", media.Content{}); err == nil {
		t.Fatal("empty component admitted")
	}
	if _, err := media.Block("bad", media.Content{Attachments: []nativeui.Attachment{{ID: "file", Name: "../secret", MediaType: "text/plain"}}}); err == nil {
		t.Fatal("invalid metadata admitted")
	}
	component, err := media.Block("attachments", media.Content{Attachments: []nativeui.Attachment{{ID: "file", Name: "brief.txt", MediaType: "text/plain", Size: 12, Destination: "https://example.com/private"}}})
	if err != nil {
		t.Fatal(err)
	}
	app, err := nativeui.NewWithExtensions("/", []extension.Definition{media.Definition()}, nativeui.Route{Path: "/", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Files", Components: []extension.Component{component}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Render(context.Background(), "/", nil); err == nil {
		t.Fatal("remote route escaped the Go navigator")
	}
}

func TestMediaPayloadRoundTripIsStrict(t *testing.T) {
	component, err := media.Block("detail", media.Content{Preview: &nativeui.FilePreview{File: nativeui.Attachment{ID: "file", Name: "brief.txt", MediaType: "text/plain", Destination: "/file"}, Text: "<script>plain text</script>"}})
	if err != nil {
		t.Fatal(err)
	}
	var content media.Content
	if err := extension.DecodeComponent(component.Payload, &content); err != nil || content.Preview.Text != "<script>plain text</script>" {
		t.Fatal("content roundtrip changed plain text", err)
	}
	var payload map[string]any
	_ = json.Unmarshal([]byte(component.Payload), &payload)
	payload["html"] = "<script>execute()</script>"
	encoded, _ := json.Marshal(payload)
	component.Payload = string(encoded)
	registry, _ := extension.New(media.Definition())
	if err := registry.ValidateComponent(component); err == nil {
		t.Fatal("undeclared presentation fields admitted")
	}
}
