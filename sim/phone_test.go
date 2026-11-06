// Package sim drives the notes app in a Chromium set up as a phone
// WebView: iPhone-sized viewport, touch, mobile user agent, and EVERY
// request intercepted through the DevTools Fetch domain and answered by
// mobilehost in-process. Chromium never opens a network connection,
// which is exactly the iOS/Android shell's position (WKURLSchemeHandler /
// shouldInterceptRequest). What the simulation cannot show: the native
// platforms' own interception limits (no redirects, no cookies, see
// README), streamed SSE (CDP's fulfill is buffered), and real WebKit.
package sim

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/owner"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
)

const origin = mobilehost.DefaultOrigin

const offOriginProbeURL = "https://off-origin.example/probe"

// offOriginProbeJS hands the transport a POST Request for another
// origin and reports how it settled. The page's CSP (connect-src
// 'self') may refuse the fetch before it reaches the network; that
// refusal is reported as "csp:<url>".
const offOriginProbeJS = `new Promise((done) => {
	let csp = "";
	document.addEventListener("securitypolicyviolation", (e) => { csp = e.blockedURI; });
	window.__gofastr_transport(new Request("` + offOriginProbeURL + `", {method: "POST", body: "x"}))
		.then(() => done("resolved"), (e) => setTimeout(() => done(csp ? "csp:" + csp : e.message), 100));
})`

const iPhoneUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"

type stats struct {
	served, sse, blocked, bridged atomic.Int64
	mu                            sync.Mutex
	errs                          []string
	paths                         []string
	// shown holds a bridged native-form answer until the navigation the
	// sim starts for it arrives, the way a shell's loadDataWithBaseURL /
	// loadSimulatedRequest shows a document it already has.
	shown map[string]mobilehost.BridgeReply
}

func (s *stats) note(format string, a ...any) {
	s.mu.Lock()
	s.errs = append(s.errs, fmt.Sprintf(format, a...))
	s.mu.Unlock()
}

// serve answers one paused request from the in-process host.
func serve(ctx context.Context, h *mobilehost.Host, st *stats, android bool, ev *fetch.EventRequestPaused) {
	exec := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
	if ev.Request.Method == http.MethodGet {
		st.mu.Lock()
		r, ok := st.shown[ev.Request.URL]
		delete(st.shown, ev.Request.URL)
		st.mu.Unlock()
		if ok {
			body, _ := base64.StdEncoding.DecodeString(r.Body)
			var entries []*fetch.HeaderEntry
			for _, kv := range r.Headers {
				entries = append(entries, &fetch.HeaderEntry{Name: kv[0], Value: kv[1]})
			}
			st.mu.Lock()
			st.paths = append(st.paths, fmt.Sprintf("SHOW %s -> %d", ev.Request.URL, r.Status))
			st.mu.Unlock()
			_ = fetch.FulfillRequest(ev.RequestID, int64(r.Status)).WithResponseHeaders(entries).
				WithBody(base64.StdEncoding.EncodeToString(body)).Do(exec)
			return
		}
	}
	u, err := url.Parse(ev.Request.URL + ev.Request.URLFragment)
	if err != nil || !strings.HasPrefix(ev.Request.URL, origin+"/") && ev.Request.URL != origin {
		// Anything off-origin is what an offline shell would refuse.
		st.blocked.Add(1)
		st.note("blocked off-origin request %s", ev.Request.URL)
		_ = fetch.FailRequest(ev.RequestID, network.ErrorReasonBlockedByClient).Do(exec)
		return
	}
	if u.Path == "/__gofastr/sse" {
		// CDP fulfills with a complete body; a held stream cannot be
		// simulated here (mobilehost.Open covers it in Go tests).
		st.sse.Add(1)
		_ = fetch.FailRequest(ev.RequestID, network.ErrorReasonAborted).Do(exec)
		return
	}
	hdr := http.Header{}
	for k, v := range ev.Request.Headers {
		hdr.Set(k, fmt.Sprint(v))
	}
	var body []byte
	for _, e := range ev.Request.PostDataEntries {
		b, err := base64.StdEncoding.DecodeString(e.Bytes)
		if err != nil {
			st.note("bad post data on %s: %v", u.Path, err)
		}
		body = append(body, b...)
	}
	if android {
		// Android's WebResourceRequest carries no body. Every body the
		// app needs has to arrive over the bridge instead.
		body = nil
	}
	if ev.Request.HasPostData && len(ev.Request.PostDataEntries) == 0 {
		st.note("post data omitted by CDP on %s", u.Path)
	}
	rctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := h.Do(rctx, &mobilehost.Request{Method: ev.Request.Method, URL: ev.Request.URL, Header: hdr, Body: body})
	if err != nil {
		st.note("host error on %s %s: %v", ev.Request.Method, u.Path, err)
		_ = fetch.FailRequest(ev.RequestID, network.ErrorReasonFailed).Do(exec)
		return
	}
	st.served.Add(1)
	st.mu.Lock()
	st.paths = append(st.paths, fmt.Sprintf("%s %s -> %d", ev.Request.Method, u.RequestURI(), resp.Status))
	st.mu.Unlock()
	var entries []*fetch.HeaderEntry
	for k, vs := range resp.Header {
		for _, v := range vs {
			entries = append(entries, &fetch.HeaderEntry{Name: k, Value: v})
		}
	}
	if err := fetch.FulfillRequest(ev.RequestID, int64(resp.Status)).
		WithResponseHeaders(entries).
		WithBody(base64.StdEncoding.EncodeToString(resp.Body)).Do(exec); err != nil && !retiredInterception(err) {
		st.note("fulfill %s: %v", u.Path, err)
	}
}

// Navigation can abort a paused stylesheet before its response reaches CDP.
// That request no longer exists; other protocol and host failures remain errors.
func retiredInterception(err error) bool {
	var protocol *cdproto.Error
	return errors.As(err, &protocol) && protocol.Code == -32602 && protocol.Message == "Invalid InterceptionId."
}

func TestRetiredInterceptionDoesNotHideOtherProtocolFailures(t *testing.T) {
	if !retiredInterception(fmt.Errorf("response: %w", &cdproto.Error{Code: -32602, Message: "Invalid InterceptionId."})) {
		t.Fatal("cancelled navigation was treated as a bridge failure")
	}
	for _, err := range []error{errors.New("Invalid InterceptionId."), &cdproto.Error{Code: -32000, Message: "Invalid InterceptionId."}, &cdproto.Error{Code: -32602, Message: "Invalid response headers"}} {
		if retiredInterception(err) {
			t.Fatalf("genuine bridge failure was hidden: %v", err)
		}
	}
}

func shotsDir(t *testing.T) string {
	if d := os.Getenv("SIM_SHOTS"); d != "" {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		return d
	}
	return t.TempDir()
}

// The bridge port, delivered the way the Android shell delivers it: after
// the load event, one MessageChannel port posted to the main frame with
// the hello string. The sim keeps the other end and relays it to the Go
// host through a CDP binding.
const deliverPortJS = `(() => {
  const ch = new MessageChannel();
  window.__simPort = ch.port2;
  ch.port2.onmessage = (e) => window.__simNativePost(e.data);
  window.dispatchEvent(new MessageEvent('message', { data: 'gofastr-mobile-bridge', ports: [ch.port1] }));
})()`

// relay answers one bridge message, as the Android shell's port listener
// does: Bridge, then the reply back through the port, or for a native
// form submission, the answer shown as the next document.
func relay(ctx context.Context, h *mobilehost.Host, st *stats, payload string) {
	exec := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
	st.bridged.Add(1)
	out := h.Bridge(payload)
	var r mobilehost.BridgeReply
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		st.note("bridge reply: %v", err)
		return
	}
	st.mu.Lock()
	st.paths = append(st.paths, fmt.Sprintf("BRIDGE %s -> %d %s", r.URL, r.Status, r.Error))
	st.mu.Unlock()
	if r.Navigate && r.Error == "" {
		st.mu.Lock()
		st.shown[r.URL] = r
		st.mu.Unlock()
		if _, _, _, _, err := page.Navigate(r.URL).Do(exec); err != nil {
			st.note("show %s: %v", r.URL, err)
		}
		out = string(mustJSON(mobilehost.BridgeReply{ID: r.ID, Navigate: true}))
	}
	q, _ := json.Marshal(out)
	if _, _, err := cdpruntime.Evaluate("window.__simPort && window.__simPort.postMessage(" + string(q) + ")").Do(exec); err != nil {
		st.note("reply %s: %v", r.ID, err)
	}
}

// waitForPath polls the page URL until its path is want, riding over
// the errors a navigation in progress returns.
func waitForPath(want string, timeout time.Duration) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		deadline := time.Now().Add(timeout)
		var last string
		for time.Now().Before(deadline) {
			var loc string
			if err := chromedp.Location(&loc).Do(ctx); err == nil {
				last = loc
				if u, perr := url.Parse(loc); perr == nil && u.Path == want {
					return nil
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		return fmt.Errorf("page never reached %s (last %s)", want, last)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestPhoneWebView(t *testing.T) {
	// ios: request bodies reach the interceptor (WebKit keeps the text
	// ones). android: none do, so every body must come over the bridge.
	t.Run("ios", func(t *testing.T) { runPhone(t, false) })
	t.Run("android", func(t *testing.T) { runPhone(t, true) })
}

func runPhone(t *testing.T, android bool) {
	exe := phoneBrowser()
	if exe == "" {
		t.Skip("no Chromium found; set SIM_CHROME")
	}
	t.Cleanup(func() { owner.SetExtractor(nil) })

	platform := mobileui.IOS
	if android {
		platform = mobileui.Android
	}
	app, h, err := notesapp.BuildWithPlatform(t.TempDir(), origin, platform)
	if err != nil {
		t.Fatal(err)
	}
	// A native POST form target, the battery/auth login shape: read the
	// form (a file field included), answer 303 to a page that shows it.
	app.Router().Post("/sim/echo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseMultipartForm(1 << 20); err != nil && err != http.ErrNotMultipart {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		got := r.FormValue("title")
		if f, fh, err := r.FormFile("attachment"); err == nil {
			b := make([]byte, 64)
			n, _ := f.Read(b)
			got += "|" + fh.Filename + ":" + string(b[:n])
			f.Close()
		}
		http.Redirect(w, r, "/sim/echoed?got="+url.QueryEscape(got), http.StatusSeeOther)
	}))
	app.Router().Get("/sim/echoed", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "echoed:"+r.URL.Query().Get("got"))
	}))
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.Stop(ctx)
	}()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(exe), chromedp.NoSandbox, chromedp.Headless,
			// Belt and braces: even a request the interceptor missed has
			// nowhere to go.
			chromedp.ProxyServer("http://127.0.0.1:9"),
			// A loaded CI box can take longer than chromedp's default
			// start waits to bring Chrome up (internal/chromedptest's
			// launch-allowance gate; that helper is internal, so this
			// module spells the two allowances itself).
			chromedp.WSURLReadTimeout(60*time.Second),
		)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithBrowserOption(chromedp.WithDialTimeout(60*time.Second)))
	defer cancel()
	ctx, cancelT := context.WithTimeout(ctx, 90*time.Second)
	defer cancelT()

	st := &stats{shown: map[string]mobilehost.BridgeReply{}}
	chromedp.ListenTarget(ctx, func(ev any) {
		switch e := ev.(type) {
		case *fetch.EventRequestPaused:
			go serve(ctx, h, st, android, e)
		case *cdpruntime.EventBindingCalled:
			if e.Name == "__simNativePost" {
				go relay(ctx, h, st, e.Payload)
			}
		case *page.EventLoadEventFired:
			go func() {
				exec := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
				if _, _, err := cdpruntime.Evaluate(deliverPortJS).Do(exec); err != nil {
					st.note("deliver port: %v", err)
				}
			}()
		case *cdpruntime.EventExceptionThrown:
			st.note("page exception: %s", e.ExceptionDetails.Error())
		case *cdpruntime.EventConsoleAPICalled:
			if e.Type == cdpruntime.APITypeError {
				var parts []string
				for _, a := range e.Args {
					parts = append(parts, string(a.Value)+a.Description)
				}
				st.note("console.error: %s", strings.Join(parts, " "))
			}
		}
	})

	dir := shotsDir(t)
	mode := map[bool]string{false: "ios", true: "android"}[android]
	shot := func(name string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			var buf []byte
			if err := chromedp.CaptureScreenshot(&buf).Do(ctx); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(dir, mode+"-"+name+".png"), buf, 0o600)
		})
	}
	fill := func(selector, value string) chromedp.Action {
		return chromedp.Evaluate(fmt.Sprintf(`(() => {
		  const field = document.querySelector(%s);
		  field.value = %s;
		  field.dispatchEvent(new Event('input', { bubbles: true }));
		})()`, mustJSON(selector), mustJSON(value)), nil)
	}
	// A native POST form with a file: the shape WebKit drops the body of
	// and Android drops every body of.
	const nativeFormJS = `(() => {
	  const f = document.createElement('form');
	  f.method = 'post'; f.action = '/sim/echo'; f.enctype = 'multipart/form-data';
	  f.innerHTML = '<input name="title" value="from a native form"><input type="file" name="attachment">';
	  const dt = new DataTransfer();
	  dt.items.add(new File(['file bytes'], 'a.txt', { type: 'text/plain' }));
	  f.querySelector('input[type=file]').files = dt.files;
	  document.body.appendChild(f);
	  f.requestSubmit();
	})()`
	var marker, saveMarker, bodyText, path, echoed, offOrigin, renderedPlatform string
	var transport bool
	var lightBG, darkBG string
	var editorError, draftRestored, searchWorked, layoutFits, targetsFit bool
	err = chromedp.Run(ctx,
		cdpruntime.AddBinding("__simNativePost"),
		page.Enable(),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		emulation.SetDeviceMetricsOverride(390, 844, 3, true),
		emulation.SetTouchEmulationEnabled(true),
		emulation.SetUserAgentOverride(iPhoneUA),
		// Pin the light scheme: unset, headless Chrome follows the host's
		// appearance, and the "-light" shots came out dark on a dark Mac.
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "light"}}),
		chromedp.Navigate(origin+"/"),
		chromedp.WaitVisible(`a[href="/notes/create"]`, chromedp.ByQuery),
		chromedp.AttributeValue(`[data-gfm-platform]`, "data-gfm-platform", &renderedPlatform, nil, chromedp.ByQuery),
		chromedp.Poll(`typeof window.__gofastr_transport === "function" && !!window.__simPort`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`typeof window.__gofastr_transport === "function"`, &transport),
		// A request for another origin falls through to the page's own
		// fetch. Built from a Request with a body, it must still go out:
		// new Request(input) moves input's body, so a fallback that
		// re-sends input throws before any request is made.
		chromedp.Evaluate(offOriginProbeJS, &offOrigin, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}),
		shot("01-list-empty-light"),
		// Client-side navigation: a window marker set now must survive
		// the click, or the router fell back to a full load.
		chromedp.Evaluate(`window.__simMarker = "kept"`, nil),
		chromedp.Click(`a[href="/notes/create"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__simMarker || "lost"`, &marker),
		shot("02-create-light"),
		chromedp.SendKeys(`input[name="title"]`, "Groceries", chromedp.ByQuery),
		chromedp.SendKeys(`textarea[name="body"]`, "Milk, eggs, coffee", chromedp.ByQuery),
		chromedp.Click(`button[form="note-editor"]`, chromedp.ByQuery),
		chromedp.Poll(`!location.pathname.endsWith("/create") && document.body.innerText.includes("Groceries")`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Location(&path),
		chromedp.Evaluate(`window.__simMarker || "lost"`, &saveMarker),
		shot("03-after-save-light"),
		fill(`input[name="title"]`, "Groceries draft"),
		chromedp.Click(`a.gfm-back`, chromedp.ByQuery),
		chromedp.WaitVisible(`a.gfm-row`, chromedp.ByQuery),
		chromedp.Click(`a.gfm-row`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('input[name="title"]')?.value === 'Groceries draft'`, nil),
		chromedp.Evaluate(`document.querySelector('[data-gfm-status]').textContent.includes('draft restored')`, &draftRestored),
		fill(`input[name="title"]`, "   "),
		chromedp.Click(`button[form="note-editor"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-gfm-editor]')?.dataset.gfmError === 'true'`, nil),
		chromedp.Evaluate(`document.querySelector('[data-gfm-status]').textContent === 'Give your note a title.' && document.querySelector('textarea').value === 'Milk, eggs, coffee'`, &editorError),
		fill(`input[name="title"]`, "Groceries"),
		fill(`textarea[name="body"]`, "Milk, eggs, coffee\n\nPick up something fresh for the weekend."),
		chromedp.Click(`button[form="note-editor"]`, chromedp.ByQuery),
		chromedp.Poll(`document.querySelector('[data-gfm-editor]')?.dataset.gfmSaving !== 'true' && document.querySelector('[data-gfm-status]')?.textContent.startsWith('Saved on this device')`, nil),
		chromedp.Click(`a.gfm-back`, chromedp.ByQuery),
		waitForPath("/notes", 10*time.Second),
		chromedp.WaitVisible(`a.gfm-row`, chromedp.ByQuery),
		chromedp.Text(`body`, &bodyText, chromedp.ByQuery),
		fill(`input[name="q"]`, "zz-no-match"),
		chromedp.Click(`.gfm-search button`, chromedp.ByQuery),
		chromedp.Poll(`document.body.innerText.includes('No matching notes')`, nil),
		chromedp.Evaluate(`location.search.includes('zz-no-match') && !document.querySelector('.gfm-row')`, &searchWorked),
		chromedp.Click(`.gfm-empty a`, chromedp.ByQuery),
		chromedp.WaitVisible(`a.gfm-row`, chromedp.ByQuery),
		shot("04-list-light"),
		chromedp.Evaluate(`getComputedStyle(document.body).backgroundColor`, &lightBG),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "dark"}}),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`getComputedStyle(document.body).backgroundColor`, &darkBG),
		shot("05-list-dark"),
		chromedp.Click(`a.gfm-row`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		shot("07-editor-dark"),
		chromedp.Click(`a.gfm-back`, chromedp.ByQuery),
		chromedp.WaitVisible(`a.gfm-row`, chromedp.ByQuery),
		emulation.SetDeviceMetricsOverride(320, 844, 3, true),
		chromedp.Evaluate(`document.documentElement.style.fontSize = '200%'`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.documentElement.scrollWidth <= innerWidth + 1`, &layoutFits),
		chromedp.Evaluate(fmt.Sprintf(`Array.from(document.querySelectorAll('.gfm-nav-action, .gfm-search button, .gfm-fab, .gfm-row')).every(element => { const rect = element.getBoundingClientRect(); return rect.height >= %d && rect.width >= %d; })`, map[bool]int{false: 44, true: 48}[android], map[bool]int{false: 44, true: 48}[android]), &targetsFit),
		shot("08-list-large-text"),
		chromedp.Evaluate(`document.documentElement.style.fontSize = ''`, nil),
		emulation.SetDeviceMetricsOverride(390, 844, 3, true),
		chromedp.Click(`a[href="/notes/create"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`input[name="title"]`, chromedp.ByQuery),
		shot("06-create-dark"),
		chromedp.Poll(`!!window.__simPort`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(nativeFormJS, nil),
		// The form's answer replaces the document, so an Evaluate issued
		// mid-navigation fails with "target navigated"; poll the location
		// across that instead of through chromedp.Poll.
		waitForPath("/sim/echoed", 15*time.Second),
		chromedp.Text(`body`, &echoed, chromedp.ByQuery),
	)
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, p := range st.paths {
		t.Log(p)
	}
	for _, e := range st.errs {
		t.Log("NOTE:", e)
	}
	t.Logf("served in-process: %d, bridged: %d, sse aborted: %d, blocked off-origin: %d, after save at %s, shots in %s",
		st.served.Load(), st.bridged.Load(), st.sse.Load(), st.blocked.Load(), path, dir)
	if err != nil {
		t.Fatalf("phone flow: %v", err)
	}
	if lightBG == darkBG {
		t.Errorf("light and dark list backgrounds are both %q: the scheme emulation did not take", lightBG)
	}
	if !transport {
		t.Errorf("bridge.js did not install window.__gofastr_transport")
	}
	if marker != "kept" {
		t.Errorf("navigation to /notes/create was a full page load, not a client-side swap")
	}
	if saveMarker != "kept" {
		t.Error("saving replaced the document instead of using the existing router")
	}
	if renderedPlatform != string(platform) {
		t.Errorf("platform = %q, want %q regardless of the iPhone user agent", renderedPlatform, platform)
	}
	if !draftRestored || !editorError || !searchWorked || !layoutFits || !targetsFit {
		t.Errorf("mobile behavior: draft=%v validation=%v search=%v large-text-fit=%v touch-targets=%v", draftRestored, editorError, searchWorked, layoutFits, targetsFit)
	}
	if !strings.Contains(bodyText, "Groceries") {
		t.Errorf("the saved note is not in the list")
	}
	if want := "echoed:from a native form|a.txt:file bytes"; !strings.Contains(echoed, want) {
		t.Errorf("native form answer = %q, want %q", echoed, want)
	}
	if st.bridged.Load() == 0 {
		t.Errorf("no request went over the bridge")
	}
	// The probe counts as sent when the interceptor saw it or the CSP
	// refused it; either way the page's fetch took the request.
	probed := strings.HasPrefix(offOrigin, "csp:"+offOriginProbeURL)
	others := st.blocked.Load()
	for _, e := range st.errs {
		if strings.Contains(e, offOriginProbeURL) {
			probed = true
			others--
		}
	}
	if !probed {
		t.Errorf("the off-origin Request never reached the page's fetch; the transport answered %q", offOrigin)
	}
	if others != 0 {
		t.Errorf("the page tried to reach %d off-origin URLs besides the probe", others)
	}
}

// phoneBrowser uses a separate headless profile through chromedp. Never attach
// to or change a user's running browser profile.
func phoneBrowser() string {
	if executable := os.Getenv("SIM_CHROME"); executable != "" {
		return executable
	}
	candidates := []string{
		"/opt/pw-browsers/chromium-1243/chrome-linux64/chrome", "/usr/bin/google-chrome",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		filepath.Join(os.Getenv("PROGRAMFILES"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}
