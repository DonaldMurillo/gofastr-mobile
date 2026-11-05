package notesnative

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/device/photos"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func TestPhotosAttachThroughNativeBinding(t *testing.T) {
	directory := t.TempDir()
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Stop() })
	connection := photosHello(t)
	opened := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: "/files"})
	shown := filesAction(t, nativeui.Command{Connection: connection, Instance: opened.Instance, RequestID: "import", Action: "import"})
	imported := filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: opened.Instance, RequestID: "imported", FileID: shown.FileID,
		FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: &files.Document{Name: "note.md", MediaType: "text/markdown", Data: []byte("Photo note\n\nKeep this body.")}}})
	noteRoute := imported.Response.Navigation.Route
	attachmentList := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: noteRoute + "/files"})
	if attachmentList.Error != nil || len(attachmentList.Screen.Actions) != 4 {
		t.Fatalf("one Notes attachment screen did not expose its sources: %+v", attachmentList)
	}
	camera := nativeui.Command{Kind: "action", Connection: connection, Instance: attachmentList.Instance, RequestID: "camera", Action: "camera"}
	showcaseCommand(t, camera)
	camera.Kind = "complete"
	if unavailable := showcaseCommand(t, camera); unavailable.Error == nil || unavailable.Error.Code != "unsupported" || unavailable.FileID != "" {
		t.Fatalf("unavailable camera pretended to capture: %+v", unavailable)
	}
	for _, outcome := range []nativeui.EffectStatus{nativeui.EffectCancelled, nativeui.EffectDenied, nativeui.EffectFailed} {
		shown = filesAction(t, nativeui.Command{Connection: connection, Instance: attachmentList.Instance, RequestID: "photo-" + string(outcome), Action: "photo"})
		var policy photos.Request
		operation := shown.Response.Files.Extension
		if operation == nil || operation.Name != "gofastr.photos/v1" || json.Unmarshal([]byte(operation.Payload), &policy) != nil || policy.Source != photos.Library || policy.MaxDimension != photos.MaxDimension {
			t.Fatal("photo request lost source or bounds")
		}
		filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: attachmentList.Instance, RequestID: "result-" + string(outcome), FileID: shown.FileID,
			FileResult: &nativeui.FileResult{Status: outcome}})
	}
	before := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: noteRoute + "/files"})
	if len(before.Screen.List.Rows) != 0 {
		t.Fatal("cancellation or denial stored an attachment")
	}
	shown = filesAction(t, nativeui.Command{Connection: connection, Instance: attachmentList.Instance, RequestID: "photo-valid", Action: "photo"})
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 24)), nil); err != nil {
		t.Fatal(err)
	}
	document := &files.Document{Name: "photo.jpg", MediaType: "image/jpeg", Data: encoded.Bytes()}
	result := nativeui.Command{Kind: "file-result", Connection: connection, Instance: attachmentList.Instance, RequestID: "photo-result", FileID: shown.FileID,
		FileResult: &nativeui.FileResult{Status: nativeui.EffectCompleted, File: document}}
	attached := filesActionResult(t, result)
	filesActionResult(t, result)
	if attached.Response.Navigation == nil || !strings.HasPrefix(attached.Response.Navigation.Route, noteRoute+"/files/") {
		t.Fatal("photo did not use the existing private attachment workflow")
	}
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	if err := Start(directory); err != nil {
		t.Fatal(err)
	}
	connection = photosHello(t)
	listed := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: noteRoute + "/files"})
	if listed.Error != nil || len(listed.Screen.List.Rows) != 1 {
		t.Fatalf("photo was duplicated or lost across restart: %+v", listed)
	}
	detail := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: attached.Response.Navigation.Route})
	if detail.Error != nil || len(detail.Screen.Images) != 1 {
		t.Fatalf("the photo detail must actually display the photo: %+v", detail.Error)
	}
	export := filesAction(t, nativeui.Command{Connection: connection, Instance: detail.Instance, RequestID: "export-photo", Action: "export"})
	if !bytes.Equal(export.Response.Files.Export.Data, document.Data) || export.Response.Files.Export.MediaType != "image/jpeg" {
		t.Fatal("stored photo bytes changed")
	}
	filesActionResult(t, nativeui.Command{Kind: "file-result", Connection: connection, Instance: detail.Instance, RequestID: "cancel-export", FileID: export.FileID, FileResult: &nativeui.FileResult{Status: nativeui.EffectCancelled}})
	note := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: noteRoute})
	if note.Screen.Form.Fields[1].Value != "Keep this body." {
		t.Fatal("photo attachment changed the note body")
	}
	if len(note.Screen.Images) != 1 || note.Screen.Images[0].Destination != attached.Response.Navigation.Route || note.Screen.Images[0].Alt == "" {
		t.Fatal("the saved note must contain a labelled, tappable photo preview")
	}
	preview := note.Screen.Images[0]
	config, err := jpeg.DecodeConfig(bytes.NewReader(preview.Data))
	if err != nil || config.Width != preview.Width || config.Height != preview.Height || config.Width > 512 || config.Height > 512 {
		t.Fatal("the editor preview must be a real, bounded JPEG")
	}
	remove := filesAction(t, nativeui.Command{Connection: connection, Instance: detail.Instance, RequestID: "remove-photo", Action: "remove"})
	filesActionResult(t, nativeui.Command{Kind: "respond", Connection: connection, Instance: detail.Instance, RequestID: "removed-photo", PresentationID: remove.PresentationID, Choice: "remove"})
	cleared := showcaseCommand(t, nativeui.Command{Kind: "open", Connection: connection, Route: noteRoute})
	if cleared.Error != nil || len(cleared.Screen.Images) != 0 || cleared.Screen.Form.Fields[1].Value != "Keep this body." {
		t.Fatal("removing a photo must remove only its preview, not the note text")
	}
}

func photosHello(t *testing.T) string {
	t.Helper()
	reply := showcaseCommand(t, nativeui.Command{Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: map[nativeui.Capability]nativeui.Support{
		nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable,
		nativeui.CapabilityPresentation: nativeui.SupportAvailable, nativeui.CapabilityFiles: nativeui.SupportAvailable,
		nativeui.CapabilityPhotos: nativeui.SupportAvailable, nativeui.CapabilityCamera: nativeui.SupportUnavailable,
	}}})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	return reply.Connection
}
