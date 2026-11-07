// Package browsersim shows an in-process mobilehost app in a Chromium window
// set up like a phone WebView. Every request is paused through the DevTools
// Fetch domain and answered by mobilehost.Host.Do, and the Android bridge is
// relayed through a CDP binding. No HTTP listener or socket is opened for the
// app, and a proxy pointed at a closed port catches anything the interceptor
// misses.
//
// It is a development preview, not WebKit or Android WebView. Streamed SSE,
// native interception limits and platform rendering differ. The browser runs
// with a fresh temporary profile and never attaches to a running browser.
package browsersim

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
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
)

// ErrNoBrowser reports that no Chrome, Chromium or Edge executable was found.
var ErrNoBrowser = errors.New("browsersim: no Chromium found; set SIM_CHROME or Options.Chrome")

const (
	iPhoneUA  = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
	androidUA = "Mozilla/5.0 (Linux; Android 15; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36"
)

// Options configures one browser session.
type Options struct {
	// Platform selects the phone shape and transport quirks. On Android the
	// interceptor drops request bodies, as WebResourceRequest does, so bodies
	// must arrive over the bridge.
	Platform mobileui.Platform
	// Headless hides the window. Tests set it; gallery-sim leaves it off.
	Headless bool
	// Chrome overrides browser discovery. SIM_CHROME is checked next.
	Chrome string
	// Dark emulates prefers-color-scheme: dark.
	Dark bool
	// Origin is the host's origin; mobilehost.DefaultOrigin when empty.
	Origin string
	// Timeout bounds the whole session; zero means no limit.
	Timeout time.Duration
}

// Session is one running browser.
type Session struct {
	ctx     context.Context
	cancel  []context.CancelFunc
	host    *mobilehost.Host
	android bool
	origin  string

	mu     sync.Mutex
	errs   []string
	paths  []string
	shown  map[string]mobilehost.BridgeReply
	closed bool
}

// Browser returns the executable Open would use, or "" when none is found.
func Browser(override string) string {
	if override != "" {
		return override
	}
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

// Open starts a browser whose every request is served by host. The host
// must already be started. Call Close when done; ending ctx also stops it.
func Open(ctx context.Context, host *mobilehost.Host, options Options) (*Session, error) {
	if host == nil {
		return nil, errors.New("browsersim: host is required")
	}
	if err := options.Platform.Validate(); err != nil {
		return nil, err
	}
	executable := Browser(options.Chrome)
	if executable == "" {
		return nil, ErrNoBrowser
	}
	if options.Origin == "" {
		options.Origin = mobilehost.DefaultOrigin
	}
	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(executable), chromedp.NoSandbox,
		chromedp.ProxyServer("http://127.0.0.1:9"), chromedp.WSURLReadTimeout(60*time.Second),
		chromedp.WindowSize(430, 932))
	if !options.Headless {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("headless", false), chromedp.Flag("hide-scrollbars", false))
	}
	session := &Session{host: host, android: options.Platform == mobileui.Android, origin: options.Origin, shown: map[string]mobilehost.BridgeReply{}}
	allocator, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	browser, cancelBrowser := chromedp.NewContext(allocator, chromedp.WithBrowserOption(chromedp.WithDialTimeout(60*time.Second)))
	session.cancel = []context.CancelFunc{cancelBrowser, cancelAllocator}
	session.ctx = browser
	if options.Timeout > 0 {
		bounded, cancel := context.WithTimeout(browser, options.Timeout)
		session.ctx = bounded
		session.cancel = append([]context.CancelFunc{cancel}, session.cancel...)
	}
	chromedp.ListenTarget(session.ctx, session.listen)
	agent := iPhoneUA
	if session.android {
		agent = androidUA
	}
	scheme := "light"
	if options.Dark {
		scheme = "dark"
	}
	if err := chromedp.Run(session.ctx, cdpruntime.AddBinding("__simNativePost"), page.Enable(),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		emulation.SetDeviceMetricsOverride(390, 844, 3, true), emulation.SetTouchEmulationEnabled(true),
		emulation.SetUserAgentOverride(agent),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}})); err != nil {
		session.Close()
		return nil, err
	}
	return session, nil
}

// Context is the chromedp context for chromedp.Run.
func (session *Session) Context() context.Context { return session.ctx }

// Navigate loads path on the host origin.
func (session *Session) Navigate(path string) error {
	return chromedp.Run(session.ctx, chromedp.Navigate(session.origin+path))
}

// Screenshot writes a PNG of the visible viewport.
func (session *Session) Screenshot(path string) error {
	var image []byte
	if err := chromedp.Run(session.ctx, chromedp.CaptureScreenshot(&image)); err != nil {
		return err
	}
	return os.WriteFile(path, image, 0o600)
}

// Wait blocks until the browser exits, the window closes or ctx ends.
func (session *Session) Wait() { <-session.ctx.Done() }

// Close stops the browser and removes its temporary profile. It is safe to
// call more than once.
func (session *Session) Close() {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return
	}
	session.closed = true
	session.mu.Unlock()
	if session.ctx.Err() == nil {
		_ = chromedp.Cancel(session.ctx)
	}
	for _, cancel := range session.cancel {
		cancel()
	}
}

// Errors returns page exceptions, console errors and transport failures.
func (session *Session) Errors() []string {
	session.mu.Lock()
	defer session.mu.Unlock()
	return append([]string(nil), session.errs...)
}

// Requests returns one "METHOD /path -> status" line per answered request.
func (session *Session) Requests() []string {
	session.mu.Lock()
	defer session.mu.Unlock()
	return append([]string(nil), session.paths...)
}

func (session *Session) note(format string, values ...any) {
	session.mu.Lock()
	session.errs = append(session.errs, fmt.Sprintf(format, values...))
	session.mu.Unlock()
}

func (session *Session) path(format string, values ...any) {
	session.mu.Lock()
	session.paths = append(session.paths, fmt.Sprintf(format, values...))
	session.mu.Unlock()
}

func (session *Session) executor() context.Context {
	return cdp.WithExecutor(session.ctx, chromedp.FromContext(session.ctx).Target)
}

func (session *Session) listen(event any) {
	switch event := event.(type) {
	case *fetch.EventRequestPaused:
		go session.serve(event)
	case *cdpruntime.EventBindingCalled:
		if event.Name == "__simNativePost" {
			go session.relay(event.Payload)
		}
	case *page.EventLoadEventFired:
		go func() {
			if _, _, err := cdpruntime.Evaluate(deliverPortJS).Do(session.executor()); err != nil && session.ctx.Err() == nil {
				session.note("bridge port: %v", err)
			}
		}()
	case *page.EventJavascriptDialogOpening:
		go func() {
			if err := page.HandleJavaScriptDialog(true).Do(session.executor()); err != nil && session.ctx.Err() == nil {
				session.note("dialog: %v", err)
			}
		}()
	case *cdpruntime.EventExceptionThrown:
		session.note("page exception: %s", event.ExceptionDetails.Error())
	case *cdpruntime.EventConsoleAPICalled:
		if event.Type == cdpruntime.APITypeError {
			var messages []string
			for _, argument := range event.Args {
				messages = append(messages, string(argument.Value)+argument.Description)
			}
			session.note("console.error: %s", strings.Join(messages, " "))
		}
	}
}

// serve answers one paused request from the in-process host.
func (session *Session) serve(event *fetch.EventRequestPaused) {
	executor := session.executor()
	if event.Request.Method == http.MethodGet {
		session.mu.Lock()
		shown, ok := session.shown[event.Request.URL]
		delete(session.shown, event.Request.URL)
		session.mu.Unlock()
		if ok {
			body, _ := base64.StdEncoding.DecodeString(shown.Body)
			var entries []*fetch.HeaderEntry
			for _, pair := range shown.Headers {
				entries = append(entries, &fetch.HeaderEntry{Name: pair[0], Value: pair[1]})
			}
			session.path("SHOW %s -> %d", event.Request.URL, shown.Status)
			_ = fetch.FulfillRequest(event.RequestID, int64(shown.Status)).WithResponseHeaders(entries).WithBody(base64.StdEncoding.EncodeToString(body)).Do(executor)
			return
		}
	}
	target, err := url.Parse(event.Request.URL + event.Request.URLFragment)
	if err != nil || !strings.HasPrefix(event.Request.URL, session.origin+"/") && event.Request.URL != session.origin {
		session.note("blocked off-origin request %s", event.Request.URL)
		_ = fetch.FailRequest(event.RequestID, network.ErrorReasonBlockedByClient).Do(executor)
		return
	}
	if target.Path == "/__gofastr/sse" {
		// CDP fulfills a complete body; a held stream cannot be shown here.
		_ = fetch.FailRequest(event.RequestID, network.ErrorReasonAborted).Do(executor)
		return
	}
	header := http.Header{}
	for key, value := range event.Request.Headers {
		header.Set(key, fmt.Sprint(value))
	}
	var body []byte
	for _, entry := range event.Request.PostDataEntries {
		decoded, err := base64.StdEncoding.DecodeString(entry.Bytes)
		if err != nil {
			session.note("bad post data on %s: %v", target.Path, err)
		}
		body = append(body, decoded...)
	}
	if session.android {
		body = nil
	}
	requestCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	response, err := session.host.Do(requestCtx, &mobilehost.Request{Method: event.Request.Method, URL: event.Request.URL, Header: header, Body: body})
	if err != nil {
		session.note("host error on %s %s: %v", event.Request.Method, target.Path, err)
		_ = fetch.FailRequest(event.RequestID, network.ErrorReasonFailed).Do(executor)
		return
	}
	session.path("%s %s -> %d", event.Request.Method, target.Path, response.Status)
	var entries []*fetch.HeaderEntry
	for key, values := range response.Header {
		for _, value := range values {
			entries = append(entries, &fetch.HeaderEntry{Name: key, Value: value})
		}
	}
	if err := fetch.FulfillRequest(event.RequestID, int64(response.Status)).WithResponseHeaders(entries).
		WithBody(base64.StdEncoding.EncodeToString(response.Body)).Do(executor); err != nil && !retiredInterception(err) && session.ctx.Err() == nil {
		session.note("fulfill %s: %v", target.Path, err)
	}
}

// retiredInterception reports a paused request that navigation already aborted.
func retiredInterception(err error) bool {
	var protocol *cdproto.Error
	return errors.As(err, &protocol) && protocol.Code == -32602 && protocol.Message == "Invalid InterceptionId."
}

// deliverPortJS hands the page one MessageChannel port after load, as the
// Android shell does, and relays its messages through the CDP binding.
const deliverPortJS = `(() => {
  const ch = new MessageChannel();
  window.__simPort = ch.port2;
  ch.port2.onmessage = (e) => window.__simNativePost(e.data);
  window.dispatchEvent(new MessageEvent('message', { data: 'gofastr-mobile-bridge', ports: [ch.port1] }));
})()`

// relay answers one bridge message the way the Android port listener does.
func (session *Session) relay(payload string) {
	executor := session.executor()
	out := session.host.Bridge(payload)
	var reply mobilehost.BridgeReply
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		session.note("bridge reply: %v", err)
		return
	}
	session.path("BRIDGE %s -> %d", reply.URL, reply.Status)
	if reply.Navigate && reply.Error == "" {
		session.mu.Lock()
		session.shown[reply.URL] = reply
		session.mu.Unlock()
		if _, _, _, _, err := page.Navigate(reply.URL).Do(executor); err != nil {
			session.note("show %s: %v", reply.URL, err)
		}
		encoded, _ := json.Marshal(mobilehost.BridgeReply{ID: reply.ID, Navigate: true})
		out = string(encoded)
	}
	quoted, _ := json.Marshal(out)
	if _, _, err := cdpruntime.Evaluate("window.__simPort && window.__simPort.postMessage(" + string(quoted) + ")").Do(executor); err != nil && session.ctx.Err() == nil {
		session.note("reply %s: %v", reply.ID, err)
	}
}
