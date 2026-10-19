package timing

import (
	"strings"

	"github.com/dpage/aerly/internal/airports"
)

// placeNames names zones whose IANA city part would mislead a reader.
var placeNames = map[string]string{
	"Etc/UTC": "UTC",
	"UTC":     "UTC",
}

// PlaceName is the place a label quotes for a zone: the override table, then
// the airport's city, then the IANA zone's city part ("Asia/Tashkent" gives
// "Tashkent"). It is empty when the zone is unknown and there is no airport.
func PlaceName(tz, iata string) string {
	if p, ok := placeNames[tz]; ok {
		return p
	}
	if iata != "" {
		if c, ok := airports.LookupCity(iata); ok && c != "" {
			return c
		}
	}
	if tz == "" {
		return ""
	}
	return strings.ReplaceAll(tz[strings.LastIndex(tz, "/")+1:], "_", " ")
}
