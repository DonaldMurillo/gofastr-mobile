package mobilehost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// maxRedirects is the Fetch Standard's redirect limit.
const maxRedirects = 20

// FetchResponse is a buffered answer to a page fetch, after redirects.
type FetchResponse struct {
	Response
	// URL is the final URL: the request's own, or the last redirect's
	// target.
	URL string
	// Redirected reports whether a redirect was followed. The runtime
	// reads it (a followed 3xx is another URL's answer, never the
	// request's own), so it must be exactly what a browser's fetch
	// would report.
	Redirected bool
}

// ErrCrossOriginRedirect is returned when a redirect leaves the app's
// origin. A page fetch could not read that answer in a browser either
// (no CORS), and the in-process host cannot reach another origin.
var ErrCrossOriginRedirect = errors.New("mobilehost: redirect leaves the app origin")

// Fetch serves a request the page made with fetch() through the
// runtime's host transport (window.__gofastr_transport). Unlike Do it
// follows redirects the way fetch's redirect: "follow" does, because
// neither platform's interception API can hand a 3xx back to the page:
// 301/302 turn a POST into a GET, 303 turns anything but HEAD into a GET,
// 307/308 keep the method and body.
func (h *Host) Fetch(ctx context.Context, req *Request) (*FetchResponse, error) {
	if req == nil {
		return nil, errors.New("mobilehost: nil request")
	}
	cur := *req
	cur.Header = req.Header.Clone()
	redirected := false
	for hop := 0; ; hop++ {
		resp, err := h.Do(ctx, &cur)
		if err != nil {
			return nil, err
		}
		loc := resp.Header.Get("Location")
		if !isRedirect(resp.Status) || loc == "" {
			return &FetchResponse{Response: *resp, URL: cur.URL, Redirected: redirected}, nil
		}
		if hop >= maxRedirects {
			return nil, fmt.Errorf("mobilehost: more than %d redirects", maxRedirects)
		}
		base, err := url.Parse(cur.URL)
		if err != nil {
			return nil, fmt.Errorf("mobilehost: bad URL: %w", err)
		}
		next, err := base.Parse(loc)
		if err != nil {
			return nil, fmt.Errorf("mobilehost: bad redirect Location: %w", err)
		}
		if !strings.EqualFold(next.Scheme, h.origin.Scheme) || !strings.EqualFold(next.Host, h.origin.Host) {
			return nil, fmt.Errorf("%w: %s", ErrCrossOriginRedirect, next.Redacted())
		}
		next.Fragment = ""
		if rewritesToGET(resp.Status, cur.Method) {
			cur.Method = http.MethodGet
			cur.Body = nil
			for _, k := range []string{"Content-Type", "Content-Length", "Content-Encoding", "Content-Language", "Content-Location"} {
				cur.Header.Del(k)
			}
		}
		cur.URL = next.String()
		redirected = true
	}
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// rewritesToGET is the Fetch Standard's HTTP-redirect fetch step 12.
func rewritesToGET(status int, method string) bool {
	m := strings.ToUpper(method)
	if m == "" {
		m = http.MethodGet
	}
	switch status {
	case http.StatusMovedPermanently, http.StatusFound:
		return m == http.MethodPost
	case http.StatusSeeOther:
		return m != http.MethodGet && m != http.MethodHead
	}
	return false
}
