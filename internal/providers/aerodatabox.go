package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// AeroDataBox is a Resolver backed by the AeroDataBox API on RapidAPI.
// Documentation: https://rapidapi.com/aedbx-aedbx/api/aerodatabox/
//
// Lookups go to GET /flights/number/{ident}/{date} which returns an array
// of flight legs (one per operator + codeshare entry). We prefer the
// canonical operator row.
type AeroDataBox struct {
	APIKey  string
	BaseURL string
	Host    string // RapidAPI host header
	HTTP    *http.Client
}

func NewAeroDataBox(apiKey string) *AeroDataBox {
	return &AeroDataBox{
		APIKey:  apiKey,
		BaseURL: "https://aerodatabox.p.rapidapi.com",
		Host:    "aerodatabox.p.rapidapi.com",
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

// Resolve looks up a flight on AeroDataBox. Airlines refer to the same
// flight as e.g. "BA87", "BA087", or "BA0087" interchangeably, but
// AeroDataBox keys them under one canonical form. To absorb that, on a
// "not found" response we retry with every reasonable zero-padding of the
// numeric portion (pad-length 2 → 5) before giving up.
func (a *AeroDataBox) Resolve(ctx context.Context, ident string, date time.Time) (*ResolvedFlight, error) {
	ident = strings.ToUpper(strings.TrimSpace(ident))
	if ident == "" {
		return nil, fmt.Errorf("ident required")
	}
	d := date.UTC().Format("2006-01-02")
	variants := identVariants(ident)
	for _, v := range variants {
		rf, err := a.resolveOne(ctx, v, d)
		if err == nil {
			return rf, nil
		}
		if !errors.Is(err, ErrFlightNotFound) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("no flight found for %s on %s: %w", ident, d, ErrFlightNotFound)
}

// resolveOne issues a single GET /flights/number/{ident}/{date} call and
// returns the picked operator row, or ErrFlightNotFound if the upstream
// has no record of this exact ident on this date.
func (a *AeroDataBox) resolveOne(ctx context.Context, ident, date string) (*ResolvedFlight, error) {
	q := url.Values{}
	q.Set("withAircraftImage", "false")
	q.Set("withLocation", "true")
	u := fmt.Sprintf("%s/flights/number/%s/%s?%s",
		a.BaseURL, url.PathEscape(ident), date, q.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-RapidAPI-Key", a.APIKey)
	req.Header.Set("X-RapidAPI-Host", a.Host)
	req.Header.Set("Accept", "application/json")

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	// AeroDataBox answers a well-formed request that simply has no matching
	// schedule with 204 No Content (empty body) rather than 404. Treat both
	// as a clean "nothing found".
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return nil, ErrFlightNotFound
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("aerodatabox rate limit hit — try again shortly")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aerodatabox %d: %s", resp.StatusCode, body)
	}

	var flights []adbFlight
	if err := json.Unmarshal(body, &flights); err != nil {
		return nil, fmt.Errorf("aerodatabox: bad JSON: %w", err)
	}
	if len(flights) == 0 {
		return nil, ErrFlightNotFound
	}

	pick := flights[0]
	for i := range flights {
		if flights[i].CodeshareStatus == "IsOperator" {
			pick = flights[i]
			break
		}
	}
	return buildResolved(&pick, ident), nil
}

// identVariants returns the input ident plus zero-padded re-spellings of
// its numeric portion. For example:
//
//	"BA87"   → [BA87,   BA087, BA0087, BA00087]
//	"BA0087" → [BA0087, BA87,  BA087,  BA00087]
//	"9W420"  → [9W420,  9W0420, 9W00420]
//
// Idents that don't match an "airline code + digits" pattern (the prefix
// must contain at least one letter) are passed through unchanged so we
// don't generate junk for pure-digit or pathological inputs.
func identVariants(ident string) []string {
	m := identRe.FindStringSubmatch(ident)
	if m == nil {
		return []string{ident}
	}
	prefix := m[1]
	if !strings.ContainsAny(prefix, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return []string{ident}
	}
	num := strings.TrimLeft(m[2], "0")
	if num == "" {
		// e.g. "BA0000" — all zeros, weird; return as-is.
		return []string{ident}
	}
	seen := map[string]bool{ident: true}
	out := []string{ident}
	const maxPad = 5
	add := func(length int) {
		if length < len(num) || length > maxPad {
			return
		}
		v := prefix + strings.Repeat("0", length-len(num)) + num
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	// AeroDataBox stores flights at the 4-digit padded form ("BA0087").
	// Try that immediately after the user's literal input so the common
	// case ("user typed BA87, canonical is BA0087") costs two API calls
	// rather than three.
	add(4)
	for length := len(num); length <= maxPad; length++ {
		add(length)
	}
	return out
}

// Airline codes can start with a digit (e.g. "9W"), but they must contain
// at least one letter — enforced by the post-regex check above.
var identRe = regexp.MustCompile(`^([A-Z0-9]+?)(\d+)$`)

func buildResolved(f *adbFlight, fallbackIdent string) *ResolvedFlight {
	// AeroDataBox returns "BA 286" — split on whitespace then re-join so the
	// canonical "BA286" form lands in the DTO regardless of the upstream's
	// formatting.
	r := &ResolvedFlight{Ident: strings.ToUpper(strings.Join(strings.Fields(f.Number), ""))}
	if r.Ident == "" {
		r.Ident = fallbackIdent
	}
	if t := f.Departure.ScheduledTime; t != nil {
		if parsed, err := parseADBTime(t.UTC); err == nil {
			r.ScheduledOut = parsed
		}
	}
	if t := f.Arrival.ScheduledTime; t != nil {
		if parsed, err := parseADBTime(t.UTC); err == nil {
			r.ScheduledIn = parsed
		}
	}
	if a := f.Departure.Airport; a.IATA != "" {
		r.OriginIATA = a.IATA
		if a.Location != nil {
			r.OriginLat = a.Location.Lat
			r.OriginLon = a.Location.Lon
		}
	}
	if a := f.Arrival.Airport; a.IATA != "" {
		r.DestIATA = a.IATA
		if a.Location != nil {
			r.DestLat = a.Location.Lat
			r.DestLon = a.Location.Lon
		}
	}
	if f.Aircraft != nil {
		r.ICAO24 = strings.ToLower(strings.TrimSpace(f.Aircraft.ModeS))
	}
	var notes []string
	if f.Airline != nil && f.Airline.Name != "" {
		notes = append(notes, f.Airline.Name)
	}
	if f.Aircraft != nil && f.Aircraft.Model != "" {
		notes = append(notes, f.Aircraft.Model)
	}
	r.Notes = strings.Join(notes, " · ")
	return r
}

// parseADBTime handles the AeroDataBox UTC time format, which uses a space
// between the date and time component instead of an ISO 'T'.
func parseADBTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	// Replace the first space with a 'T' so RFC3339 parser is happy.
	s = strings.Replace(s, " ", "T", 1)
	// AeroDataBox sometimes omits seconds (e.g. "2026-05-19T08:30Z").
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02T15:04Z", s); err == nil {
		return t.UTC(), nil
	}
	return time.Parse("2006-01-02T15:04:05Z07:00", s)
}

// AeroDataBox JSON shape (just the fields we use).

type adbFlight struct {
	Number          string       `json:"number"`
	CallSign        string       `json:"callSign"`
	Status          string       `json:"status"`
	CodeshareStatus string       `json:"codeshareStatus"`
	Departure       adbMovement  `json:"departure"`
	Arrival         adbMovement  `json:"arrival"`
	Aircraft        *adbAircraft `json:"aircraft,omitempty"`
	Airline         *adbAirline  `json:"airline,omitempty"`
}

type adbMovement struct {
	Airport       adbAirport `json:"airport"`
	ScheduledTime *adbTime   `json:"scheduledTime,omitempty"`
}

type adbTime struct {
	UTC   string `json:"utc"`
	Local string `json:"local"`
}

type adbAirport struct {
	IATA     string       `json:"iata"`
	ICAO     string       `json:"icao"`
	Name     string       `json:"name"`
	Location *adbLocation `json:"location,omitempty"`
}

type adbLocation struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type adbAircraft struct {
	Reg   string `json:"reg"`
	ModeS string `json:"modeS"`
	Model string `json:"model"`
}

type adbAirline struct {
	Name string `json:"name"`
	IATA string `json:"iata"`
	ICAO string `json:"icao"`
}

// Compile-time check that AeroDataBox satisfies Resolver.
var _ Resolver = (*AeroDataBox)(nil)
