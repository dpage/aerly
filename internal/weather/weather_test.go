package weather

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var day0 = time.Date(2026, 10, 16, 14, 30, 0, 0, time.UTC)

const okBody = `{"daily":{"time":["2026-10-16","2026-10-17"],"weather_code":[61,0],` +
	`"temperature_2m_max":[16.0,18.5],"temperature_2m_min":[3.6,5.0],` +
	`"precipitation_probability_max":[19,null]}}`

func TestForecastParsesAndBuildsRequest(t *testing.T) {
	var gotPath string
	var gotQuery map[string]string
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		gotQuery = map[string]string{}
		for k := range r.URL.Query() {
			gotQuery[k] = r.URL.Query().Get(k)
		}
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	days, err := NewOpenMeteo(srv.URL+"/").Forecast(context.Background(), 41.98, -87.9, "America/Chicago", day0, 2)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	if gotPath != "/v1/forecast" || gotUA != "Aerly" {
		t.Errorf("path=%q ua=%q", gotPath, gotUA)
	}
	want := map[string]string{
		"latitude": "41.9800", "longitude": "-87.9000", "timezone": "America/Chicago",
		"start_date": "2026-10-16", "end_date": "2026-10-17",
		"daily": "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max",
	}
	for k, v := range want {
		if gotQuery[k] != v {
			t.Errorf("query %s = %q, want %q", k, gotQuery[k], v)
		}
	}
	if len(days) != 2 {
		t.Fatalf("got %d days", len(days))
	}
	if days[0].Date != "2026-10-16" || days[0].Code != 61 || days[0].MaxC != 16.0 || days[0].MinC != 3.6 {
		t.Errorf("day0 = %+v", days[0])
	}
	if days[0].PrecipPct == nil || *days[0].PrecipPct != 19 {
		t.Errorf("day0 precip = %v", days[0].PrecipPct)
	}
	if days[1].PrecipPct != nil {
		t.Errorf("day1 precip should be nil, got %d", *days[1].PrecipPct)
	}
}

func TestForecastEmptyTimezoneUsesAuto(t *testing.T) {
	var tz string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tz = r.URL.Query().Get("timezone")
		_, _ = w.Write([]byte(`{"daily":{"time":[],"weather_code":[],"temperature_2m_max":[],"temperature_2m_min":[],"precipitation_probability_max":[]}}`))
	}))
	defer srv.Close()
	days, err := NewOpenMeteo(srv.URL).Forecast(context.Background(), 1, 2, "", day0, 1)
	if err != nil || len(days) != 0 {
		t.Fatalf("days=%v err=%v", days, err)
	}
	if tz != "auto" {
		t.Errorf("timezone = %q, want auto", tz)
	}
}

func TestForecastMissingPrecipArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"daily":{"time":["2026-10-16"],"weather_code":[3],"temperature_2m_max":[10],"temperature_2m_min":[2]}}`))
	}))
	defer srv.Close()
	days, err := NewOpenMeteo(srv.URL).Forecast(context.Background(), 1, 2, "UTC", day0, 1)
	if err != nil || len(days) != 1 || days[0].PrecipPct != nil {
		t.Fatalf("days=%v err=%v", days, err)
	}
}

func TestForecastErrors(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"bad request", 400, `{"error":true,"reason":"Date out of range"}`, "status 400"},
		{"long error body truncated", 500, strings.Repeat("x", 500), "..."},
		{"bad json", 200, `not json`, "bad JSON"},
		{"mismatched arrays", 200, `{"daily":{"time":["a","b"],"weather_code":[1],"temperature_2m_max":[1],"temperature_2m_min":[1]}}`, "differ in length"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			_, err := NewOpenMeteo(srv.URL).Forecast(context.Background(), 1, 2, "UTC", day0, 1)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, c.wantErr)
			}
			if c.name == "long error body truncated" && len(err.Error()) > 300 {
				t.Errorf("error not truncated: %d bytes", len(err.Error()))
			}
		})
	}
}

func TestForecastRejectsNonPositiveDays(t *testing.T) {
	if _, err := NewOpenMeteo("").Forecast(context.Background(), 1, 2, "UTC", day0, 0); err == nil {
		t.Fatal("expected an error for days=0")
	}
}

func TestForecastTransportAndRequestErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listening now
	if _, err := NewOpenMeteo(url).Forecast(context.Background(), 1, 2, "UTC", day0, 1); err == nil {
		t.Fatal("expected a transport error")
	}
	if _, err := NewOpenMeteo("http://bad host").Forecast(context.Background(), 1, 2, "UTC", day0, 1); err == nil {
		t.Fatal("expected a request-building error")
	}
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (errBody) Close() error             { return nil }

type bodyErrTransport struct{}

func (bodyErrTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: errBody{}, Header: http.Header{}}, nil
}

func TestForecastBodyReadError(t *testing.T) {
	o := NewOpenMeteo("http://example.test")
	o.HTTP = &http.Client{Transport: bodyErrTransport{}}
	if _, err := o.Forecast(context.Background(), 1, 2, "UTC", day0, 1); err == nil || !strings.Contains(err.Error(), "reading response") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewOpenMeteoDefaults(t *testing.T) {
	o := NewOpenMeteo("")
	if o.BaseURL != DefaultBaseURL || o.HTTP.Timeout != 15*time.Second {
		t.Errorf("defaults = %q %v", o.BaseURL, o.HTTP.Timeout)
	}
}

type fakeForecaster struct {
	calls int
	err   error
}

func (f *fakeForecaster) Forecast(_ context.Context, _, _ float64, _ string, _ time.Time, _ int) ([]Day, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return []Day{{Date: "2026-10-16", Code: 1}}, nil
}

func TestCachedHitsAndExpiry(t *testing.T) {
	inner := &fakeForecaster{}
	c := NewCached(inner, time.Hour)
	now := day0
	c.Now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := c.Forecast(ctx, 41.981, -87.902, "UTC", day0, 3); err != nil {
		t.Fatal(err)
	}
	// Same 2 dp coordinates share an entry; the copy is independent.
	got, _ := c.Forecast(ctx, 41.984, -87.899, "UTC", day0, 3)
	got[0].Code = 99
	again, _ := c.Forecast(ctx, 41.98, -87.9, "UTC", day0, 3)
	if inner.calls != 1 || again[0].Code != 1 {
		t.Fatalf("calls=%d code=%d", inner.calls, again[0].Code)
	}
	// A different day count or time zone is a different key.
	_, _ = c.Forecast(ctx, 41.98, -87.9, "UTC", day0, 4)
	_, _ = c.Forecast(ctx, 41.98, -87.9, "Europe/London", day0, 3)
	if inner.calls != 3 {
		t.Fatalf("calls = %d, want 3", inner.calls)
	}
	now = now.Add(2 * time.Hour)
	_, _ = c.Forecast(ctx, 41.98, -87.9, "UTC", day0, 3)
	if inner.calls != 4 {
		t.Fatalf("calls after expiry = %d, want 4", inner.calls)
	}
}

func TestCachedDoesNotCacheErrors(t *testing.T) {
	inner := &fakeForecaster{err: errors.New("down")}
	c := NewCached(inner, 0)
	if c.TTL != time.Hour {
		t.Errorf("default TTL = %v", c.TTL)
	}
	for i := 0; i < 2; i++ {
		if _, err := c.Forecast(context.Background(), 1, 2, "UTC", day0, 1); err == nil {
			t.Fatal("expected error")
		}
	}
	if inner.calls != 2 {
		t.Errorf("calls = %d, want 2", inner.calls)
	}
}

func TestCachedClearsWhenFull(t *testing.T) {
	inner := &fakeForecaster{}
	c := NewCached(inner, time.Hour)
	for i := 0; i <= maxCacheEntries; i++ {
		_, _ = c.Forecast(context.Background(), float64(i)/10, 0, "UTC", day0, 1)
	}
	if len(c.buf) > maxCacheEntries || len(c.buf) == 0 {
		t.Errorf("cache size = %d", len(c.buf))
	}
}

func TestDescribe(t *testing.T) {
	cases := map[int]string{
		0: "Clear sky", 1: "Mostly clear", 2: "Partly cloudy", 3: "Overcast",
		45: "Fog", 48: "Fog", 53: "Drizzle", 56: "Freezing drizzle", 63: "Rain",
		67: "Freezing rain", 73: "Snow", 81: "Rain showers", 85: "Snow showers",
		95: "Thunderstorm", 99: "Thunderstorm", 1234: "Unsettled",
	}
	for code, want := range cases {
		text, emoji := Describe(code)
		if text != want || emoji == "" {
			t.Errorf("Describe(%d) = %q, %q; want %q", code, text, emoji, want)
		}
	}
}

func TestCachedStructLiteralIsSafe(t *testing.T) {
	inner := &fakeForecaster{}
	c := &Cached{Inner: inner, TTL: time.Hour} // no Now, no map
	for i := 0; i < 2; i++ {
		if _, err := c.Forecast(context.Background(), 1, 2, "UTC", day0, 1); err != nil {
			t.Fatal(err)
		}
	}
	if inner.calls != 1 {
		t.Errorf("calls = %d, want 1 (second call served from the cache)", inner.calls)
	}
}

func TestTruncateKeepsRunesWhole(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("short string changed: %q", got)
	}
	// "é" is two bytes; cutting at 3 would split the second one.
	if got := truncate("aéé", 4); got != "aé..." {
		t.Errorf("got %q, want %q", got, "aé...")
	}
	if got := truncate(strings.Repeat("é", 300), maxErrBody); !utf8.ValidString(got) {
		t.Errorf("truncated body is not valid UTF-8: %q", got)
	}
}
