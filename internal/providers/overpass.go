package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// POI is a normalised point of interest returned from OpenStreetMap.
type POI struct {
	OSMType   string
	OSMID     int64
	Name      string
	Category  string
	Lat, Lon  float64
	DistanceM int
	Address   string
	Wikidata  string
	Wikipedia string
	Website   string
}

// POIResolver looks up points of interest around a coordinate.
type POIResolver interface {
	Nearby(ctx context.Context, lat, lon float64, radiusM int, cats []string) ([]POI, error)
}

// Overpass is a client for the OpenStreetMap Overpass API.
type Overpass struct {
	BaseURL   string
	UserAgent string
	HTTP      *http.Client
	Limiter   *rate.Limiter
}

// NewOverpass builds an Overpass client with sane defaults: a 30s HTTP
// timeout and a 1 req/sec limiter, matching the Overpass public instance's
// documented fair-use policy.
func NewOverpass(baseURL, userAgent string) *Overpass {
	return &Overpass{
		BaseURL:   baseURL,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		Limiter:   rate.NewLimiter(rate.Every(time.Second), 1),
	}
}

// poiCategoryTags maps a UI category key to Overpass tag-filter fragments.
// Each fragment is appended to node/way selectors, e.g. `["tourism"~"museum|gallery"]`.
var poiCategoryTags = map[string][]string{
	"sights":   {`["tourism"~"attraction|viewpoint|artwork"]`, `["historic"]`},
	"museum":   {`["tourism"~"museum|gallery"]`},
	"landmark": {`["amenity"="place_of_worship"]`, `["man_made"="tower"]`},
	"park":     {`["leisure"="park"]`, `["natural"~"peak|beach|wood"]`},
	"food":     {`["amenity"~"restaurant|cafe|bar"]`},
}

const poiResultCap = 60

func (o *Overpass) buildQuery(lat, lon float64, radiusM int, cats []string) string {
	var b strings.Builder
	b.WriteString("[out:json][timeout:25];(")
	for _, cat := range cats {
		for _, frag := range poiCategoryTags[cat] {
			for _, elem := range []string{"node", "way"} {
				fmt.Fprintf(&b, "%s%s(around:%d,%f,%f);", elem, frag, radiusM, lat, lon)
			}
		}
	}
	fmt.Fprintf(&b, ");out center %d;", poiResultCap)
	return b.String()
}

type overpassResponse struct {
	Elements []struct {
		Type   string  `json:"type"`
		ID     int64   `json:"id"`
		Lat    float64 `json:"lat"`
		Lon    float64 `json:"lon"`
		Center *struct {
			Lat, Lon float64
		} `json:"center"`
		Tags map[string]string `json:"tags"`
	} `json:"elements"`
}

// Nearby queries the Overpass API for named POIs within radiusM metres of
// (lat, lon), restricted to the given category keys, and returns them
// sorted by distance ascending. Elements with no name tag are dropped since
// they carry nothing worth surfacing to a trip planner.
func (o *Overpass) Nearby(ctx context.Context, lat, lon float64, radiusM int, cats []string) ([]POI, error) {
	if len(cats) == 0 {
		return []POI{}, nil
	}
	// Overpass accepts the raw QL query as the entire POST body (the same
	// as `wget --post-file=query.overpassql .../interpreter`), so we send
	// it untouched rather than url-encoding it into a form field.
	query := o.buildQuery(lat, lon, radiusM, cats)
	req, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL, strings.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("User-Agent", o.UserAgent)
	req.Header.Set("Accept", "application/json")

	if o.Limiter != nil {
		if err := o.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("overpass: status %d", resp.StatusCode)
	}
	var raw overpassResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("overpass: bad JSON: %w", err)
	}

	out := make([]POI, 0, len(raw.Elements))
	for _, el := range raw.Elements {
		name := el.Tags["name"]
		if name == "" {
			continue
		}
		plat, plon := el.Lat, el.Lon
		if el.Center != nil {
			plat, plon = el.Center.Lat, el.Center.Lon
		}
		out = append(out, POI{
			OSMType:   el.Type,
			OSMID:     el.ID,
			Name:      name,
			Category:  categoryOf(el.Tags),
			Lat:       plat,
			Lon:       plon,
			DistanceM: int(haversineM(lat, lon, plat, plon)),
			Address:   addressOf(el.Tags),
			Wikidata:  el.Tags["wikidata"],
			Wikipedia: el.Tags["wikipedia"],
			Website:   el.Tags["website"],
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DistanceM < out[j].DistanceM })
	return out, nil
}

// categoryOf classifies a POI from its tags in priority order.
func categoryOf(tags map[string]string) string {
	switch {
	case tags["tourism"] == "museum" || tags["tourism"] == "gallery":
		return "museum"
	case tags["historic"] != "" || tags["amenity"] == "place_of_worship" || tags["man_made"] == "tower":
		return "landmark"
	case tags["leisure"] == "park" || tags["natural"] != "":
		return "park"
	case tags["amenity"] != "":
		return "food"
	default:
		return "sights"
	}
}

func addressOf(tags map[string]string) string {
	parts := []string{}
	for _, k := range []string{"addr:housenumber", "addr:street", "addr:city"} {
		if v := tags[k]; v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " ")
}

func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return r * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}
