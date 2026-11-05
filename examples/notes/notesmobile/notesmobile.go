// Package notesmobile is the surface the native shells bind to with
// `gomobile bind` (an .xcframework for iOS, an .aar for Android). Only
// types gomobile can export cross this boundary: strings, byte slices,
// ints, errors, and pointers to this package's structs. A scaffold would
// generate this file per app; everything app-specific is the one
// notesapp.BuildScreensWithPlatform call.
package notesmobile

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-native/examples/notes/notesapp"
	"github.com/DonaldMurillo/gofastr-native/mobilehost"
	"github.com/DonaldMurillo/gofastr-native/mobileui"
)

var (
	mu   sync.Mutex
	host *mobilehost.Host
)

// requestTimeout bounds one buffered request. A streamed one (Open) is
// bounded by its Close.
const requestTimeout = 60 * time.Second

// Start opens the app's data under dataDir and starts serving. origin is
// the scheme://host the WebView loads ("" for mobilehost.DefaultOrigin).
// Call it off the UI thread: migrations run here.
func Start(dataDir, origin string) error {
	mu.Lock()
	defer mu.Unlock()
	if host != nil {
		return errors.New("notesmobile: already started")
	}
	platform := mobileui.IOS
	if runtime.GOOS == "android" {
		platform = mobileui.Android
	}
	_, h, err := notesapp.BuildScreensWithPlatform(dataDir, origin, platform)
	if err != nil {
		return err
	}
	if err := h.Start(); err != nil {
		return err
	}
	host = h
	return nil
}

// Stop drains the app. Safe to call when not started.
func Stop() error {
	mu.Lock()
	h := host
	host = nil
	mu.Unlock()
	if h == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h.Stop(ctx)
}

// Response is a buffered answer. Headers are "Name: value\n" lines.
type Response struct {
	Status  int
	Headers string
	Body    []byte
}

// Handle serves one WebView request and buffers the answer.
func Handle(method, url, headers string, body []byte) (*Response, error) {
	h, err := current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	r, err := h.Do(ctx, &mobilehost.Request{Method: method, URL: url, Header: mobilehost.ParseHeaderLines(headers), Body: body})
	if err != nil {
		return nil, err
	}
	return &Response{Status: r.Status, Headers: mobilehost.FormatHeaderLines(r.Header), Body: r.Body}, nil
}

// Stream is a streamed answer (SSE, large bodies).
type Stream struct {
	Status  int
	Headers string
	s       *mobilehost.Stream
	cancel  context.CancelFunc
}

// Read returns up to max bytes, blocking until some arrive; the error is
// io.EOF (gomobile surfaces it as an exception) at the end.
func (s *Stream) Read(max int) ([]byte, error) { return s.s.Read(max) }

// Close ends the stream.
func (s *Stream) Close() error {
	s.cancel()
	return s.s.Close()
}

// Open serves one WebView request and returns once headers are in.
func Open(method, url, headers string, body []byte) (*Stream, error) {
	h, err := current()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s, err := h.Open(ctx, &mobilehost.Request{Method: method, URL: url, Header: mobilehost.ParseHeaderLines(headers), Body: body})
	if err != nil {
		cancel()
		return nil, err
	}
	return &Stream{Status: s.Status, Headers: mobilehost.FormatHeaderLines(s.Header), s: s, cancel: cancel}, nil
}

func current() (*mobilehost.Host, error) {
	mu.Lock()
	defer mu.Unlock()
	if host == nil {
		return nil, errors.New("notesmobile: not started")
	}
	return host, nil
}

// Bridge carries one message from the page's bridge.js (the runtime's
// host transport, and native POST forms) and returns the reply. Both are
// JSON strings, so the shells pass them through untouched: iOS from its
// "gofastr" WKScriptMessageHandlerWithReply, Android from the
// MessagePort it posted to the page. It never fails; a failure is the
// reply's error field. Call it off the UI thread.
func Bridge(msg string) string {
	h, err := current()
	if err != nil {
		// Echo the id: a MessagePort reply without one never settles.
		var req struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal([]byte(msg), &req)
		out, _ := json.Marshal(mobilehost.BridgeReply{ID: req.ID, Error: "not started"})
		return string(out)
	}
	return h.Bridge(msg)
}
