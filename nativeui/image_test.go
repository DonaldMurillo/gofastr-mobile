package nativeui

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestImagePreviewBoundsAndRoutes(t *testing.T) {
	var source bytes.Buffer
	if err := png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 2048, 1024))); err != nil {
		t.Fatal(err)
	}
	preview, err := NewImage("photo", "A note photo", source.Bytes(), 512)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Width != 512 || preview.Height != 256 || len(preview.Data) > maxPreviewBytes {
		t.Fatal("preview did not preserve its aspect ratio within the display bounds")
	}
	if _, err := jpeg.Decode(bytes.NewReader(preview.Data)); err != nil {
		t.Fatal(err)
	}
	preview.Destination = "/photo"
	app, err := New("/photo", Route{Path: "/photo", Screen: func(context.Context, Request) (Screen, error) {
		return Screen{Title: "Note", Text: "Text", Images: []Image{preview}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Render(context.Background(), "/photo", nil); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{"https://example.com/image", "/missing"} {
		preview.Destination = destination
		if _, err := app.Render(context.Background(), "/photo", nil); err == nil {
			t.Fatalf("unregistered image destination was accepted: %q", destination)
		}
	}
	preview.Destination = ""
	preview.Width++
	if _, err := app.Render(context.Background(), "/photo", nil); err == nil {
		t.Fatal("incorrect image dimensions were accepted")
	}
	for _, input := range [][]byte{nil, []byte("fake image"), make([]byte, 1<<20+1)} {
		if _, err := NewImage("photo", "Photo", input, 512); err == nil {
			t.Fatal("invalid image was accepted")
		}
	}
}
