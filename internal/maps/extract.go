// Package maps extracts geographic coordinates from Google Maps URLs and
// resolves short maps.app.goo.gl links by following their redirects under a
// strict host allowlist.
package maps

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const num = `(-?\d+(?:\.\d+)?)`

var (
	// The pinned place in the data segment is the most accurate.
	placeRe = regexp.MustCompile(`!3d` + num + `!4d` + num)
	// Explicit query coordinates.
	queryRe = regexp.MustCompile(`[?&](?:q|ll|query|destination|center)=` + num + `,` + num)
	// The viewport centre: a fallback, close to but not always the pin.
	atRe = regexp.MustCompile(`@` + num + `,` + num)
	// placePathRe captures the name segment of a /maps/place/<name> URL, stopping
	// at the next path segment (Google appends /data=... and /@... segments).
	placePathRe = regexp.MustCompile(`/maps/place/([^/?#]+)`)
)

// ExtractLatLon pulls coordinates from a full Google Maps URL, in precedence
// order: the pinned place (!3d!4d), explicit query coords, then the @ viewport.
// Percent-encoded commas are decoded first. Returns ok=false for a place-only
// URL or out-of-range values.
func ExtractLatLon(rawURL string) (lat, lon float64, ok bool) {
	s := rawURL
	if dec, err := url.QueryUnescape(rawURL); err == nil {
		s = dec
	}
	for _, re := range []*regexp.Regexp{placeRe, queryRe, atRe} {
		m := re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		la, err1 := strconv.ParseFloat(m[1], 64)
		lo, err2 := strconv.ParseFloat(m[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		if la < -90 || la > 90 || lo < -180 || lo > 180 {
			continue
		}
		return la, lo, true
	}
	return 0, 0, false
}

// ExtractHint pulls the human-readable place text out of a Google Maps URL that
// carries no coordinates: the q= parameter (which the iOS "Share" link's
// destination carries alongside its feature ID) or the /maps/place/<name>
// segment.
//
// This is deliberately not a coordinate: there is no supported way to turn a
// Google feature ID (ftid) or CID into a location, and reading the rendered page
// is both unreliable (Google re-centres on the caller's IP) and against their
// terms. The text is a lead to geocode and show the user for confirmation, never
// a pin to plot silently.
//
// CALLING CONTRACT: only call this once ExtractLatLon has already returned
// ok=false for the same URL. ExtractHint checks only whether the q= value is
// itself a coordinate pair; it does not know about the !3d!4d or @lat,lon forms,
// so a URL like /maps/place/X/@48.86,2.29,17z/data=!3d48.8584!4d2.2945 would
// yield the near-useless hint "X" even though it carries an exact pin. Exact
// coordinates always win: a geocoded hint is a guess we must ask the user about,
// whereas the pin in the URL is the answer they already chose.
//
// ok=false when the URL names no place.
func ExtractHint(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	if q := strings.TrimSpace(u.Query().Get("q")); q != "" {
		if _, _, isCoord := ExtractLatLon("?q=" + url.QueryEscape(q)); !isCoord {
			return q, true
		}
		return "", false
	}
	if m := placePathRe.FindStringSubmatch(u.EscapedPath()); m != nil {
		name, err := url.PathUnescape(strings.ReplaceAll(m[1], "+", " "))
		if err != nil {
			return "", false
		}
		if name = strings.TrimSpace(name); name != "" {
			return name, true
		}
	}
	return "", false
}
