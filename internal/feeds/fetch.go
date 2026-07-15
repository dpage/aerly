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
// loopback or link-local addresses. The dialer resolves each hostname once and
// connects to the vetted IP directly (see guardedDial), so a DNS-rebinding
// attacker can't slip a private address in between the check and the connect.
// userAgent identifies us to feed publishers.
func NewFetcher(userAgent string) *Fetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	f := &Fetcher{UserAgent: userAgent}
	f.HTTP = &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return f.guardedDial(ctx, dialer, network, addr)
			},
		},
		// Re-validate the destination on every redirect hop, and cap the
		// chain so a feed can't bounce us around indefinitely.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return validateURL(req.URL, false)
		},
	}
	return f
}

// guardedDial resolves addr's host at most once and dials a VALIDATED public IP
// directly. Dialing the resolved IP — rather than handing the hostname back to
// the dialer for a second, unchecked resolution — is what actually closes the
// DNS-rebinding TOCTOU: the address we vet is exactly the address we connect to.
// AllowPrivate (test-only) skips the public-IP checks so tests can reach a
// loopback httptest server through the real guarded transport.
func (f *Fetcher) guardedDial(ctx context.Context, dialer *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	// A literal IP has nothing to resolve: validate it and dial as-is.
	if net.ParseIP(host) != nil {
		if !f.AllowPrivate {
			if err := guardAddr(addr); err != nil {
				return nil, err
			}
		}
		return dialer.DialContext(ctx, network, addr)
	}
	// A hostname: resolve once, then try each answer — but only after vetting it
	// and only by dialing that exact IP, never the hostname.
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ipa := range ips {
		ipPort := net.JoinHostPort(ipa.IP.String(), port)
		if !f.AllowPrivate {
			if err := guardAddr(ipPort); err != nil {
				lastErr = err
				continue
			}
		}
		conn, derr := dialer.DialContext(ctx, network, ipPort)
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("feeds: no usable address for %s", host)
	}
	return nil, lastErr
}

// Result is the outcome of a successful (200) fetch + parse.
type Result struct {
	Events       []store.TripFeedEvent
	ETag         string
	LastModified string
	// CalName is the feed's own X-WR-CALNAME, used as a fallback display name.
	CalName string
}

// Fetch retrieves and parses a feed. fallbackTZ is an optional IANA zone used
// as the display zone for events that carry no zone of their own and whose
// calendar declares no X-WR-TIMEZONE — set by the user for feeds that omit zone
// information. It sends the cached validators as a conditional GET and returns
// ErrNotModified on a 304.
func (f *Fetcher) Fetch(ctx context.Context, rawURL, etag, lastModified, fallbackTZ string) (*Result, error) {
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
		Events:       mapEvents(cal.Events, strings.TrimSpace(cal.Timezone), fallbackTZ),
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
// timeline). The display zone for each event is the first of: its own TZID, the
// calendar's X-WR-TIMEZONE (calTZ), or the user-set fallback (fallbackTZ). That
// zone is also used to anchor floating wall-clock times to a real instant.
func mapEvents(in []importics.Event, calTZ, fallbackTZ string) []store.TripFeedEvent {
	out := make([]store.TripFeedEvent, 0, len(in))
	for _, e := range in {
		// Display zone: the event's own TZID, else the calendar default, else the
		// user-set fallback. For a UTC-stamped feed with no zone info this is what
		// turns "all times in UTC" into the event's real local time.
		zone := e.Start.TZID
		if zone == "" {
			zone = calTZ
		}
		if zone == "" {
			zone = fallbackTZ
		}
		start, ok := resolveInstant(e.Start, zone)
		if !ok {
			continue
		}
		ev := store.TripFeedEvent{
			UID:         e.UID,
			Summary:     e.Summary,
			Description: e.Description,
			Location:    e.Location,
			StartsAt:    start,
			AllDay:      !e.Start.HasTime,
		}
		// Date-only events have no time of day, so no zone applies.
		if !ev.AllDay {
			ev.StartTZ = zone
		}
		if end, ok := resolveInstant(e.End, zone); ok {
			ev.EndsAt = &end
		}
		out = append(out, ev)
	}
	return out
}

// resolveInstant turns a parsed DTSTART/DTEND into a UTC instant. A UTC- or
// TZID-anchored value already is an absolute instant. A *floating* value is a
// wall-clock with no zone; when the calendar supplies a default zone we
// reinterpret the wall-clock in it (so a 09:00 talk in America/Vancouver lands
// at the right instant, not at 09:00 UTC). ok is false for an undated value.
func resolveInstant(dt importics.DateTime, zone string) (time.Time, bool) {
	if dt.Time.IsZero() {
		return time.Time{}, false
	}
	if dt.Floating && zone != "" {
		if loc, err := time.LoadLocation(zone); err == nil {
			t := dt.Time // wall-clock, parsed naively as UTC by importics
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc).UTC(), true
		}
	}
	return dt.Time.UTC(), true
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

// guardAddr reports whether a "host:port" is allowed: it rejects a literal
// non-public IP outright, and rejects a hostname any of whose resolved IPs is
// non-public. guardedDial calls it to validate the concrete IP it is about to
// dial (so the anti-rebinding guarantee comes from guardedDial pinning that IP,
// not from this check); it is also the standalone validator used in tests.
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
	if v4 := ip.To4(); v4 != nil {
		switch {
		// 0.0.0.0/8 ("this network"): IsUnspecified only catches 0.0.0.0, but
		// the kernel can route the rest of the block to the local host.
		case v4[0] == 0:
			return false
		// 100.64.0.0/10 shared/CGNAT space, which IsPrivate misses.
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return false
		// 255.255.255.255 limited broadcast.
		case v4[0] == 255 && v4[1] == 255 && v4[2] == 255 && v4[3] == 255:
			return false
		}
	}
	return true
}
