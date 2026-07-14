package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// Geoapify resolves POIs via the Geoapify Places API (https://api.geoapify.com).
// It's a keyed, purpose-built service that answers categorised POI queries
// directly, and its data is OpenStreetMap-derived (hence the OSM attribution in
// the UI and the description/wikidata tags we surface).
type Geoapify struct {
	APIKey  string
	BaseURL string
	HTTP    *http.Client
	Limiter *rate.Limiter
}

// NewGeoapify builds a Geoapify Places client. The free tier allows a few
// requests per second, which the limiter respects; results are cached upstream.
func NewGeoapify(apiKey string) *Geoapify {
	return &Geoapify{
		APIKey:  apiKey,
		BaseURL: "https://api.geoapify.com/v2/places",
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		Limiter: rate.NewLimiter(rate.Every(200*time.Millisecond), 5),
	}
}

// geoapifyCategoryCodes maps our UI chip keys to Geoapify category codes.
// Values are unioned into a single request; Geoapify handles multi-category
// queries server-side. Places of worship are their own key rather than being
// folded into landmark, so they can be filtered independently (they're numerous
// enough to swamp genuine landmarks otherwise).
var geoapifyCategoryCodes = map[string][]string{
	"sights":   {"tourism.sights", "tourism.attraction"},
	"museum":   {"entertainment.museum"},
	"landmark": {"heritage"},
	"worship":  {"religion.place_of_worship"},
	"park":     {"leisure.park", "national_park", "natural"},
	"food":     {"catering.restaurant", "catering.cafe", "catering.bar"},
}

const geoapifyResultCap = 60

type geoapifyResponse struct {
	Features []struct {
		Properties struct {
			Name         string   `json:"name"`
			Categories   []string `json:"categories"`
			Lat          float64  `json:"lat"`
			Lon          float64  `json:"lon"`
			Formatted    string   `json:"formatted"`
			AddressLine1 string   `json:"address_line1"`
			Distance     float64  `json:"distance"`
			PlaceID      string   `json:"place_id"`
			Website      string   `json:"website"`
			Datasource   struct {
				Raw map[string]any `json:"raw"`
			} `json:"datasource"`
		} `json:"properties"`
	} `json:"features"`
}

func (g *Geoapify) Nearby(ctx context.Context, lat, lon float64, radiusM int, cats []string) ([]POI, error) {
	if len(cats) == 0 {
		return []POI{}, nil
	}
	codes := g.categoryCodes(cats)
	if len(codes) == 0 {
		return []POI{}, nil
	}

	params := url.Values{}
	params.Set("categories", strings.Join(codes, ","))
	// Geoapify takes coordinates as lon,lat.
	params.Set("filter", fmt.Sprintf("circle:%f,%f,%d", lon, lat, radiusM))
	params.Set("bias", fmt.Sprintf("proximity:%f,%f", lon, lat))
	params.Set("limit", strconv.Itoa(geoapifyResultCap))
	params.Set("apiKey", g.APIKey)

	req, err := http.NewRequestWithContext(ctx, "GET", g.BaseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	if g.Limiter != nil {
		if err := g.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		if isTransientStatus(resp.StatusCode) {
			return nil, ErrPOIUnavailable
		}
		return nil, fmt.Errorf("geoapify: status %d", resp.StatusCode)
	}

	var raw geoapifyResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("geoapify: bad JSON: %w", err)
	}

	out := make([]POI, 0, len(raw.Features))
	for _, f := range raw.Features {
		p := f.Properties
		if p.Name == "" {
			continue
		}
		addr := p.AddressLine1
		if addr == "" {
			addr = p.Formatted
		}
		website := p.Website
		if website == "" {
			website = rawString(p.Datasource.Raw, "website")
		}
		out = append(out, POI{
			ID:          p.PlaceID,
			Name:        p.Name,
			Category:    geoapifyCategory(p.Categories),
			Lat:         p.Lat,
			Lon:         p.Lon,
			DistanceM:   int(p.Distance),
			Address:     addr,
			Description: rawString(p.Datasource.Raw, "description"),
			Wikidata:    rawString(p.Datasource.Raw, "wikidata"),
			Wikipedia:   rawString(p.Datasource.Raw, "wikipedia"),
			Website:     website,
		})
	}
	// Geoapify already sorts by the proximity bias, but be explicit so callers
	// get a guaranteed distance-ascending contract.
	sortByDistance(out)
	return out, nil
}

// categoryCodes unions the Geoapify codes for the requested chip keys, deduped.
func (g *Geoapify) categoryCodes(cats []string) []string {
	seen := map[string]bool{}
	var codes []string
	for _, c := range cats {
		for _, code := range geoapifyCategoryCodes[c] {
			if !seen[code] {
				seen[code] = true
				codes = append(codes, code)
			}
		}
	}
	return codes
}

// geoapifyCategory classifies a feature into our chip key from its Geoapify
// category codes, most-specific first. Worship is checked before heritage so a
// historic church lands under Worship (what a user filtering on it expects)
// rather than Landmark.
func geoapifyCategory(cats []string) string {
	has := func(prefix string) bool {
		for _, c := range cats {
			if c == prefix || strings.HasPrefix(c, prefix+".") {
				return true
			}
		}
		return false
	}
	switch {
	case has("entertainment.museum"):
		return "museum"
	case has("religion.place_of_worship"):
		return "worship"
	case has("heritage"):
		return "landmark"
	case has("leisure.park") || has("national_park") || has("natural"):
		return "park"
	case has("catering"):
		return "food"
	default:
		return "sights"
	}
}

func rawString(raw map[string]any, key string) string {
	if raw == nil {
		return ""
	}
	if v, ok := raw[key].(string); ok {
		return v
	}
	return ""
}
