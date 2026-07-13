package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// ErrPOIUnavailable signals that every configured Overpass endpoint answered
// with a transient failure (rate limit or gateway timeout) rather than a
// result. It's distinct from a hard error so the handler can return a
// try-again-later response instead of a blunt 500.
var ErrPOIUnavailable = errors.New("overpass: all endpoints temporarily unavailable")

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

// Overpass is a client for the OpenStreetMap Overpass API. It can be given
// more than one endpoint (via a comma-separated URL) and falls back across
// them on transient failures, which matters because the public instances
// readily rate-limit or time out a busy server IP.
type Overpass struct {
	Endpoints      []string
	UserAgent      string
	HTTP           *http.Client
	Limiter        *rate.Limiter
	AttemptTimeout time.Duration // per-request deadline, so one stalled endpoint can't hog the whole call
	MaxRetries     int           // extra passes over the endpoint list after the first
	RetryWait      time.Duration // pause between passes
}

// NewOverpass builds an Overpass client with sane defaults. baseURL may be a
// single URL or a comma-separated list of endpoints tried in order (the first
// that answers wins). Each attempt is capped at AttemptTimeout so a stalled
// endpoint fails over quickly rather than hanging the user's request; the
// 1 req/sec limiter matches the public instances' fair-use policy. We don't
// auto-retry the same endpoint by default — fanning out across endpoints, plus
// the UI's "Try again", is the better recovery for a synchronous request.
func NewOverpass(baseURL, userAgent string) *Overpass {
	return &Overpass{
		Endpoints:      splitEndpoints(baseURL),
		UserAgent:      userAgent,
		HTTP:           &http.Client{Timeout: 30 * time.Second},
		Limiter:        rate.NewLimiter(rate.Every(time.Second), 1),
		AttemptTimeout: 8 * time.Second,
		MaxRetries:     0,
		RetryWait:      500 * time.Millisecond,
	}
}

// splitEndpoints turns a comma-separated URL list into a trimmed slice,
// dropping blanks so a stray trailing comma is harmless.
func splitEndpoints(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// poiCategoryTags maps a UI category key to Overpass tag-filter fragments.
// Each fragment is appended to node/way selectors, e.g. `["tourism"~"museum|gallery"]`.
var poiCategoryTags = map[string][]string{
	"sights":   {`["tourism"~"attraction|viewpoint|artwork"]`},
	"museum":   {`["tourism"~"museum|gallery"]`},
	"landmark": {`["historic"]`, `["amenity"="place_of_worship"]`, `["man_made"="tower"]`},
	"park":     {`["leisure"="park"]`, `["natural"~"peak|beach|wood"]`},
	"food":     {`["amenity"~"restaurant|cafe|bar"]`},
}

const poiResultCap = 60

func (o *Overpass) buildQuery(lat, lon float64, radiusM int, cats []string) string {
	var b strings.Builder
	b.WriteString("[out:json][timeout:25];(")
	for _, cat := range cats {
		for _, frag := range poiCategoryTags[cat] {
			// nwr matches node+way+relation in one statement, and the extra
			// ["name"] filters to named features server-side. Since we drop
			// unnamed elements anyway, this keeps the query cheap enough to
			// stand a chance on a busy public Overpass instance.
			fmt.Fprintf(&b, `nwr%s["name"](around:%d,%f,%f);`, frag, radiusM, lat, lon)
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
	// Overpass reports a server-side timeout or runtime error here while still
	// answering HTTP 200 (usually with no elements).
	Remark string `json:"remark"`
}

// Nearby queries the Overpass API for named POIs within radiusM metres of
// (lat, lon), restricted to the given category keys, and returns them
// sorted by distance ascending. Elements with no name tag are dropped since
// they carry nothing worth surfacing to a trip planner.
func (o *Overpass) Nearby(ctx context.Context, lat, lon float64, radiusM int, cats []string) ([]POI, error) {
	if len(cats) == 0 {
		return []POI{}, nil
	}
	if len(o.Endpoints) == 0 {
		return nil, errors.New("overpass: no endpoints configured")
	}
	query := o.buildQuery(lat, lon, radiusM, cats)

	// Try each endpoint in turn, then take another pass (up to MaxRetries) with
	// a short pause between passes. A transient answer (rate limit, gateway
	// timeout, or a network error) moves on to the next endpoint/pass; a
	// definitive non-200 (e.g. a 400 from a malformed query — our bug, not
	// theirs) surfaces immediately. Only if every attempt is transient do we
	// report ErrPOIUnavailable.
	sawTransient := false
	for pass := 0; pass <= o.MaxRetries; pass++ {
		if pass > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(o.RetryWait):
			}
		}
		for _, ep := range o.Endpoints {
			body, status, err := o.doQuery(ctx, ep, query)
			switch {
			case err != nil:
				sawTransient = true // network error or per-attempt timeout: fail over
			case status == http.StatusOK:
				pois, timedOut, perr := parsePOIs(body, lat, lon)
				if perr != nil {
					return nil, perr
				}
				if timedOut {
					sawTransient = true // 200 but the server gave up mid-query
					continue
				}
				return pois, nil
			case isTransientStatus(status):
				sawTransient = true
			default:
				return nil, fmt.Errorf("overpass: status %d", status)
			}
		}
	}
	// If the caller cancelled or their deadline passed, report that rather than
	// masking it as an upstream problem (which would log a false "unavailable").
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if sawTransient {
		return nil, ErrPOIUnavailable
	}
	return nil, errors.New("overpass: no endpoints configured")
}

// doQuery posts the QL query to one endpoint and returns the response body and
// status. The limiter keeps us within the public instances' fair-use policy.
func (o *Overpass) doQuery(ctx context.Context, endpoint, query string) ([]byte, int, error) {
	// Cap each attempt so one stalled endpoint can't consume the whole request;
	// on timeout we move on to the next endpoint rather than hanging.
	if o.AttemptTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.AttemptTimeout)
		defer cancel()
	}
	// Overpass accepts the raw QL query as the entire POST body (the same as
	// `wget --post-file=query.overpassql .../interpreter`), so we send it
	// untouched rather than url-encoding it into a form field.
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(query))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	req.Header.Set("User-Agent", o.UserAgent)
	req.Header.Set("Accept", "application/json")

	if o.Limiter != nil {
		if err := o.Limiter.Wait(ctx); err != nil {
			return nil, 0, err
		}
	}
	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// isTransientStatus reports whether an Overpass status is worth retrying or
// failing over: rate limiting and gateway/backend hiccups, not a 400 that
// means our query was wrong.
func isTransientStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, // 429
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}

// parsePOIs decodes an Overpass JSON body into normalised POIs, dropping
// unnamed elements and sorting by distance from (lat, lon). The bool return is
// true when Overpass answered 200 but reported a server-side timeout/runtime
// error (which the caller should treat as transient, not "nothing here").
func parsePOIs(body []byte, lat, lon float64) ([]POI, bool, error) {
	var raw overpassResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, false, fmt.Errorf("overpass: bad JSON: %w", err)
	}
	if r := strings.ToLower(raw.Remark); strings.Contains(r, "timed out") || strings.Contains(r, "runtime error") {
		return nil, true, nil
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
	return out, false, nil
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
