package sim

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

func TestNotesBehaviorShowcaseWebView(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) { runNotesBehaviorShowcase(t, platform) })
	}
}

func runNotesBehaviorShowcase(t *testing.T, platform mobileui.Platform) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	_, host, err := notesapp.BuildScreensWithPlatform(t.TempDir(), origin, platform)
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
	ctx, traffic := mobileScreensBrowser(t, host, platform == mobileui.Android)
	if err := chromedp.Run(ctx, chromedp.Navigate(origin+"/"), clickMobileAction("Behaviors"), waitForPath("/behaviors", 10*time.Second), chromedp.WaitVisible(`a[href="/behaviors/alert"]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	for _, example := range []struct{ name, label string }{{"alert", "alert"}, {"confirmation", "confirmation"}, {"menu", "action menu"}, {"dialog", "dialog"}, {"sheet", "bottom sheet"}, {"selection", "single selection"}, {"multiple", "multiple selection"}} {
		actions := chromedp.Tasks{
			chromedp.Navigate(origin + "/behaviors/" + example.name),
			chromedp.Evaluate(`window.__showcaseMarker = 'same-document'`, nil),
			clickMobileAction("Show " + example.label),
			chromedp.WaitVisible(`[data-gfw-presentation]`, chromedp.ByQuery),
			chromedp.Poll(`!!document.activeElement?.closest('[data-gfw-presentation]')`, nil),
		}
		switch example.name {
		case "alert":
			actions = append(actions, clickMobileAction("Got it"))
		case "confirmation":
			actions = append(actions, clickMobileAction("Cancel"), presentationClosed(), clickMobileAction("Show confirmation"), chromedp.WaitVisible(`[data-gfw-presentation]`, chromedp.ByQuery), clickMobileAction("Reset example"), presentationClosed(), chromedp.Poll(`document.body.innerText.includes('Example reset confirmed')`, nil))
		case "menu":
			actions = append(actions, chromedp.Poll(`document.querySelector('[data-gfw-choice="unavailable"]').disabled`, nil), clickMobileAction("Reset example"), chromedp.Poll(`document.querySelector('[data-gfw-presentation]')?.innerText.includes('Reset this example?')`, nil), clickMobileAction("Cancel"))
		case "dialog":
			actions = append(actions, chromedp.KeyEvent("\u001b"), chromedp.Poll(`!!document.querySelector('[data-gfw-presentation]')`, nil), clickMobileAction("Cancel"))
		case "sheet":
			actions = append(actions, chromedp.KeyEvent("\u001b"))
		case "selection":
			actions = append(actions, chromedp.Poll(`document.querySelector('[data-gfw-option="shared"]').disabled`, nil), chromedp.Click(`[data-gfw-option="title"]`, chromedp.ByQuery), clickMobileAction("Apply"), waitForPath("/notes", 10*time.Second), chromedp.Poll(`location.search.includes('order=title')`, nil))
		case "multiple":
			actions = append(actions,
				chromedp.Poll(`document.querySelector('[data-gfw-option="shared"]').disabled`, nil),
				clickMobileAction("Apply"),
				chromedp.Poll(`document.querySelector('[data-gfw-presentation-status]')?.innerText.includes('required number')`, nil),
				chromedp.Click(`[data-gfw-option="ideas"]`, chromedp.ByQuery),
				chromedp.Click(`[data-gfw-option="work"]`, chromedp.ByQuery),
				chromedp.Click(`[data-gfw-option="personal"]`, chromedp.ByQuery),
				clickMobileAction("Apply"), presentationClosed(),
				chromedp.Poll(`document.body.innerText.includes('Selected tags: ideas, work, personal')`, nil))
		}
		actions = append(actions, presentationClosed(), chromedp.Poll(`window.__showcaseMarker === 'same-document'`, nil))
		if err := chromedp.Run(ctx, actions); err != nil {
			var body, location string
			_ = chromedp.Run(ctx, chromedp.Text("body", &body, chromedp.ByQuery), chromedp.Location(&location))
			t.Fatalf("%s/%s: %v\n%s\n%s", platform, example.name, err, location, body)
		}
	}
	var pixels []byte
	if err := chromedp.Run(ctx, chromedp.Navigate(origin+"/behaviors/multiple"), clickMobileAction("Show multiple selection"), chromedp.WaitVisible(`[data-gfw-presentation]`, chromedp.ByQuery), chromedp.CaptureScreenshot(&pixels)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shotsDir(t), fmt.Sprintf("%s-behavior-selection.png", platform)), pixels, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "dark"}}),
		chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.Poll(`(() => { const dialog = document.querySelector('[data-gfw-presentation]'); return dialog.scrollWidth <= dialog.clientWidth + 1 && parseFloat(getComputedStyle(dialog).getPropertyValue('--gfm-target')) >= `+fmt.Sprint(map[mobileui.Platform]int{mobileui.IOS: 44, mobileui.Android: 48}[platform])+`; })()`, nil)); err != nil {
		var metrics any
		_ = chromedp.Run(ctx, chromedp.Evaluate(`(() => { const dialog = document.querySelector('[data-gfw-presentation]'); return {width: dialog.clientWidth, scrollWidth: dialog.scrollWidth, target: getComputedStyle(dialog).getPropertyValue('--gfm-target')}; })()`, &metrics))
		t.Fatalf("%s large-text presentation: %v; metrics: %v", platform, err, metrics)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-gfw-choice="cancel"]`, chromedp.ByQuery), presentationClosed(),
		chromedp.Poll(`document.activeElement?.matches('button[form="gfw-form"][value="show"]')`, nil)); err != nil {
		var focused string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.activeElement?.outerHTML`, &focused))
		t.Fatalf("%s dismissal and focus restoration: %v; focused: %s", platform, err, focused)
	}
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	if len(traffic.errs) != 0 {
		t.Errorf("renderer errors: %v", traffic.errs)
	}
}

func presentationClosed() chromedp.Action {
	return chromedp.Poll(`!document.querySelector('[data-gfw-presentation]')`, nil)
}
