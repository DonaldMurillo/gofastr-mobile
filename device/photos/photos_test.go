package photos

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/DonaldMurillo/gofastr-mobile/device/files"
)

func TestRequestChecksRealBoundedJPEG(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 16, 12)), nil); err != nil {
		t.Fatal(err)
	}
	document := files.Document{Name: "photo.JPG", MediaType: "image/jpeg", Data: encoded.Bytes()}
	request := Request{Title: "Choose photo", Source: Library, MaxBytes: files.MaxBytes, MaxDimension: 16}
	if err := request.Accept(document); err != nil {
		t.Fatal(err)
	}
	for _, source := range []Source{Library, Camera} {
		request.Source = source
		if err := request.Validate(); err != nil {
			t.Fatalf("valid source %q: %v", source, err)
		}
	}
	for _, invalid := range []files.Document{
		{Name: "photo.jpg", MediaType: "image/jpeg", Data: []byte("not an image")},
		{Name: "photo.png", MediaType: "image/jpeg", Data: encoded.Bytes()},
		{Name: "photo.jpg", MediaType: "text/plain", Data: encoded.Bytes()},
		{Name: "../photo.jpg", MediaType: "image/jpeg", Data: encoded.Bytes()},
		{Name: "photo.jpg", MediaType: "image/jpeg", Data: encoded.Bytes()[:encoded.Len()/2]},
	} {
		if err := request.Accept(invalid); err == nil {
			t.Fatalf("accepted invalid document %q", invalid.Name)
		}
	}
	request.MaxDimension = 15
	if err := request.Accept(document); err == nil {
		t.Fatal("accepted pixels beyond the requested dimension")
	}
	request.MaxDimension, request.MaxBytes = 16, encoded.Len()-1
	if err := request.Accept(document); err == nil {
		t.Fatal("accepted bytes beyond the requested bound")
	}
}

func TestRequestRejectsInvalidLimits(t *testing.T) {
	valid := Request{Title: "Take photo", Source: Camera, MaxBytes: files.MaxBytes, MaxDimension: MaxDimension}
	for _, change := range []func(*Request){
		func(request *Request) { request.Source = "video" },
		func(request *Request) { request.Title = "" },
		func(request *Request) { request.MaxBytes = 0 },
		func(request *Request) { request.MaxBytes++ },
		func(request *Request) { request.MaxDimension = 0 },
		func(request *Request) { request.MaxDimension++ },
	} {
		invalid := valid
		change(&invalid)
		if err := invalid.Validate(); err == nil {
			t.Fatalf("accepted invalid request: %+v", invalid)
		}
	}
}
