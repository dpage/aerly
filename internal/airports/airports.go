// Package airports holds a small embedded IATA → (lat, lon) table used both
// by the stub tracker and by the store layer (which resolves coordinates at
// write time so newly-created flights render on the map without waiting for
// the first poll cycle). It is a stand-alone package to avoid a store ↔
// providers import cycle.
package airports

import (
	"fmt"
	"strings"
)

type Entry struct {
	Lat, Lon float64
	Name     string
	// TZ is the IANA timezone string at the airport (e.g. "Europe/London",
	// "America/New_York"). Used to render scheduled times in the airport's
	// local time on the client.
	TZ string
	// City is the city the airport serves (e.g. "London", "New York"). Used to
	// give imported trips a place-based name instead of the first flight's
	// ident. Several airports serving the same city share a City value.
	City string
}

// Lookup returns lat/lon for a 3-letter IATA code, or zeros + false.
// Case-insensitive.
func Lookup(code string) (lat, lon float64, ok bool) {
	a, ok := table[strings.ToUpper(strings.TrimSpace(code))]
	if !ok {
		return 0, 0, false
	}
	return a.Lat, a.Lon, true
}

// LookupTZ returns the IANA timezone for a 3-letter IATA code, or "" + false.
// Case-insensitive.
func LookupTZ(code string) (string, bool) {
	a, ok := table[strings.ToUpper(strings.TrimSpace(code))]
	if !ok || a.TZ == "" {
		return "", false
	}
	return a.TZ, true
}

// LookupCity returns the city served by a 3-letter IATA code, or "" + false.
// Case-insensitive.
func LookupCity(code string) (string, bool) {
	a, ok := table[strings.ToUpper(strings.TrimSpace(code))]
	if !ok || a.City == "" {
		return "", false
	}
	return a.City, true
}

// Label renders a human-friendly airport label "Name (CODE)" for an IATA code,
// e.g. "London Heathrow (LHR)" or "Faro (FAO)". The name is the embedded
// table's airport name when the code is on-table; otherwise the supplied
// providerName (the flight data source's airport name, for off-table airports
// like NQY/FAO) is used. When no name is available it falls back to the bare
// upper-cased code ("NQY"), so the result is never blank for a non-empty code.
// A blank code yields "".
func Label(code, providerName string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return ""
	}
	name := ""
	if a, ok := table[code]; ok && a.Name != "" {
		name = a.Name
	} else if p := strings.TrimSpace(providerName); p != "" {
		name = p
	}
	if name == "" {
		return code
	}
	return fmt.Sprintf("%s (%s)", name, code)
}
