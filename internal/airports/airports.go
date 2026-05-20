// Package airports holds a small embedded IATA → (lat, lon) table used both
// by the stub tracker and by the store layer (which resolves coordinates at
// write time so newly-created flights render on the map without waiting for
// the first poll cycle). It is a stand-alone package to avoid a store ↔
// providers import cycle.
package airports

import "strings"

type Entry struct {
	Lat, Lon float64
	Name     string
	// TZ is the IANA timezone string at the airport (e.g. "Europe/London",
	// "America/New_York"). Used to render scheduled times in the airport's
	// local time on the client.
	TZ string
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
