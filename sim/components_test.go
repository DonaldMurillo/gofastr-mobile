package sim

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notes"
	"github.com/DonaldMurillo/gofastr-mobile/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-mobile/mobileui"
)

func TestMobileComponentsWebView(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) { runMobileComponents(t, platform) })
	}
}

func runMobileComponents(t *testing.T, platform mobileui.Platform) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	directory := t.TempDir()
	app, host, err := notesapp.BuildScreensWithPlatform(directory, origin, platform)
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
	user, err := localapp.LoadOrMint(directory)
	if err != nil {
		t.Fatal(err)
	}
	service := notesapp.Service(app)
	for index := 0; index < 24; index++ {
		if _, err := service.Save(handler.SetUser(context.Background(), user), "", fmt.Sprintf("Seed note %02d", index), "Reusable collection fixture"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, traffic := mobileScreensBrowser(t, host, platform == mobileui.Android)
	stage := 0
	check := func(actions ...chromedp.Action) {
		t.Helper()
		stage++
		t.Logf("component stage %d", stage)
		stageContext, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := chromedp.Run(stageContext, actions...); err != nil {
			var body, location string
			var renderedState any
			var failurePixels []byte
			_ = chromedp.Run(ctx, chromedp.Text("body", &body, chromedp.ByQuery), chromedp.Location(&location), chromedp.CaptureScreenshot(&failurePixels), chromedp.Evaluate(`({config: JSON.parse(document.querySelector('[data-gfw-config]').dataset.gfwConfig).route, body: document.querySelector('textarea[name="body"]')?.value})`, &renderedState))
			_ = os.WriteFile(filepath.Join(shotsDir(t), string(platform)+"-components-failure.png"), failurePixels, 0o600)
			t.Fatalf("%s component stage %d: %v\n%s\nstate: %v\n%s", platform, stage, err, location, renderedState, body)
		}
	}
	check(chromedp.Navigate(origin+"/notes"),
		chromedp.WaitVisible(`[data-gfw-tab="notes"]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-filters] > summary`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-gfw-filter="view"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-gfw-screen][data-gfw-instance]`, chromedp.ByQuery),
		chromedp.WaitVisible(`[data-gfw-load-more]`, chromedp.ByQuery))
	var initialRows int
	check(chromedp.Evaluate(`window.__collectionBeforePaging = {instance: document.querySelector('[data-gfw-screen]').dataset.gfwInstance}`, nil),
		chromedp.Evaluate(`document.querySelectorAll('a.gfm-row').length`, &initialRows),
		chromedp.Click(`[data-gfw-load-more]`, chromedp.ByQuery),
		chromedp.Poll(fmt.Sprintf(`document.querySelectorAll('a.gfm-row').length > %d && !!document.querySelector('[data-gfw-screen]').dataset.gfwInstance && window.__collectionBeforePaging.instance !== document.querySelector('[data-gfw-screen]').dataset.gfwInstance`, initialRows), nil, chromedp.WithPollingTimeout(10*time.Second)))
	runCollectionInteractions(t, service, handler.SetUser(context.Background(), user), check)
	var collectionPixels []byte
	check(chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.Poll(`document.documentElement.scrollWidth <= innerWidth + 1`, nil),
		chromedp.CaptureScreenshot(&collectionPixels),
		chromedp.Evaluate(`document.documentElement.style.fontSize = ''`, nil))
	if err := os.WriteFile(filepath.Join(shotsDir(t), string(platform)+"-mobile-collection.png"), collectionPixels, 0o600); err != nil {
		t.Fatal(err)
	}
	check(clickMobileAction("New note"), chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery))
	check(setMobileField(`input[name="title"]`, "Shared mobile controls"),
		setMobileField(`textarea[name="body"]`, "Saved controls and an unsaved draft."),
		chromedp.Click(`[data-gfw-details] > summary`, chromedp.ByQuery),
		chromedp.Click(`input[name="pinned"]`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const tag = document.querySelector('select[name="tag"]');
			tag.value = Array.from(tag.options).find(option => option.value && !option.disabled).value;
			tag.dispatchEvent(new Event('change', {bubbles: true}));
		})()`, nil),
		setMobileField(`input[name="reminder_date"]`, "2026-12-20"),
		setMobileField(`input[name="reminder_time"]`, "09:30"))
	check(clickMobileAction("Done"), waitForComponentPath("/notes"),
		chromedp.Poll(`Array.from(document.querySelectorAll('a.gfm-row')).some(row => row.textContent.includes('Shared mobile controls'))`, nil))
	check(chromedp.Evaluate(`Array.from(document.querySelectorAll('a.gfm-row')).find(row => row.textContent.includes('Shared mobile controls')).click()`, nil),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('input[name="pinned"]').checked && document.querySelector('select[name="tag"]').value !== '' && document.querySelector('input[name="reminder_date"]').value === '2026-12-20' && document.querySelector('input[name="reminder_time"]').value === '09:30'`, nil))
	var editorURL string
	check(chromedp.Location(&editorURL), setMobileField(`textarea[name="body"]`, "Draft survives switching tabs."),
		chromedp.Click(`[data-gfw-tab="files"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		waitForComponentPath("/files"))
	check(chromedp.Click(`[data-gfw-tab="notes"]`, chromedp.ByQuery), waitForComponentPath(strings.TrimPrefix(editorURL, origin)),
		chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Draft survives switching tabs.'`, nil))
	check(chromedp.Reload(),
		chromedp.WaitVisible(`textarea[name="body"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Draft survives switching tabs.' && document.querySelector('input[name="pinned"]').checked`, nil),
		chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.Poll(`document.documentElement.scrollWidth <= innerWidth + 1`, nil))
	var pixels []byte
	check(chromedp.CaptureScreenshot(&pixels))
	if err := os.WriteFile(filepath.Join(shotsDir(t), string(platform)+"-mobile-controls.png"), pixels, 0600); err != nil {
		t.Fatal(err)
	}
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	if len(traffic.errs) != 0 {
		t.Fatalf("component bridge errors: %v", traffic.errs)
	}
}

func runCollectionInteractions(t *testing.T, service *notes.Service, userContext context.Context, check func(...chromedp.Action)) {
	t.Helper()
	var selected []string
	check(chromedp.Click(`[data-gfw-selection-toggle]`, chromedp.ByQuery), chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-gfw-select]')).slice(0, 2).map(control => control.dataset.gfwSelect)`, &selected))
	if len(selected) != 2 {
		t.Fatalf("need two selectable rows, got %v", selected)
	}
	rowSelector := `[data-gfw-row="` + selected[0] + `"]`
	rowPresent := fmt.Sprintf(`!!document.querySelector(%s)`, mustJSON(rowSelector))
	check(chromedp.Evaluate(fmt.Sprintf(`(() => {
		%s.forEach(identifier => document.querySelector('[data-gfw-select="' + identifier + '"]').click());
	})()`, mustJSON(selected)), nil))
	check(chromedp.Poll(`document.querySelectorAll('[data-gfw-select]:checked').length === 2`, nil),
		clickMobileAction("Archive selected"), chromedp.WaitVisible(`[data-gfw-choice="cancel"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__collectionBeforeCancel = document.querySelector('[data-gfw-screen]').dataset.gfwInstance`, nil),
		chromedp.Click(`[data-gfw-choice="cancel"]`, chromedp.ByQuery), presentationClosed(),
		chromedp.Poll(`window.__collectionBeforeCancel !== document.querySelector('[data-gfw-screen]').dataset.gfwInstance`, nil),
		chromedp.Poll(`document.querySelectorAll('[data-gfw-select]:checked').length === 2`, nil))
	for _, identifier := range selected {
		note, err := service.Get(userContext, identifier)
		if err != nil || note.Archived {
			t.Fatalf("cancel changed note %s: %+v, %v", identifier, note, err)
		}
	}
	check(clickMobileAction("Archive selected"), chromedp.WaitVisible(`[data-gfw-choice="apply"]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-choice="apply"]`, chromedp.ByQuery), presentationClosed(),
		chromedp.Poll("!"+rowPresent, nil), clickMobileAction("Undo last change (30 seconds)"), chromedp.Poll(rowPresent, nil))
	for _, identifier := range selected {
		note, err := service.Get(userContext, identifier)
		if err != nil || note.Archived {
			t.Fatalf("undo did not restore note %s: %+v, %v", identifier, note, err)
		}
	}
	check(chromedp.Click(`[data-gfw-select="`+selected[0]+`"]`, chromedp.ByQuery),
		setMobileFilter("view", "trash"),
		chromedp.Poll(`document.querySelector('[data-gfw-filter="view"]').value === 'trash' && !document.querySelector('[data-gfw-select]')`, nil),
		setMobileFilter("view", "active"), chromedp.Poll(rowPresent, nil),
		chromedp.Poll(`!document.querySelector('[data-gfw-select]:checked')`, nil))
	check(chromedp.Evaluate(fmt.Sprintf(`document.querySelector(%s).scrollIntoView({block: 'center'})`, mustJSON(rowSelector)), nil),
		chromedp.Click(rowSelector+` summary`, chromedp.ByQuery),
		chromedp.Poll(fmt.Sprintf(`document.querySelector(%s + ' details[open] > div')?.getBoundingClientRect().bottom <= document.querySelector('[aria-label="App tabs"]').getBoundingClientRect().top`, mustJSON(rowSelector)), nil),
		chromedp.Click(rowSelector+` [data-gfw-row-action="pin"]`, chromedp.ByQuery),
		chromedp.Poll(fmt.Sprintf(`document.querySelector(%s)?.textContent.includes('Pinned')`, mustJSON(rowSelector)), nil))
	check(componentTouchSwipe(rowSelector, "right"),
		chromedp.Poll(fmt.Sprintf(`!document.querySelector(%s)?.textContent.includes('Pinned')`, mustJSON(rowSelector)), nil))
	check(componentTouchSwipe(rowSelector, "left"),
		chromedp.Poll(fmt.Sprintf(`!!document.querySelector(%s)?.querySelector('details[open]')`, mustJSON(rowSelector)), nil),
		chromedp.Click(rowSelector+` summary`, chromedp.ByQuery))
	check(componentTouchSwipe(rowSelector, "up"),
		chromedp.Evaluate(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`, nil, func(parameters *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return parameters.WithAwaitPromise(true)
		}),
		waitForComponentPath("/notes"),
		chromedp.Poll(fmt.Sprintf(`!document.querySelector(%s)?.querySelector('details[open]')`, mustJSON(rowSelector)), nil))
	note, err := service.Get(userContext, selected[0])
	if err != nil || note.Pinned || note.Archived || note.Trashed {
		t.Fatalf("menu/vertical gestures mutated the note: %+v, %v", note, err)
	}
}

func setMobileFilter(key, value string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(() => {
		const control = document.querySelector('[data-gfw-filter="' + %s + '"]');
		control.value = %s;
		control.dispatchEvent(new Event('change', {bubbles: true}));
	})()`, mustJSON(key), mustJSON(value)), nil)
}

func componentTouchSwipe(selector, direction string) chromedp.Action {
	var bounds struct {
		Left, Top, Width, Height, StartX, RightX float64
	}
	return chromedp.Tasks{
		chromedp.ScrollIntoView(selector, chromedp.ByQuery),
		chromedp.Evaluate(fmt.Sprintf(`(() => { const row = document.querySelector(%s); const rect = row.getBoundingClientRect(); const link = row.querySelector('a.gfm-row').getBoundingClientRect(); return {Left: rect.left, Top: link.top, Width: rect.width, Height: link.height, StartX: link.left + 4, RightX: link.right - 4}; })()`, mustJSON(selector)), &bounds),
		chromedp.ActionFunc(func(ctx context.Context) error {
			startX, startY := bounds.StartX, bounds.Top+bounds.Height/2
			if direction == "left" {
				startX = bounds.RightX
			}
			if err := input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: startX, Y: startY}}).Do(ctx); err != nil {
				return err
			}
			for step := 1; step <= 8; step++ {
				endX, endY := startX, startY
				switch direction {
				case "right":
					endX += (bounds.Left + bounds.Width - 5 - startX) * float64(step) / 8
				case "left":
					endX += (bounds.Left + 5 - startX) * float64(step) / 8
				default:
					endY -= 80 * float64(step) / 8
				}
				if err := input.DispatchTouchEvent(input.TouchMove, []*input.TouchPoint{{X: endX, Y: endY}}).Do(ctx); err != nil {
					return err
				}
			}
			return input.DispatchTouchEvent(input.TouchEnd, []*input.TouchPoint{}).Do(ctx)
		}),
	}
}

func waitForComponentPath(route string) chromedp.Action {
	return chromedp.Poll(fmt.Sprintf(`(() => {
		const screen = document.querySelector('[data-gfw-config]');
		return screen && JSON.parse(screen.dataset.gfwConfig).route.split('?')[0] === %s;
	})()`, mustJSON(strings.Split(route, "?")[0])), nil, chromedp.WithPollingTimeout(10*time.Second))
}
