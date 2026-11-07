package galleryweb

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/nativeui/devtrace"
)

func TestBuildServesGalleryInProcess(t *testing.T) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	if _, _, err := Build("relative", mobileui.IOS, nativeui.RuntimeOptions{}); err == nil {
		t.Fatal("relative data directory accepted")
	}
	recorder := devtrace.NewRecorder(0)
	host, renderer, err := Build(t.TempDir(), mobileui.Android, nativeui.RuntimeOptions{Observer: recorder.Observe})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Stop(ctx); err != nil {
			t.Fatal(err)
		}
	}()
	response, err := host.Do(context.Background(), &mobilehost.Request{Method: http.MethodGet, URL: mobilehost.DefaultOrigin + "/", Header: http.Header{}})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusOK || !strings.Contains(string(response.Body), "data-gfw-screen") || !strings.Contains(string(response.Body), "Controls") {
		t.Fatalf("root page: %d %.400s", response.Status, response.Body)
	}
	if response.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("security headers missing")
	}
	if renderer.Runtime() == nil || len(renderer.Runtime().Inspect().Connections) != 0 {
		t.Fatal("renderer runtime unavailable or already connected before the page said hello")
	}
}
