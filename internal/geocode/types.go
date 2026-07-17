package geocode

// LatLon is a geographic point.
type LatLon struct{ Lat, Lon float64 }

// Candidate is one ranked geocoding result. Confidence is Geoapify's
// rank.confidence (0–1), which expresses how well the result matched the query
// rather than how prominent the place is; the distinction that made Nominatim's
// "importance" useless for rejecting a bad match.
type Candidate struct {
	Lat, Lon    float64
	Confidence  float64
	MatchType   string // full_match | inner_part | match_by_building | …
	Formatted   string // display string, shown to the user for confirmation
	CountryCode string // lowercase ISO 3166-1 alpha-2
	SourceName  string // datasource.sourcename
}

// BetterThan reports whether c outranks o.
func (c Candidate) BetterThan(o Candidate) bool { return c.Confidence > o.Confidence }

// Query is a forward-geocoding request. The zero value queries nothing.
type Query struct {
	Text        string
	CountryCode string  // lowercase ISO 3166-1 alpha-2; "" for no filter
	Bias        *LatLon // soft proximity preference; nil for none
	Type        string  // "amenity" | "street" | "city" | ""; "" for no filter
	Limit       int     // 0 means DefaultLimit
}

// DefaultLimit is the candidate count for resolver queries: enough to contain
// the right answer for an ambiguous venue name, small enough to keep a re-rank
// prompt cheap.
const DefaultLimit = 10
