package geocode

import (
	"context"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/geotz"
	"github.com/dpage/aerly/internal/store"
)

// PlanParts fills any missing start/end coordinates on a plan's parts from their
// addresses, then anchors any still-floating local times to the real timezone of
// their coordinates. It is best-effort: a geocode miss or a single failed update
// is skipped rather than aborting the rest. Returns whether any part changed so
// the caller can decide to republish over SSE. A nil geocoder is a no-op (e.g. in
// tests), as is a nil store.
//
// This is the shared core behind the HTTP handler's geocodePlanAsync and the
// email-ingest path — both need a committed plan's addressed parts plotted on the
// map, so the logic lives here in the neutral geocode package (handlers already
// imports emailingest, so emailingest can't import handlers).
func PlanParts(ctx context.Context, st *store.Store, g Geocoder, planID int64) (bool, error) {
	if st == nil || g == nil {
		return false, nil
	}
	parts, err := st.PartsByPlan(ctx, planID)
	if err != nil {
		return false, err
	}
	// The trip's country biases ambiguous name-only lookups (an addressless
	// airport label) to the right country. Best-effort: a miss just means no bias.
	tripCountry, _ := st.TripCountryByPlan(ctx, planID)
	// A plan owner's pinned home coordinates (when set) override geocoding of any
	// endpoint whose address matches their home address, so a "from home" plan
	// plots on the exact pinned spot instead of a fuzzy geocode.
	homeAddr, homeLat, homeLon := planOwnerHome(ctx, st, planID)
	var changed bool
	for _, p := range parts {
		payload := store.UpdatePlanPartPayload{}
		startLat, startLon := p.StartLat, p.StartLon
		endLat, endLon := p.EndLat, p.EndLon

		// Fill missing coordinates from the address, falling back to the place
		// label (e.g. "Alicante Airport", "Melia Benidorm") when there's no
		// address OR the address doesn't resolve — so a transfer's airport
		// endpoint, which often arrives as a bare name, still plots rather than
		// collapsing onto the other end. Flight parts are skipped: their labels
		// are IATA codes located via the airport table / poller, which we must
		// not pre-empt with a fuzzy name lookup.
		// A pinned endpoint carries a manual coordinate override; never geocode
		// over it, even if its coordinates were somehow cleared.
		pinned := true
		if startLat == nil && !p.StartCoordsPinned {
			if homeLat != nil && addressIsHome(p.StartAddress, homeAddr) {
				payload.StartLat, payload.StartLon, payload.StartCoordsPinned = homeLat, homeLon, &pinned
				startLat, startLon = homeLat, homeLon
			} else if lat, lon, ok := geocodeEndpoint(ctx, g, p.Type, p.StartAddress, p.StartLabel, tripCountry); ok {
				payload.StartLat, payload.StartLon = &lat, &lon
				startLat, startLon = &lat, &lon
			}
		}
		if endLat == nil && !p.EndCoordsPinned {
			if homeLat != nil && addressIsHome(p.EndAddress, homeAddr) {
				payload.EndLat, payload.EndLon, payload.EndCoordsPinned = homeLat, homeLon, &pinned
				endLat, endLon = homeLat, homeLon
			} else if lat, lon, ok := geocodeEndpoint(ctx, g, p.Type, p.EndAddress, p.EndLabel, tripCountry); ok {
				payload.EndLat, payload.EndLon = &lat, &lon
				endLat, endLon = &lat, &lon
			}
		}

		// Anchor floating local times (tz unknown) to the zone of their
		// coordinates, shifting the instant so the displayed wall-clock is
		// preserved. Flights already carry a tz, so they're untouched.
		ResolvePartTZ(p, &payload, startLat, startLon, endLat, endLon)

		if !payload.IsEmpty() {
			if _, uerr := st.UpdatePlanPart(ctx, p.ID, payload); uerr == nil {
				changed = true
			}
		}
	}
	return changed, nil
}

// planOwnerHome returns the plan owner's normalised home address and pinned home
// coordinates, or ("", nil, nil) when the owner has no home pin. Best-effort: any
// lookup miss just means no home substitution.
func planOwnerHome(ctx context.Context, st *store.Store, planID int64) (string, *float64, *float64) {
	pl, err := st.PlanByID(ctx, planID)
	if err != nil || pl.CreatedBy == nil {
		return "", nil, nil
	}
	u, err := st.UserByID(ctx, *pl.CreatedBy)
	if err != nil || u == nil || u.HomeLat == nil || u.HomeLon == nil {
		return "", nil, nil
	}
	return normAddr(u.HomeAddress), u.HomeLat, u.HomeLon
}

// addressIsHome reports whether an endpoint address is the owner's home address.
// homeAddr must already be normalised; a blank home address never matches.
func addressIsHome(addr, homeAddr string) bool {
	return homeAddr != "" && normAddr(addr) == homeAddr
}

// normAddr normalises an address for a home match: lowercased, inner whitespace
// collapsed, and trailing spaces/punctuation trimmed.
func normAddr(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimRight(s, " .,")
}

// geocodeEndpoint resolves an endpoint to coordinates, most reliable signal first:
//  1. an IATA airport code in the label via the airport table (non-flight only) —
//     deterministic, no network;
//  2. the full postal address (normalised to one line);
//  3. the place/property name + the address's country tail (non-flight only) —
//     never the bare name, so a generic name ("Hilton") can't resolve on the
//     wrong continent; skipped when there's no label, no country tail, or the
//     label is a generic personal one ("Home") that names no place;
//  4. tail backoff: progressively shorter versions of the address (drop the
//     leading segment, first hit wins) — country-agnostic, a postcode rides along
//     in whatever tail resolves;
//  5. an airport-like label ("… Airport"/"… Terminal") via the geocoder, bare,
//     constrained to tripCountry (the trip's ISO-2 country) when known so an
//     ambiguous name ("Sal Airport") can't resolve on the wrong continent.
//
// Flight parts never use the label. ok=false when nothing resolved.
func geocodeEndpoint(ctx context.Context, g Geocoder, partType, address, label, tripCountry string) (float64, float64, bool) {
	if partType != "flight" {
		if code := iataIn(label); code != "" {
			if lat, lon, ok := airports.Lookup(code); ok {
				return lat, lon, true
			}
		}
	}
	addr := normalizeAddress(address)
	if addr != "" {
		if lat, lon, ok, err := g.Geocode(ctx, addr, ""); err == nil && ok {
			return lat, lon, true
		}
	}
	if partType != "flight" && strings.TrimSpace(label) != "" && !isGenericLabel(label) {
		if country := countryFromAddress(addr); country != "" {
			if lat, lon, ok, err := g.Geocode(ctx, label+", "+country, ""); err == nil && ok {
				return lat, lon, true
			}
		}
	}
	for _, tail := range addressTails(addr, 4) {
		if lat, lon, ok, err := g.Geocode(ctx, tail, ""); err == nil && ok {
			return lat, lon, true
		}
	}
	if partType != "flight" && isAirportLabel(label) {
		if lat, lon, ok, err := g.Geocode(ctx, label, tripCountry); err == nil && ok {
			return lat, lon, true
		}
	}
	return 0, 0, false
}

// Endpoint resolves a single plan-part endpoint to coordinates using the shared
// fallback chain. Exported so the edit handler resolves a changed address
// identically to the backfill path. tripCountry is the owning trip's ISO-2
// country code (or "") used to bias an ambiguous bare airport name.
func Endpoint(ctx context.Context, g Geocoder, partType, address, label, tripCountry string) (lat, lon float64, ok bool) {
	return geocodeEndpoint(ctx, g, partType, address, label, tripCountry)
}

// isGenericLabel reports whether a label is a personal or relative place name
// that names no geocodable location on its own — "Home", "Work", "Office" and
// the like. Such a label must never be sent to the geocoder qualified only by a
// country, or it collides with unrelated venues that share the word (e.g.
// "Home, United Kingdom" resolves to the HOME arts centre in Manchester, not to
// a traveller's actual home).
func isGenericLabel(label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "home", "my home", "my house", "house", "work", "my work",
		"office", "the office", "my place", "my flat", "my apartment":
		return true
	}
	return false
}

// normalizeAddress collapses a multi-line address into a single comma-separated
// line (Nominatim handles those far better than embedded newlines).
func normalizeAddress(s string) string {
	var parts []string
	for _, line := range strings.Split(s, "\n") {
		if l := strings.TrimSpace(line); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, ", ")
}

// countryFromAddress returns the trimmed last comma-separated segment of an
// address to qualify a name lookup, or "" when the address has no distinct tail
// (fewer than two segments). Pass a normalized address (newlines already
// collapsed to commas).
func countryFromAddress(address string) string {
	segs := strings.Split(address, ",")
	if len(segs) < 2 {
		return ""
	}
	return strings.TrimSpace(segs[len(segs)-1])
}

// addressTails returns progressively shorter versions of a comma-separated
// address, each dropping one more leading segment, most-specific first. It omits
// the full address (already tried by the caller) and the bare final segment
// (too coarse — usually just the country), and returns at most max candidates.
func addressTails(address string, max int) []string {
	segs := strings.Split(address, ",")
	for i := range segs {
		segs[i] = strings.TrimSpace(segs[i])
	}
	var tails []string
	for i := 1; i <= len(segs)-2 && len(tails) < max; i++ {
		tails = append(tails, strings.Join(segs[i:], ", "))
	}
	return tails
}

// iataIn returns an all-uppercase 3-letter token in the label that's a known
// IATA airport (e.g. "LHR" in "LHR T5"). Requiring all-caps avoids matching
// ordinary place-name words that happen to spell a code.
func iataIn(label string) string {
	for _, tok := range strings.FieldsFunc(label, func(r rune) bool {
		return !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	}) {
		if len(tok) == 3 && tok == strings.ToUpper(tok) {
			if _, _, ok := airports.Lookup(tok); ok {
				return tok
			}
		}
	}
	return ""
}

// isAirportLabel reports whether a place label clearly denotes an airport (so
// geocoding the bare name is safe). Conservative on purpose — only "airport" /
// "terminal" — to avoid mis-placing ambiguous names.
//
// It additionally requires an identifying place token beyond the generic
// keyword and a terminal designator: a bare "Terminal 3" (or "T3", "Airport")
// names no airport, so geocoding it picks an arbitrary global match — that's
// how a UK transfer once ended up at Jakarta's Soekarno-Hatta Terminal 3.
func isAirportLabel(label string) bool {
	l := strings.ToLower(label)
	if !strings.Contains(l, "airport") && !strings.Contains(l, "terminal") {
		return false
	}
	for _, tok := range strings.FieldsFunc(l, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if !isGenericAirportToken(tok) {
			return true
		}
	}
	return false
}

// isGenericAirportToken reports whether tok carries no location signal on its
// own: the "airport"/"terminal" keywords, or a bare terminal designator such as
// "3", a single letter ("a"), or a "t"-prefixed number ("t5").
func isGenericAirportToken(tok string) bool {
	switch tok {
	case "", "airport", "terminal":
		return true
	}
	if isAllDigits(tok) || len(tok) == 1 {
		return true
	}
	return tok[0] == 't' && isAllDigits(tok[1:])
}

// isAllDigits reports whether s is non-empty and every rune is an ASCII digit.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ResolvePartTZ resolves a still-empty start/end tz from the part's coordinates
// and rewrites the stored instant so the local wall-clock is unchanged. A hotel's
// checkout (no end coordinates of its own) inherits the start tz. Does nothing for
// parts that already have a tz, or that have no usable coordinate.
func ResolvePartTZ(
	p *store.PlanPart,
	payload *store.UpdatePlanPartPayload,
	startLat, startLon, endLat, endLon *float64,
) {
	// The part's primary tz: from the start coordinate, else the end.
	primary := ""
	if startLat != nil && startLon != nil {
		if tz, ok := geotz.Lookup(*startLat, *startLon); ok {
			primary = tz
		}
	} else if endLat != nil && endLon != nil {
		if tz, ok := geotz.Lookup(*endLat, *endLon); ok {
			primary = tz
		}
	}

	if p.StartTZ == "" && primary != "" {
		tz := primary
		payload.StartTZ = &tz
		if s, ok := reinterpretLocal(p.StartsAt, tz); ok {
			payload.StartsAt = &s
		}
	}

	if p.EndTZ == "" && p.EndsAt != nil {
		etz := primary
		if endLat != nil && endLon != nil {
			if tz, ok := geotz.Lookup(*endLat, *endLon); ok {
				etz = tz
			}
		}
		if etz != "" {
			payload.EndTZ = &etz
			if e, ok := reinterpretLocal(*p.EndsAt, etz); ok {
				payload.EndsAt = &e
			}
		}
	}
}

// reinterpretLocal takes an instant whose UTC wall-clock digits are really a
// floating *local* time (the ingest convention for tz-less parts) and returns
// the instant those same digits denote in tzName. e.g. 16:00Z + "America/
// New_York" → 20:00Z (16:00 EDT). Returns ok=false if the zone won't load.
func reinterpretLocal(t time.Time, tzName string) (time.Time, bool) {
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return t, false
	}
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), u.Hour(), u.Minute(), u.Second(), u.Nanosecond(), loc), true
}
