package handlers

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// localDeparture renders the departure in the origin airport's zone, the date
// the provider matches on, and reports not-known for an unknown airport.
func TestLocalDeparture(t *testing.T) {
	// 23:30Z on 1 June is 00:30 BST on 2 June at Heathrow.
	out := time.Date(2026, 6, 1, 23, 30, 0, 0, time.UTC)
	got, ok := localDeparture(&store.FlightDetail{OriginIATA: "LHR", ScheduledOut: out})
	if !ok || got.Format("2006-01-02") != "2026-06-02" || !got.Equal(out) {
		t.Errorf("LHR: got %v, %v, want the same instant on 2026-06-02", got, ok)
	}
	if got, ok := localDeparture(&store.FlightDetail{OriginIATA: "ZZZ", ScheduledOut: out}); ok || !got.IsZero() {
		t.Errorf("unknown airport: got %v, %v, want not known", got, ok)
	}
}
