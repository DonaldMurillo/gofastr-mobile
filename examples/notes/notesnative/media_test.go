package notesnative

import (
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func screenMedia(t *testing.T, screen *nativeui.Screen) nativeui.Media {
	t.Helper()
	if screen == nil {
		t.Fatal("missing media screen")
	}
	if len(screen.Images) != 0 || len(screen.Attachments) != 0 || screen.FilePreview != nil {
		t.Fatal("Notes bypassed its compiled media rendering extension")
	}
	var media nativeui.Media
	if len(screen.Components) == 0 {
		return media
	}
	if len(screen.Components) != 1 || screen.Components[0].Name != "gofastr.media/v1" {
		t.Fatal("unexpected Notes rendering components")
	}
	if err := extension.DecodeComponent(screen.Components[0].Payload, &media); err != nil {
		t.Fatal(err)
	}
	return media
}
