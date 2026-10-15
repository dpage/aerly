// Package weather fetches short daily forecasts from Open-Meteo
// (https://open-meteo.com) for the departure summary. The free tier needs no
// API key.
package weather

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
	"unicode/utf8"
)

// Day is one day's forecast. Temperatures are in Celsius; PrecipPct is nil
// when Open-Meteo has no precipitation probability for the day (it returns
// null beyond the range of its probability models).
type Day struct {
	Date      string // YYYY-MM-DD, in the forecast location's time zone
	Code      int    // WMO weather code; see Describe
	MaxC      float64
	MinC      float64
	PrecipPct *int
}

// Forecaster is what the poller depends on, so tests can substitute a fake.
type Forecaster interface {
	Forecast(ctx context.Context, lat, lon float64, tz string, from time.Time, days int) ([]Day, error)
}

// OpenMeteo is a client for the Open-Meteo forecast API.
type OpenMeteo struct {
	BaseURL string
	HTTP    *http.Client
}

// DefaultBaseURL is the public Open-Meteo API root.
const DefaultBaseURL = "https://api.open-meteo.com"

// NewOpenMeteo builds a client for the given base URL, falling back to the
// public API when it is empty.
func NewOpenMeteo(baseURL string) *OpenMeteo {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &OpenMeteo{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 15 * time.Second},
	}
}

type openMeteoResponse struct {
	Daily struct {
		Time          []string  `json:"time"`
		WeatherCode   []int     `json:"weather_code"`
		TempMax       []float64 `json:"temperature_2m_max"`
		TempMin       []float64 `json:"temperature_2m_min"`
		PrecipProbMax []*int    `json:"precipitation_probability_max"`
	} `json:"daily"`
}

// maxErrBody bounds how much of an error response is quoted back in an error.
const maxErrBody = 200

// truncate cuts s to at most n bytes, backing up to a rune boundary so a
// multi-byte character is never split, as providers' truncate does.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
}

// Forecast returns up to days daily forecasts starting at the calendar date of
// from. tz is an IANA zone name; empty asks Open-Meteo to work it out from the
// coordinates. Dates outside Open-Meteo's forecast range produce an error.
func (o *OpenMeteo) Forecast(ctx context.Context, lat, lon float64, tz string, from time.Time, days int) ([]Day, error) {
	if days < 1 {
		return nil, fmt.Errorf("open-meteo: days must be at least 1, got %d", days)
	}
	if tz == "" {
		tz = "auto"
	}
	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	params.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	params.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max")
	params.Set("timezone", tz)
	params.Set("start_date", from.Format("2006-01-02"))
	params.Set("end_date", from.AddDate(0, 0, days-1).Format("2006-01-02"))

	req, err := http.NewRequestWithContext(ctx, "GET", o.BaseURL+"/v1/forecast?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Aerly")

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("open-meteo: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("open-meteo: reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo: status %d: %s", resp.StatusCode,
			truncate(strings.TrimSpace(string(body)), maxErrBody))
	}

	var raw openMeteoResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("open-meteo: bad JSON: %w", err)
	}
	d := raw.Daily
	n := len(d.Time)
	if len(d.WeatherCode) != n || len(d.TempMax) != n || len(d.TempMin) != n {
		return nil, fmt.Errorf("open-meteo: daily arrays differ in length")
	}
	out := make([]Day, 0, n)
	for i := range d.Time {
		day := Day{
			Date: d.Time[i],
			Code: d.WeatherCode[i],
			MaxC: d.TempMax[i],
			MinC: d.TempMin[i],
		}
		if i < len(d.PrecipProbMax) {
			day.PrecipPct = d.PrecipProbMax[i]
		}
		out = append(out, day)
	}
	return out, nil
}

var _ Forecaster = (*OpenMeteo)(nil)
