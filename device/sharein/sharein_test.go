package sharein

import (
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"strings"
	"testing"
)

func TestInboxRequestAndContentBoundaries(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, request := range []Request{{Operation: Next}, {Operation: Acknowledge, Identifier: id}} {
		if err := request.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range []Request{{}, {Operation: Next, Identifier: id}, {Operation: Acknowledge}, {Operation: Acknowledge, Identifier: "../private"}, {Operation: Acknowledge, Identifier: strings.Repeat("A", 32)}} {
		if request.Validate() == nil {
			t.Fatalf("accepted invalid request: %+v", request)
		}
	}
	item := Item{Identifier: id, Text: "A shared note"}
	if err := (Request{Operation: Next}).Accept(&item); err != nil {
		t.Fatal(err)
	}
	if (Request{Operation: Acknowledge, Identifier: id}).Accept(&item) == nil {
		t.Fatal("acknowledgement returned content")
	}
	for _, item := range []Item{{Identifier: id}, {Identifier: id, Text: strings.Repeat("x", MaxTextBytes+1)}, {Identifier: id, Text: "bad\x00text"}, {Identifier: id, Files: []files.Document{{Name: "../secret", MediaType: "text/plain"}}}, {Identifier: "untrusted", Text: "text"}} {
		if item.Validate() == nil {
			t.Fatal("accepted unsafe transfer")
		}
	}
	doc := files.Document{Name: "bounded.txt", MediaType: "text/plain", Data: make([]byte, MaxBytes/2)}
	item = Item{Identifier: id, Files: []files.Document{doc, doc}}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	item.Files = append(item.Files, files.Document{Name: "extra.txt", MediaType: "text/plain", Data: []byte{1}})
	if item.Validate() == nil {
		t.Fatal("accepted excessive combined bytes")
	}
}
