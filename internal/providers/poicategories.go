package providers

import "strings"

// subcategoryCodes maps our fine-grained sub-category keys to Geoapify Places
// category codes. Values are unioned into a single request; Geoapify handles
// multi-category queries server-side. This is the single source of truth shared
// by the Geoapify provider (query + result classification) and the
// natural-language category resolver.
//
// Codes are Geoapify's OSM-derived taxonomy, verified against the live category
// reference on 2026-07-27 (Task 0). An unknown code silently returns nothing, so
// do not alter a code without re-checking it against the reference.
var subcategoryCodes = map[string][]string{
	// Sights & landmarks
	"attractions":        {"tourism.sights", "tourism.attraction"},
	"viewpoints":         {"tourism.attraction.viewpoint"},
	"monuments_heritage": {"heritage", "tourism.sights.memorial", "tourism.sights.memorial.monument"},
	// Food & drink
	"restaurants": {"catering.restaurant"},
	"cafes":       {"catering.cafe"},
	"bars":        {"catering.bar"},
	"pubs":        {"catering.pub"},
	"street_food": {"catering.fast_food", "catering.food_court"},
	// Live music & nightlife. Geoapify has no dedicated live-music/music-venue
	// category, so live_venues is the best available proxy: general events
	// venues plus theatres and arts centres (which host concerts).
	"nightclubs":  {"adult.nightclub"},
	"live_venues": {"activity.events_venue", "entertainment.culture.theatre", "entertainment.culture.arts_centre"},
	"cinemas":     {"entertainment.cinema"},
	// Culture
	"museums":   {"entertainment.museum"},
	"galleries": {"entertainment.culture.gallery"},
	"theatres":  {"entertainment.culture.theatre"},
	// Outdoors & nature
	"parks_gardens":    {"leisure.park", "leisure.park.garden", "national_park"},
	"natural_features": {"natural"},
	"beaches":          {"beach"},
	// Shopping
	"markets":          {"commercial.marketplace"},
	"malls":            {"commercial.shopping_mall"},
	"speciality_shops": {"commercial.hobby.music", "commercial.books"},
	// Sport & leisure. Geoapify nests these under sport.* only (no leisure.*
	// equivalents exist).
	"sports_centres": {"sport.sports_centre"},
	"swimming":       {"sport.swimming_pool"},
	"stadiums":       {"sport.stadium"},
	// Family
	"zoos_aquariums": {"entertainment.zoo", "entertainment.aquarium"},
	"theme_parks":    {"entertainment.theme_park"},
	"playgrounds":    {"leisure.playground"},
	// Worship
	"worship": {"religion.place_of_worship"},
}

// themeOrder is the display order of themes in the picker.
var themeOrder = []string{
	"sights", "food_drink", "nightlife", "culture",
	"outdoors", "shopping", "sport", "family", "worship",
}

// themeSubcategories groups sub-category keys under a display theme. A theme's
// children are the toggles revealed when it is expanded; selecting the theme
// selects all of them (that expansion happens client-side, so the backend only
// ever receives sub-category keys).
var themeSubcategories = map[string][]string{
	"sights":     {"attractions", "viewpoints", "monuments_heritage"},
	"food_drink": {"restaurants", "cafes", "bars", "pubs", "street_food"},
	"nightlife":  {"nightclubs", "live_venues", "cinemas"},
	"culture":    {"museums", "galleries", "theatres"},
	"outdoors":   {"parks_gardens", "natural_features", "beaches"},
	"shopping":   {"markets", "malls", "speciality_shops"},
	"sport":      {"sports_centres", "swimming", "stadiums"},
	"family":     {"zoos_aquariums", "theme_parks", "playgrounds"},
	"worship":    {"worship"},
}

// classifyOrder lists sub-category keys most-specific first for Classify. A
// feature can carry several codes (a historic church has both worship and
// heritage); the first key whose codes match wins, so worship must precede
// monuments_heritage, and specific catering keys precede the generic ones.
var classifyOrder = []string{
	"worship",
	"museums", "galleries", "theatres", "cinemas", "live_venues", "nightclubs",
	"restaurants", "cafes", "bars", "pubs", "street_food",
	"zoos_aquariums", "theme_parks", "playgrounds",
	"markets", "malls", "speciality_shops",
	"sports_centres", "swimming", "stadiums",
	"viewpoints", "monuments_heritage", "beaches", "parks_gardens", "natural_features",
	"attractions",
}

// classifyDefault is returned when a feature's codes match no sub-category, so
// Classify never returns "" (the frontend indexes an icon map by the result).
const classifyDefault = "attractions"

// ValidSubcategoryKeys returns the set of known sub-category keys, for input
// validation on the wire and in the resolver.
func ValidSubcategoryKeys() map[string]bool {
	out := make(map[string]bool, len(subcategoryCodes))
	for k := range subcategoryCodes {
		out[k] = true
	}
	return out
}

// SubcategoryCodes unions the Geoapify codes for the requested sub-category
// keys, deduped and order-stable. Unknown keys contribute nothing.
func SubcategoryCodes(keys []string) []string {
	seen := map[string]bool{}
	var codes []string
	for _, k := range keys {
		for _, code := range subcategoryCodes[k] {
			if !seen[code] {
				seen[code] = true
				codes = append(codes, code)
			}
		}
	}
	return codes
}

// codeMatches reports whether any of a feature's codes is, or is a descendant
// of, one of the target codes (Geoapify returns hierarchical codes like
// "catering.restaurant.pizza").
func codeMatches(featureCodes, targets []string) bool {
	for _, target := range targets {
		for _, c := range featureCodes {
			if c == target || strings.HasPrefix(c, target+".") {
				return true
			}
		}
	}
	return false
}

// Classify maps a Geoapify feature's category codes to our best sub-category
// key, most-specific first. It always returns a valid key.
func Classify(featureCodes []string) string {
	for _, key := range classifyOrder {
		if codeMatches(featureCodes, subcategoryCodes[key]) {
			return key
		}
	}
	return classifyDefault
}
