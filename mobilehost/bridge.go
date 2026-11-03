package mobilehost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// The bridge protocol. The page's bridge.js hands every same-origin
// fetch (and every native POST form submission) to the native shell as
// one JSON string; the shell passes it to Bridge unchanged and hands the
// returned JSON string back. Neither platform's interception API carries
// what this channel does: Android's WebResourceRequest has no body,
// WKURLSchemeHandler drops Blob and File bodies, and neither can return
// a redirect. Keeping the protocol in Go keeps the Swift and Java shells
// to string plumbing.

// maxBridgeMessage caps one inbound message: the largest body the app
// accepts (uihost's mutating-body cap is well under this) plus base64
// and envelope overhead.
const maxBridgeMessage = 96 << 20

// bridgeTimeout bounds one bridged request.
const bridgeTimeout = 60 * time.Second

// BridgeRequest is one message from bridge.js.
type BridgeRequest struct {
	ID      string      `json:"id,omitempty"`
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers [][2]string `json:"headers"`
	// Body is base64 (standard alphabet); empty for no body.
	Body string `json:"body,omitempty"`
	// Navigate marks a native form submission: the shell shows the
	// answer as the page's new document instead of returning it to JS.
	Navigate bool `json:"navigate,omitempty"`
}

// BridgeReply is Bridge's answer.
type BridgeReply struct {
	ID         string      `json:"id,omitempty"`
	Status     int         `json:"status,omitempty"`
	Headers    [][2]string `json:"headers,omitempty"`
	Body       string      `json:"body,omitempty"`
	URL        string      `json:"url,omitempty"`
	Redirected bool        `json:"redirected,omitempty"`
	Navigate   bool        `json:"navigate,omitempty"`
	// Error is set instead of the fields above when the request could
	// not be served; bridge.js rejects the fetch with a TypeError, as a
	// browser does on a network error.
	Error string `json:"error,omitempty"`
}

// Bridge serves one bridge.js message and returns the reply JSON. It
// never fails: a malformed message or a failed request comes back as a
// reply carrying Error, so the page's pending fetch always settles.
func (h *Host) Bridge(msg string) string {
	ctx, cancel := context.WithTimeout(context.Background(), bridgeTimeout)
	defer cancel()
	return string(mustJSON(h.bridge(ctx, msg)))
}

func (h *Host) bridge(ctx context.Context, msg string) BridgeReply {
	if len(msg) > maxBridgeMessage {
		return BridgeReply{Error: "message too large"}
	}
	var in BridgeRequest
	if err := json.Unmarshal([]byte(msg), &in); err != nil {
		return BridgeReply{Error: "malformed bridge message"}
	}
	fail := func(s string) BridgeReply { return BridgeReply{ID: in.ID, Navigate: in.Navigate, Error: s} }
	body, err := base64.StdEncoding.DecodeString(in.Body)
	if err != nil {
		return fail("malformed body")
	}
	hdr := http.Header{}
	for _, kv := range in.Headers {
		if kv[0] == "" || hasCtl(kv[0]) || hasCtl(kv[1]) {
			continue
		}
		hdr.Add(kv[0], kv[1])
	}
	resp, err := h.Fetch(ctx, &Request{Method: strings.ToUpper(in.Method), URL: in.URL, Header: hdr, Body: body})
	if err != nil {
		return fail(err.Error())
	}
	out := BridgeReply{
		ID:         in.ID,
		Status:     resp.Status,
		URL:        resp.URL,
		Redirected: resp.Redirected,
		Navigate:   in.Navigate,
		Body:       base64.StdEncoding.EncodeToString(resp.Body),
	}
	for _, k := range sortedKeys(resp.Header) {
		for _, v := range resp.Header[k] {
			out.Headers = append(out.Headers, [2]string{k, v})
		}
	}
	return out
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// BridgeReply holds only strings, ints and bools.
		return []byte(`{"error":"encode reply"}`)
	}
	return b
}
