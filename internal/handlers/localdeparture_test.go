package handlers

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// localDeparture renders the departure in the origin airport's zone, the date
// the provider matches on, and falls back to UTC for an unknown airport.
func TestLocalDeparture(t *testing.T) {
	// 23:30Z on 1 June is 00:30 BST on 2 June at Heathrow.
	out := time.Date(2026, 6, 1, 23, 30, 0, 0, time.UTC)
	if got := localDeparture(&store.FlightDetail{OriginIATA: "LHR", ScheduledOut: out}); got.Format("2006-01-02") != "2026-06-02" || !got.Equal(out) {
		t.Errorf("LHR: got %v, want the same instant on 2026-06-02", got)
	}
	if got := localDeparture(&store.FlightDetail{OriginIATA: "ZZZ", ScheduledOut: out}); got.Location() != time.UTC || !got.Equal(out) {
		t.Errorf("unknown airport: got %v, want UTC", got)
	}
}
