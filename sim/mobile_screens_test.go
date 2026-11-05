package sim

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
)

func TestSharedMobileScreensWebView(t *testing.T) {
	for _, platform := range []mobileui.Platform{mobileui.IOS, mobileui.Android} {
		t.Run(string(platform), func(t *testing.T) { runSharedMobileScreens(t, platform) })
	}
}

func runSharedMobileScreens(t *testing.T, platform mobileui.Platform) {
	t.Cleanup(func() { owner.SetExtractor(nil) })
	dataDir := t.TempDir()
	app, host, err := notesapp.BuildScreensWithPlatform(dataDir, origin, platform)
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
	shots := shotsDir(t)
	shot := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			var pixels []byte
			if err := chromedp.CaptureScreenshot(&pixels).Do(ctx); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(shots, string(platform)+"-shared-"+name+".png"), pixels, 0o600)
		})
	}
	var savedMarker, invalidTitle, invalidBody, renderedPlatform, createdRoute string
	var layoutFits, editorFits bool
	err = chromedp.Run(ctx,
		chromedp.Navigate(origin+"/"),
		chromedp.WaitVisible(`input[type="search"]`, chromedp.ByQuery),
		chromedp.Poll(`!!window.__simPort && typeof window.__gofastr_transport === 'function'`, nil),
		chromedp.AttributeValue(`[data-gfm-platform]`, "data-gfm-platform", &renderedPlatform, nil, chromedp.ByQuery),
		chromedp.Evaluate(`window.__sharedScreenMarker = 'same-document'`, nil),
		clickMobileAction("New note"),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		waitForFileScreen("/notes/new"),
		setMobileField(`input[name="title"]`, "   "),
		setMobileField(`textarea[name="body"]`, "An unsaved thought <not markup>."),
		clickMobileAction("Done"),
		chromedp.Poll(`document.querySelector('[data-gfw-field="title"]').getAttribute('aria-invalid') === 'true' && document.querySelector('[data-gfw-field-error="title"]').textContent === 'Required.'`, nil),
		chromedp.Value(`input[name="title"]`, &invalidTitle, chromedp.ByQuery),
		chromedp.Value(`textarea[name="body"]`, &invalidBody, chromedp.ByQuery),
		setMobileField(`input[name="title"]`, "One definition, two renderers"),
		chromedp.Poll(`!!document.querySelector('[data-gfw-screen]').dataset.gfwInstance`, nil),
		shot("editor-light"),
		chromedp.Evaluate(`window.__originalPortMessage = window.__simPort.onmessage; window.__simPort.onmessage = event => {
			const request = JSON.parse(event.data);
			if (request.method === 'POST' && new URL(request.url).pathname === '/_mobile/actions' && JSON.parse(atob(request.body || '')).kind === 'action') {
				window.__releaseSubmit = () => window.__originalPortMessage(event);
			} else window.__originalPortMessage(event);
		}`, nil),
		clickMobileAction("Done"),
		chromedp.Poll(`typeof window.__releaseSubmit === 'function' && !document.querySelector('input[name="title"]').readOnly && !document.querySelector('textarea[name="body"]').readOnly`, nil),
		setMobileField(`textarea[name="body"]`, "Newer edits during save."),
		chromedp.Evaluate(`document.querySelector('textarea[name="body"]').focus(); document.querySelector('textarea[name="body"]').setSelectionRange(5, 5)`, nil),
		chromedp.Evaluate(`window.__simPort.onmessage = window.__originalPortMessage; window.__releaseSubmit()`, nil),
		chromedp.Poll(`location.pathname.startsWith('/notes/') && location.pathname !== '/notes/new' && document.body.innerText.includes('Newer edits remain unsaved')`, nil),
		chromedp.Poll(`document.activeElement === document.querySelector('textarea[name="body"]') && document.activeElement.selectionStart === 5 && document.activeElement.value === 'Newer edits during save.'`, nil),
		chromedp.Location(&createdRoute),
		clickMobileAction("Done"),
		waitForFileScreen("/notes"),
		chromedp.Poll(`document.body.innerText.includes('One definition, two renderers')`, nil),
		chromedp.Evaluate(`window.__sharedScreenMarker || 'lost'`, &savedMarker),
		shot("list-light"),
		chromedp.Click(`a[href^="/notes/"]:not([href="/notes/new"])`, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForFileScreen(strings.TrimPrefix(createdRoute, origin)).Do(ctx)
		}),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Newer edits during save.'`, nil),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "dark"}}),
		shot("editor-dark"),
		setMobileField(`textarea[name="body"]`, "Unsaved text to share"),
		chromedp.Evaluate(`Object.defineProperty(navigator, 'share', {value: data => { window.__sharedText = data.text; return Promise.resolve(); }, configurable: true}); Object.defineProperty(navigator, 'canShare', {value: () => true, configurable: true})`, nil),
		clickMobileAction("Share note"),
		chromedp.WaitVisible(`[data-gfw-service-perform]`, chromedp.ByQuery),
		chromedp.Click(`[data-gfw-service-perform]`, chromedp.ByQuery),
		chromedp.Poll(`!document.querySelector('[data-gfw-service]') && window.__sharedText === 'One definition, two renderers\n\nUnsaved text to share'`, nil),
		clickMobileAction("Notes"),
		chromedp.WaitVisible(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		chromedp.Click(`dialog[open] [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		waitForFileScreen("/notes"),
		chromedp.WaitVisible(`a[href^="/notes/"]:not([href="/notes/new"])`, chromedp.ByQuery),
		chromedp.Click(`a[href^="/notes/"]:not([href="/notes/new"])`, chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForFileScreen(strings.TrimPrefix(createdRoute, origin)).Do(ctx)
		}),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Unsaved text to share' && document.body.innerText.includes('draft restored')`, nil),
		chromedp.Poll(`JSON.parse(document.querySelector('[data-gfw-screen]').dataset.gfwEditor || '{}').draftStored === true`, nil),
		chromedp.Evaluate(`sessionStorage.clear()`, nil),
		chromedp.Reload(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return waitForFileScreen(strings.TrimPrefix(createdRoute, origin)).Do(ctx)
		}),
		chromedp.Poll(`document.querySelector('textarea[name="body"]')?.value === 'Unsaved text to share' && document.body.innerText.includes('draft restored')`, nil),
		clickMobileAction("Discard Changes"),
		chromedp.Poll(`document.querySelector('textarea[name="body"]').value === 'Newer edits during save.'`, nil),
		emulation.SetDeviceMetricsOverride(320, 844, 3, true),
		chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.Evaluate(`document.documentElement.scrollWidth <= innerWidth`, &editorFits),
		shot("editor-large-text"),
		emulation.SetDeviceMetricsOverride(390, 844, 3, true),
		chromedp.Evaluate(`document.documentElement.style.fontSize = ''`, nil),
		clickMobileAction("Done"),
		waitForFileScreen("/notes"),
		setMobileField(`input[type="search"]`, "does-not-match"),
		chromedp.Evaluate(`document.querySelector('input[type="search"]').form.requestSubmit()`, nil),
		chromedp.Poll(`document.body.innerText.includes('No matching notes')`, nil),
		setMobileField(`input[type="search"]`, "two renderers"),
		chromedp.Evaluate(`document.querySelector('input[type="search"]').form.requestSubmit()`, nil),
		chromedp.Poll(`document.body.innerText.includes('One definition, two renderers')`, nil),
		clickMobileAction("About Notes"),
		chromedp.Poll(`document.body.innerText.includes('Your choice of renderer.')`, nil),
		clickMobileAction("Done"),
		waitForComponentPath("/notes"),
		chromedp.Poll(`!!document.querySelector('[data-gfw-screen]').dataset.gfwInstance`, nil),
		chromedp.WaitVisible(`input[type="search"]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.documentElement.scrollWidth <= innerWidth`, &layoutFits),
		shot("list-dark"),
	)
	if err != nil {
		var location, text string
		_ = chromedp.Run(ctx, chromedp.Location(&location), chromedp.Text("body", &text, chromedp.ByQuery), shot("failure"))
		traffic.mu.Lock()
		paths := append([]string(nil), traffic.paths...)
		failures := append([]string(nil), traffic.errs...)
		traffic.mu.Unlock()
		t.Fatalf("shared screens: %v\nlocation: %s\nbody: %s\nrequests: %v\nerrors: %v", err, location, text, paths, failures)
	}
	if invalidTitle != "   " || invalidBody != "An unsaved thought <not markup>." {
		t.Errorf("validation lost the editing buffer: %q %q", invalidTitle, invalidBody)
	}
	if renderedPlatform != string(platform) || savedMarker != "same-document" || !layoutFits || !editorFits {
		t.Errorf("platform=%q marker=%q list fits=%v editor fits=%v", renderedPlatform, savedMarker, layoutFits, editorFits)
	}
	user, err := localapp.LoadOrMint(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := notesapp.Service(app).List(handler.SetUser(context.Background(), user), "")
	if err != nil || len(items) != 1 || items[0].Title != "One definition, two renderers" || items[0].Body != "Newer edits during save." || !strings.HasSuffix(createdRoute, "/notes/"+items[0].ID) {
		t.Errorf("shared actions persisted %+v, %v", items, err)
	}
	if traffic.bridged.Load() < 1 || traffic.blocked.Load() != 0 {
		t.Errorf("transport: bridged=%d blocked=%d", traffic.bridged.Load(), traffic.blocked.Load())
	}
	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	if len(traffic.errs) != 0 {
		t.Errorf("browser errors: %v", traffic.errs)
	}
	t.Logf("screenshots: %s; actions use the local host bridge", shots)
}

func clickMobileAction(label string) chromedp.Action {
	find := fmt.Sprintf(`Array.from(document.querySelectorAll('button,a')).find(element => !element.disabled && (element.getAttribute('aria-label') === %s || element.textContent.trim() === %s))`, mustJSON(label), mustJSON(label))
	return chromedp.Tasks{
		chromedp.Poll(`!!(`+find+`)`, nil),
		chromedp.Evaluate(`(`+find+`).click()`, nil),
	}
}

func setMobileField(selector, value string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`(() => {
		const field = document.querySelector(%s);
		field.value = %s;
		field.dispatchEvent(new Event('input', {bubbles: true}));
	})()`, mustJSON(selector), mustJSON(value)), nil)
}

func mobileScreensBrowser(t *testing.T, host *mobilehost.Host, android bool, options ...chromedp.ExecAllocatorOption) (context.Context, *stats) {
	t.Helper()
	executable := os.Getenv("SIM_CHROME")
	if executable == "" {
		for _, candidate := range []string{"/opt/pw-browsers/chromium-1243/chrome-linux64/chrome", "/usr/bin/google-chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"} {
			if _, err := os.Stat(candidate); err == nil {
				executable = candidate
				break
			}
		}
	}
	if executable == "" {
		t.Skip("no Chromium found; set SIM_CHROME")
	}
	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(executable), chromedp.NoSandbox, chromedp.Headless,
		chromedp.ProxyServer("http://127.0.0.1:9"), chromedp.WSURLReadTimeout(60*time.Second))
	allocatorOptions = append(allocatorOptions, options...)
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	t.Cleanup(cancelAllocator)
	ctx, cancelBrowser := chromedp.NewContext(allocator, chromedp.WithBrowserOption(chromedp.WithDialTimeout(60*time.Second)))
	t.Cleanup(cancelBrowser)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	t.Cleanup(cancel)
	traffic := &stats{shown: map[string]mobilehost.BridgeReply{}}
	chromedp.ListenTarget(ctx, func(event any) {
		switch event := event.(type) {
		case *fetch.EventRequestPaused:
			go serve(ctx, host, traffic, android, event)
		case *cdpruntime.EventBindingCalled:
			if event.Name == "__simNativePost" {
				go relay(ctx, host, traffic, event.Payload)
			}
		case *page.EventLoadEventFired:
			go func() {
				executor := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
				if _, _, err := cdpruntime.Evaluate(deliverPortJS).Do(executor); err != nil {
					traffic.note("bridge port: %v", err)
				}
			}()
		case *page.EventJavascriptDialogOpening:
			go func() {
				executor := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
				if err := page.HandleJavaScriptDialog(true).Do(executor); err != nil {
					traffic.note("leave/draft confirmation: %v", err)
				}
			}()
		case *cdpruntime.EventExceptionThrown:
			traffic.note("page exception: %s", event.ExceptionDetails.Error())
		case *cdpruntime.EventConsoleAPICalled:
			if event.Type == cdpruntime.APITypeError {
				var messages []string
				for _, argument := range event.Args {
					messages = append(messages, string(argument.Value)+argument.Description)
				}
				traffic.note("console.error: %s", strings.Join(messages, " "))
			}
		}
	})
	if err := chromedp.Run(ctx, cdpruntime.AddBinding("__simNativePost"), page.Enable(),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		emulation.SetDeviceMetricsOverride(390, 844, 3, true), emulation.SetTouchEmulationEnabled(true),
		emulation.SetUserAgentOverride(iPhoneUA),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "light"}})); err != nil {
		t.Fatal(err)
	}
	return ctx, traffic
}
