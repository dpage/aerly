// Package maps extracts geographic coordinates from Google Maps URLs and
// resolves short maps.app.goo.gl links by following their redirects under a
// strict host allowlist.
package maps

import (
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
