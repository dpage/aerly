package maps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotAllowed is returned when the URL (or a redirect hop) is not an https
// Google-controlled host on the allowlist: the SSRF guard.
var ErrNotAllowed = errors.New("maps: url not on the allowlist")

const (
	maxHops  = 8
	maxBody  = 1 << 20 // 1 MiB; we only need headers, but cap the read.
	httpWait = 10 * time.Second
)

// Resolver follows a Google short link to the full URL it points at and reads
// the coordinates out of it. It only ever issues requests to hosts on
// AllowedHosts and never auto-follows redirects, so a user-supplied URL cannot
// steer it to an internal address.
type Resolver struct {
	HTTP         *http.Client
	UserAgent    string
	AllowedHosts []string
}

// NewResolver builds a resolver with the production Google host allowlist and a
// client that does not auto-follow redirects (we follow manually, validating
// each hop).
func NewResolver() *Resolver {
	return &Resolver{
		HTTP: &http.Client{
			Timeout:       httpWait,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		UserAgent: "aerly/1.0 (+https://aerly.me)",
		// google.co.uk is listed explicitly (not a wildcard) alongside
		// google.com: this is an SSRF guard on a user-supplied URL, so a
		// loose google.<tld> pattern could admit an attacker-controlled
		// host. hostAllowed's suffix match still lets any *.google.co.uk
		// subdomain through, same as for google.com, and that's fine since
		// those subdomains are Google-controlled.
		AllowedHosts: []string{"google.com", "google.co.uk", "goo.gl", "g.co"},
	}
}

func (r *Resolver) hostAllowed(host string) bool {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	for _, a := range r.AllowedHosts {
		// Strip port from the allowlist entry too, so test entries of the form
		// "127.0.0.1:PORT" added by newTestResolver match correctly.
		entry := strings.ToLower(a)
		if i := strings.IndexByte(entry, ':'); i >= 0 {
			entry = entry[:i]
		}
		if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}

// ResolveURL returns the coordinates the URL ultimately points at. It validates
// the scheme and host on every hop, tries to read coordinates directly from
// each URL, and otherwise follows a single redirect at a time (without fetching
// non-allowlisted hosts). ok=false (err=nil) means no coordinates could be
// found; ErrNotAllowed means the URL/redirect was off the allowlist.
func (r *Resolver) ResolveURL(ctx context.Context, rawURL string) (lat, lon float64, ok bool, err error) {
	lat, lon, ok, _, err = r.hopToCoordsOrTerminal(ctx, rawURL)
	return lat, lon, ok, err
}

// ResolveURLOrHint is ResolveURL's hint-aware sibling: it runs the identical
// allowlisted hop loop (coordinates are always tried first, on every hop) and,
// only once the chain ends without a coordinate match, extracts a human
// hint from the terminal URL (see ExtractHint's calling contract — this is
// exactly the "only once ExtractLatLon has returned false" case it requires).
// A URL that carries an exact pin therefore never falls through to a hint,
// and, like ResolveURL, the response body is drained and discarded rather than
// scraped: hint or not, we only ever hand the user something to confirm, never
// a guess we plot ourselves.
func (r *Resolver) ResolveURLOrHint(ctx context.Context, rawURL string) (lat, lon float64, ok bool, hint string, err error) {
	lat, lon, ok, terminalURL, err := r.hopToCoordsOrTerminal(ctx, rawURL)
	if err != nil || ok || terminalURL == "" {
		return lat, lon, ok, "", err
	}
	if h, found := ExtractHint(terminalURL); found {
		hint = h
	}
	return 0, 0, false, hint, nil
}

// hopToCoordsOrTerminal is the shared allowlisted hop loop behind ResolveURL
// and ResolveURLOrHint. It validates the scheme and host on every hop, tries
// coordinates on each URL, and otherwise follows a single redirect at a time.
// When the chain ends without a coordinate match, terminalURL carries the last
// URL visited (so a caller wanting a hint knows exactly which URL to read it
// from); it is "" when the loop instead errored out (bad scheme/host, a
// request/parse failure, or too many redirects).
func (r *Resolver) hopToCoordsOrTerminal(ctx context.Context, rawURL string) (lat, lon float64, ok bool, terminalURL string, err error) {
	cur := rawURL
	for hop := 0; hop < maxHops; hop++ {
		u, perr := url.Parse(cur)
		if perr != nil {
			return 0, 0, false, "", fmt.Errorf("maps: parse url: %w", perr)
		}
		if u.Scheme != "https" || !r.hostAllowed(u.Host) {
			return 0, 0, false, "", ErrNotAllowed
		}
		if la, lo, found := ExtractLatLon(cur); found {
			return la, lo, true, "", nil
		}
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, cur, nil)
		if rerr != nil {
			return 0, 0, false, "", rerr
		}
		req.Header.Set("User-Agent", r.UserAgent)
		resp, derr := r.HTTP.Do(req)
		if derr != nil {
			return 0, 0, false, "", derr
		}
		// We only need the redirect headers; a Google Maps place page never
		// carries the pin's coordinates for a key-less, session-less request
		// (it just re-centres on the caller's IP), so reading the body would
		// only invite a wrong guess. Drain and discard it.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		_ = resp.Body.Close()
		if resp.StatusCode < 300 || resp.StatusCode >= 400 {
			return 0, 0, false, cur, nil
		}
		loc := resp.Header.Get("Location")
		if loc == "" {
			return 0, 0, false, cur, nil
		}
		next, nerr := u.Parse(loc)
		if nerr != nil {
			return 0, 0, false, "", nerr
		}
		cur = next.String()
	}
	return 0, 0, false, "", fmt.Errorf("maps: too many redirects")
}
