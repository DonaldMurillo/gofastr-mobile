package sharing

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-mobile/device/files"
)

func TestDecodedCombinedBytesAndClipboard(t *testing.T) {
	document := files.Document{Name: "local.txt", MediaType: "text/plain", Data: make([]byte, MaxBytes/2)}
	request := Request{Text: strings.Repeat("x", MaxTextBytes), Files: []files.Document{document, document}}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	request.Files = append(request.Files, files.Document{Name: "extra", MediaType: "text/plain", Data: []byte{1}})
	if request.Validate() == nil {
		t.Fatal("combined decoded byte bound bypassed")
	}
	request.Files, request.Text = nil, ""
	if request.Validate() == nil {
		t.Fatal("empty share accepted")
	}
	for _, text := range []string{strings.Repeat("x", MaxTextBytes+1), "bad\x00value", string([]byte{0xff})} {
		if (Clipboard{Text: text}).Validate() == nil {
			t.Fatal("unsafe clipboard accepted")
		}
	}
	if err := (Clipboard{}).Validate(); err != nil {
		t.Fatal("explicit clear should be allowed", err)
	}
	request = Request{Files: []files.Document{document, document, document, document, document}}
	if request.Validate() == nil {
		t.Fatal("too many shared files accepted")
	}
}
