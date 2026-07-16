// Package maps extracts geographic coordinates from Google Maps URLs and
// resolves short maps.app.goo.gl links by following their redirects under a
// strict host allowlist.
package maps

import (
	"html"
	"net/url"
	"regexp"
	"strconv"
)

const num = `(-?\d+(?:\.\d+)?)`

var (
	// The pinned place in the data segment is the most accurate.
	placeRe = regexp.MustCompile(`!3d` + num + `!4d` + num)
	// Explicit query coordinates.
	queryRe = regexp.MustCompile(`[?&](?:q|ll|query|destination|center)=` + num + `,` + num)
	// The viewport centre: a fallback, close to but not always the pin.
	atRe = regexp.MustCompile(`@` + num + `,` + num)

	// A rendered Google Maps page carries the primary place's coordinates in its
	// canonical URL and og:image tags even when the request URL does not (a short
	// link, or a place identified only by a feature ID). Attribute order varies,
	// so match rel/href in either order.
	canonicalRe  = regexp.MustCompile(`(?i)<link[^>]+rel=["']canonical["'][^>]+href=["']([^"']+)["']`)
	canonicalRe2 = regexp.MustCompile(`(?i)<link[^>]+href=["']([^"']+)["'][^>]+rel=["']canonical["']`)
	ogImageRe    = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']+)["']`)
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

// ExtractLatLonFromHTML reads coordinates from a Google Maps page body when the
// request URL carried none — a short link, or a place identified only by a
// feature ID. The canonical URL and og:image tags both point at the primary
// place, so their embedded coordinates are the place's own. Returns ok=false
// when nothing usable is found. HTML entities in the attribute (e.g. &amp;) are
// decoded before parsing.
func ExtractLatLonFromHTML(body string) (lat, lon float64, ok bool) {
	for _, re := range []*regexp.Regexp{canonicalRe, canonicalRe2, ogImageRe} {
		m := re.FindStringSubmatch(body)
		if m == nil {
			continue
		}
		if la, lo, found := ExtractLatLon(html.UnescapeString(m[1])); found {
			return la, lo, true
		}
	}
	return 0, 0, false
}
