package api

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

func TestTripSpanInItemZones(t *testing.T) {
	es := time.Date(2026, 10, 17, 20, 0, 0, 0, time.UTC)  // 18 Oct 01:00 in Tashkent
	ee := time.Date(2026, 10, 25, 23, 30, 0, 0, time.UTC) // 25 Oct 19:30 in New York
	d := ToTripDTO(&store.Trip{
		ID: 3, Name: "Test Trip", EffectiveStart: &es, EffectiveEnd: &ee,
		FirstTZ: "Asia/Tashkent", LastTZ: "America/New_York",
	}, "owner", nil, nil)
	if *d.EffectiveStart != "2026-10-18" || *d.EffectiveEnd != "2026-10-25" {
		t.Errorf("effective = %s .. %s", *d.EffectiveStart, *d.EffectiveEnd)
	}
	if !d.SpanStart.Equal(time.Date(2026, 10, 17, 19, 0, 0, 0, time.UTC)) {
		t.Errorf("span_start = %v", d.SpanStart)
	}
	// Midnight after 25 Oct in New York, still on daylight time (UTC-4).
	if !d.SpanEnd.Equal(time.Date(2026, 10, 26, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("span_end = %v", d.SpanEnd)
	}
}

func TestTripSpanFromExplicitDates(t *testing.T) {
	so := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	eo := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	d := ToTripDTO(&store.Trip{ID: 4, StartsOn: &so, EndsOn: &eo, FirstTZ: "Europe/London"}, "owner", nil, nil)
	if !d.SpanStart.Equal(time.Date(2026, 6, 30, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("span_start = %v", d.SpanStart)
	}
	// No last zone: the end date reads in UTC.
	if !d.SpanEnd.Equal(time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("span_end = %v", d.SpanEnd)
	}
}

func TestTripSpanEmptyAndBadZone(t *testing.T) {
	d := ToTripDTO(&store.Trip{ID: 5}, "owner", nil, nil)
	if d.SpanStart != nil || d.SpanEnd != nil {
		t.Errorf("dateless span = %v .. %v", d.SpanStart, d.SpanEnd)
	}
	if tripZone("Mars/Olympus") != time.UTC || tripZone("") != time.UTC || tripZone("Local") != time.UTC {
		t.Error("tripZone should fall back to UTC")
	}
}

func TestTripSpanOneSided(t *testing.T) {
	so := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	d := ToTripDTO(&store.Trip{ID: 6, StartsOn: &so, FirstTZ: "Europe/London"}, "owner", nil, nil)
	if d.SpanStart == nil || !d.SpanStart.Equal(time.Date(2026, 6, 30, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("span_start = %v", d.SpanStart)
	}
	if d.SpanEnd != nil {
		t.Errorf("span_end = %v, want none without an end date", d.SpanEnd)
	}

	ee := time.Date(2026, 7, 8, 22, 0, 0, 0, time.UTC) // 9 Jul 00:00 in Paris
	d = ToTripDTO(&store.Trip{ID: 7, EffectiveEnd: &ee, LastTZ: "Europe/Paris"}, "owner", nil, nil)
	if d.SpanStart != nil {
		t.Errorf("span_start = %v, want none without a start", d.SpanStart)
	}
	// Midnight ending 9 Jul in Paris (UTC+2).
	if d.SpanEnd == nil || !d.SpanEnd.Equal(time.Date(2026, 7, 9, 22, 0, 0, 0, time.UTC)) {
		t.Errorf("span_end = %v", d.SpanEnd)
	}
}
