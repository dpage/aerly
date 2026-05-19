package providers

import "github.com/dpage/flight-tracker/internal/airports"

// LookupIATA is a thin re-export of airports.Lookup, kept here so callers
// inside this package don't need to import a second one.
func LookupIATA(code string) (lat, lon float64, ok bool) {
	return airports.Lookup(code)
}
