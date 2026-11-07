package sim

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/examples/gallery/gallery"
	"github.com/DonaldMurillo/gofastr-native/examples/gallery/galleryweb"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
	"github.com/DonaldMurillo/gofastr-native/nativeui/devtrace"
	"github.com/DonaldMurillo/gofastr-native/sim/browsersim"
)

// TestGalleryWebView walks the component gallery through sim/browsersim for
// both phone shapes. It checks behavior, not pixels; screenshots are saved
// to SIM_SHOTS for a person to inspect.
func TestGalleryWebView(t *testing.T) {
	if browsersim.Browser("") == "" {
		t.Skip("no Chromium found; set SIM_CHROME")
	}
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) { runGallery(t, platform) })
	}
}

func runGallery(t *testing.T, platform mobileui.Platform) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	recorder := devtrace.NewRecorder(devtrace.MaxSize)
	host, renderer, err := galleryweb.Build(t.TempDir(), platform, nativeui.RuntimeOptions{Observer: recorder.Observe})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := host.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	session, err := browsersim.Open(context.Background(), host, browsersim.Options{Platform: platform, Headless: true, Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx := session.Context()
	screen := func(path string) chromedp.Tasks {
		return chromedp.Tasks{chromedp.Navigate(origin + path), waitForPath(strings.SplitN(path, "?", 2)[0], 10*time.Second), chromedp.WaitVisible(`[data-gfw-screen]`, chromedp.ByQuery)}
	}
	bodyHas := func(text string) chromedp.Action {
		return chromedp.Poll(`document.body.innerText.includes(`+string(mustJSON(text))+`)`, nil, chromedp.WithPollingTimeout(10*time.Second))
	}
	run := func(name string, actions ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, actions...); err != nil {
			var body, location, status string
			_ = chromedp.Run(ctx, chromedp.Text("body", &body, chromedp.ByQuery), chromedp.Location(&location),
				chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-gfw-status],[data-gfw-field-error]')).map(e => e.outerHTML).join('\n')`, &status))
			var trace []string
			for _, event := range recorder.Events() {
				trace = append(trace, devtrace.Format(event))
			}
			t.Fatalf("%s/%s: %v\n%s\n%s\nstatus: %s\ntrace:\n%s\nerrors: %v", platform, name, err, location, body, status, strings.Join(trace[max(0, len(trace)-8):], "\n"), session.Errors())
		}
	}

	// Every tab and every row destination opens.
	app, err := gallery.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{gallery.Root, "/lists", "/feedback", "/device"} {
		run("tab "+root, screen(root))
		rendered, err := app.Render(context.Background(), root, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rendered.List.Rows {
			if row.Destination == "/device/protected" {
				continue
			}
			run("row "+row.Destination, screen(row.Destination))
		}
	}

	run("validation", screen("/gallery/fields"), setMobileField(`[name="name"]`, " "), chromedp.Click(`[data-gfw-header-menu] summary`, chromedp.ByQuery), clickMobileAction("Validate"),
		chromedp.Poll(`document.querySelector('[data-gfw-field-error="name"]').innerText === "Required."`, nil, chromedp.WithPollingTimeout(10*time.Second)))
	run("save fields", setMobileField(`[name="name"]`, "Ada"), clickMobileAction("Save"), bodyHas("Saved in memory."))
	run("unknown save", screen("/gallery/unknown"), clickMobileAction("Save"), bodyHas("The save outcome is unknown"))
	run("protected refuses", chromedp.Navigate(origin+"/device/protected"), bodyHas("privacy/v1"))
	run("capability matrix", screen("/device/capabilities"),
		// The WebView paints pages on the server through App.Render, before any
		// hello, so no negotiated contract exists there. The matrix must say so.
		bodyHas("Renderer: "+gallery.LegacyRenderPath), bodyHas("privacy/v1: "+gallery.LegacyRenderPath), bodyHas(gallery.MatrixFooter))
	run("row action", screen("/lists/browse"), bodyHas("Row 01"), bodyHas("20 rows shown"))
	run("live bump", screen("/lists/live"), clickMobileAction("Add"), bodyHas("Update 1"))
	for _, kind := range gallery.PresentationKinds {
		close := clickMobileAction("Cancel")
		switch kind {
		case "alert":
			close = clickMobileAction("OK")
		case "dialog":
			close = clickMobileAction("Not now")
		case "sheet":
			close = clickMobileAction("Close")
		}
		run("presentation "+kind, screen("/feedback/present/"+kind), clickMobileAction("Show"), chromedp.WaitVisible(`[data-gfw-presentation]`, chromedp.ByQuery), close, presentationClosed())
	}
	run("menu choice", screen("/feedback/present/menu"), clickMobileAction("Show"), chromedp.WaitVisible(`[data-gfw-presentation]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-gfw-choice="unavailable"]').disabled`, nil), clickMobileAction("First option"), presentationClosed(), bodyHas("You chose first."))
	run("announcement", screen("/feedback/effects/announcement"), clickMobileAction("Run"), bodyHas("This sentence came from Go."))
	run("push and pop", screen("/feedback/navigation"), clickMobileAction("Push"), waitForPath("/feedback/navigation/pushed", 10*time.Second), clickMobileAction("Pop"), waitForPath("/feedback/navigation", 10*time.Second))

	directory := shotsDir(t)
	for _, shot := range []struct{ path, name string }{{"/gallery/fields", "fields"}, {"/device/capabilities", "capabilities"}, {"/feedback/images", "images"}} {
		run("screenshot "+shot.name, screen(shot.path))
		if err := session.Screenshot(filepath.Join(directory, fmt.Sprintf("gallery-%s-%s.png", platform, shot.name))); err != nil {
			t.Fatal(err)
		}
	}
	dark, err := browsersim.Open(context.Background(), host, browsersim.Options{Platform: platform, Headless: true, Dark: true, Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(dark.Context(), chromedp.Navigate(origin+"/gallery/fields"), chromedp.WaitVisible(`[data-gfw-screen]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if err := dark.Screenshot(filepath.Join(directory, fmt.Sprintf("gallery-%s-fields-dark.png", platform))); err != nil {
		t.Fatal(err)
	}
	dark.Close()

	kinds := map[string]bool{}
	for _, event := range recorder.Events() {
		kinds[event.Kind] = true
	}
	for _, kind := range []string{"hello", "open", "action", "complete", "respond"} {
		if !kinds[kind] {
			t.Errorf("trace has no %s event: %v", kind, kinds)
		}
	}
	if snapshot := renderer.Runtime().Inspect(); len(snapshot.Connections) == 0 || len(snapshot.Sessions) == 0 {
		t.Errorf("Inspect saw no live state: %+v", snapshot)
	}
	for _, message := range session.Errors() {
		// The protected screen's refusal is expected; anything else is a renderer error.
		if !strings.Contains(message, "privacy/v1") {
			t.Errorf("renderer error: %s", message)
		}
	}
}
