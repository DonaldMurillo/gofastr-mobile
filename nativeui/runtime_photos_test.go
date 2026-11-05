package nativeui

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr-mobile/device/files"
	"github.com/DonaldMurillo/gofastr-mobile/device/photos"
)

func TestPhotosThroughCommandBoundary(t *testing.T) {
	var saved atomic.Int32
	app, err := New("/photos", Route{Path: "/photos", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Photos", Text: "Choose a source", Actions: []Action{{ID: "library", Label: "Choose photo"}, {ID: "camera", Label: "Take photo"}}}, nil
	}, Actions: map[string]Handle{
		"library": func(context.Context, Request) (Result, error) {
			return Result{Files: &FileRequest{ID: "photo", Commit: true, Photo: &photos.Request{Title: "Choose", Source: photos.Library, MaxBytes: files.MaxBytes, MaxDimension: 16}}}, nil
		},
		"camera": func(context.Context, Request) (Result, error) {
			return Result{Files: &FileRequest{ID: "photo", Commit: true, Photo: &photos.Request{Title: "Capture", Source: photos.Camera, MaxBytes: files.MaxBytes, MaxDimension: 16}}}, nil
		},
	}, Files: map[string]FileRespond{"photo": func(context.Context, Request, FileResult) (Result, error) {
		saved.Add(1)
		return Result{Announcement: "Photo attached"}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(app, RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	connection := presentationCommand(t, runtime, Command{Kind: "hello", Hello: &Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[Capability]Support{
		CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, CapabilityPhotos: SupportFallback,
	}}}).Connection
	opened := presentationCommand(t, runtime, Command{Kind: "open", Connection: connection, Route: "/photos"})
	start := Command{Kind: "action", Connection: connection, Instance: opened.Instance, RequestID: "camera", Action: "camera"}
	presentationCommand(t, runtime, start)
	start.Kind = "complete"
	if reply := presentationCommand(t, runtime, start); reply.Error == nil || reply.Error.Code != "unsupported" || reply.FileID != "" {
		t.Fatalf("camera availability was inferred from library support: %+v", reply)
	}
	start.Kind, start.RequestID, start.Action = "action", "library", "library"
	presentationCommand(t, runtime, start)
	start.Kind = "complete"
	shown := presentationCommand(t, runtime, start)
	if shown.Error != nil || shown.FileID == "" || shown.Response.Files.Photo.Source != photos.Library {
		t.Fatalf("library fallback required unrelated files/camera capability: %+v", shown)
	}
	finish := Command{Kind: "file-result", Connection: connection, Instance: opened.Instance, RequestID: "result", FileID: shown.FileID,
		FileResult: &FileResult{Status: EffectCompleted, File: &files.Document{Name: "fake.jpg", MediaType: "image/jpeg", Data: []byte("not a JPEG")}}}
	if reply := presentationCommand(t, runtime, finish); reply.Error == nil || reply.Error.Code != "validation" {
		t.Fatal("accepted an image label without real JPEG bytes")
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 16, 12)), nil); err != nil {
		t.Fatal(err)
	}
	finish.FileResult.File = &files.Document{Name: "photo.jpg", MediaType: "image/jpeg", Data: encoded.Bytes()}
	for attempt := 0; attempt < 2; attempt++ {
		if reply := presentationCommand(t, runtime, finish); reply.Error != nil {
			t.Fatal(reply.Error)
		}
	}
	finish.Kind = "complete"
	if reply := presentationCommand(t, runtime, finish); reply.Error != nil || reply.Response.Announcement != "Photo attached" {
		t.Fatalf("photo did not settle: %+v", reply)
	}
	start.Kind, start.RequestID = "action", "another-library"
	presentationCommand(t, runtime, start)
	start.Kind = "complete"
	shown = presentationCommand(t, runtime, start)
	finish.Kind, finish.RequestID, finish.FileID, finish.FileResult = "file-result", "cancel", shown.FileID, &FileResult{Status: EffectCancelled}
	presentationCommand(t, runtime, finish)
	finish.Kind = "complete"
	presentationCommand(t, runtime, finish)
	if saved.Load() != 1 {
		t.Fatalf("duplicate or cancelled photo wrote %d times", saved.Load())
	}
}

func TestPhotoFileRequestValidation(t *testing.T) {
	photo := &photos.Request{Title: "Photo", Source: photos.Camera, MaxBytes: files.MaxBytes, MaxDimension: photos.MaxDimension}
	handlers := map[string]FileRespond{"photo": func(context.Context, Request, FileResult) (Result, error) { return Result{}, nil }}
	if err := validateFiles(FileRequest{ID: "photo", Photo: photo}, handlers); err != nil {
		t.Fatal(err)
	}
	for _, request := range []FileRequest{
		{ID: "photo", Photo: photo, Pick: &files.Pick{Title: "Files", MaxBytes: files.MaxBytes}},
		{ID: "photo", Photo: photo, Export: &files.Document{Name: "empty.txt", MediaType: "text/plain"}},
		{ID: "unregistered", Photo: photo},
		{ID: "photo", Photo: &photos.Request{Title: "Video", Source: "video", MaxBytes: files.MaxBytes, MaxDimension: photos.MaxDimension}},
	} {
		if err := validateFiles(request, handlers); err == nil {
			t.Fatalf("accepted invalid photo request: %+v", request)
		}
	}
}
