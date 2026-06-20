// Package feeds fetches and parses trip-scoped iCalendar feed subscriptions
// (e.g. a conference's published schedule) and keeps a trip's cached events in
// sync. It owns the network half — a polite, SSRF-guarded HTTP client with
// conditional GETs — and reuses internal/importics for the RFC 5545 parse.
package feeds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/importics"
	"github.com/dpage/aerly/internal/store"
)

// maxFeedBytes caps how much of a feed we read, so a hostile or runaway URL
// can't exhaust memory. Conference schedules are comfortably under this.
const maxFeedBytes = 8 << 20 // 8 MiB

// ErrNotModified is returned by Fetch when the server answers a conditional GET
// with 304 Not Modified — the cached events are still current.
var ErrNotModified = errors.New("feeds: not modified")

// ErrNotICalendar is returned when a fetched feed isn't an iCalendar document
// (e.g. an HTML error page, or a frab/Pentabarf XML schedule). Its message is
// surfaced verbatim as the feed's last_error in the UI, so it's phrased for a
// human and carries no "feeds:" prefix.
var ErrNotICalendar = errors.New("not an iCalendar feed — the URL must return an .ics calendar (BEGIN:VCALENDAR)")

// Fetcher performs SSRF-guarded conditional GETs of external iCal feeds.
type Fetcher struct {
	HTTP      *http.Client
	UserAgent string
	// AllowPrivate disables the private/loopback address checks. Test-only —
	// production always leaves it false so user URLs can't reach internal hosts.
	AllowPrivate bool
}

// NewFetcher builds a Fetcher whose dialer refuses to connect to private,
// loopback or link-local addresses (checked after DNS resolution, so it also
// defeats DNS-rebinding). userAgent identifies us to feed publishers.
func NewFetcher(userAgent string) *Fetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if err := guardAddr(addr); err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	return &Fetcher{
		HTTP: &http.Client{
			Timeout:   20 * time.Second,
			Transport: transport,
			// Re-validate the destination on every redirect hop, and cap the
			// chain so a feed can't bounce us around indefinitely.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("too many redirects")
				}
				return validateURL(req.URL, false)
			},
		},
		UserAgent: userAgent,
	}
}

// Result is the outcome of a successful (200) fetch + parse.
type Result struct {
	Events       []store.TripFeedEvent
	ETag         string
	LastModified string
	// CalName is the feed's own X-WR-CALNAME, used as a fallback display name.
	CalName string
}

// Fetch retrieves and parses a feed, sending the cached validators as a
// conditional GET. It returns ErrNotModified on a 304. The returned events are
// not yet associated with a feed id — the caller fills FeedID on store.
func (f *Fetcher) Fetch(ctx context.Context, rawURL, etag, lastModified string) (*Result, error) {
	u, err := parseFeedURL(rawURL, f.AllowPrivate)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", "text/calendar, text/plain;q=0.9, */*;q=0.5")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, ErrNotModified
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feeds: unexpected status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes))
	if err != nil {
		return nil, err
	}
	// Reject anything that isn't an iCalendar document. importics.Parse is
	// lenient and would happily return zero events from, say, an HTML error page
	// or a frab/Pentabarf XML schedule — leaving the user with a silently empty
	// feed. Sniffing for the VCALENDAR sentinel turns that into a clear error.
	if !looksLikeICalendar(body) {
		return nil, ErrNotICalendar
	}

	cal, err := importics.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	return &Result{
		Events:       mapEvents(cal.Events),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		CalName:      strings.TrimSpace(cal.Name),
	}, nil
}

// looksLikeICalendar reports whether body is an iCalendar stream, identified by
// the mandatory "BEGIN:VCALENDAR" line (RFC 5545 §3.4). The check is
// case-insensitive and tolerates a leading BOM / whitespace.
func looksLikeICalendar(body []byte) bool {
	return bytes.Contains(bytes.ToUpper(body), []byte("BEGIN:VCALENDAR"))
}

// mapEvents projects parsed VEVENTs into cached store events, dropping any with
// no usable start instant (a feed we couldn't date can't be placed on a
// timeline).
func mapEvents(in []importics.Event) []store.TripFeedEvent {
	out := make([]store.TripFeedEvent, 0, len(in))
	for _, e := range in {
		if e.Start.Time.IsZero() {
			continue
		}
		ev := store.TripFeedEvent{
			UID:         e.UID,
			Summary:     e.Summary,
			Description: e.Description,
			Location:    e.Location,
			StartsAt:    e.Start.Time.UTC(),
			StartTZ:     e.Start.TZID,
			AllDay:      !e.Start.HasTime,
		}
		if !e.End.Time.IsZero() {
			end := e.End.Time.UTC()
			ev.EndsAt = &end
		}
		out = append(out, ev)
	}
	return out
}

// parseFeedURL parses rawURL, upgrades the webcal scheme to https, and runs the
// scheme/host validation (rejecting private literal-IP hosts unless
// allowPrivate). It's the single entry point both Fetch and the add/edit
// handlers use, so an unsafe URL is rejected before it's ever stored.
func parseFeedURL(rawURL string, allowPrivate bool) (*url.URL, error) {
	raw := strings.TrimSpace(rawURL)
	// webcal:// is just an http(s) iCal feed by convention; calendar clients
	// rewrite it to https. Do the same so subscribers can paste either form.
	if strings.HasPrefix(strings.ToLower(raw), "webcal://") {
		raw = "https://" + raw[len("webcal://"):]
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("feeds: invalid URL")
	}
	if err := validateURL(u, allowPrivate); err != nil {
		return nil, err
	}
	return u, nil
}

// NormalizeURL validates and canonicalises a user-supplied feed URL for
// storage, returning the cleaned string. Exposed for the add/edit handlers;
// always enforces the full SSRF rules.
func NormalizeURL(rawURL string) (string, error) {
	u, err := parseFeedURL(rawURL, false)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// validateURL rejects anything but an http(s) URL to a public host. The
// hostname is checked here; literal-IP hosts are blocked outright (unless
// allowPrivate), and named hosts are re-checked against their resolved address
// at dial time (guardAddr).
func validateURL(u *url.URL, allowPrivate bool) error {
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("feeds: URL must be http(s)")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("feeds: URL has no host")
	}
	if allowPrivate {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicIP(ip) {
		return fmt.Errorf("feeds: URL host is not allowed")
	}
	return nil
}

// guardAddr blocks a dialled "host:port" whose resolved IPs include any
// non-public address. Run from the transport's DialContext, it catches both
// hostnames that resolve to private space and DNS-rebinding between the
// validate and the connect.
func guardAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return fmt.Errorf("feeds: address %s is not allowed", host)
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return fmt.Errorf("feeds: address %s is not allowed", host)
		}
	}
	return nil
}

// isPublicIP reports whether ip is a globally-routable unicast address — i.e.
// not loopback, private, link-local, multicast, or unspecified.
func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// Block the IPv4 shared/CGNAT range 100.64.0.0/10, which IsPrivate misses.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}
