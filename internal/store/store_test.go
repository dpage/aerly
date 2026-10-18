package store

import (
	"testing"
	"time"

	"github.com/dpage/aerly/internal/timing"
)

// TestFlightTimes: Times copies the six time fields and nothing else.
func TestFlightTimes(t *testing.T) {
	out := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	in := out.Add(9 * time.Hour)
	estOut, actOut := out.Add(20*time.Minute), out.Add(25*time.Minute)
	estIn, actIn := in.Add(15*time.Minute), in.Add(10*time.Minute)
	f := &Flight{Ident: "XX100", ScheduledOut: out, ScheduledIn: in,
		EstimatedOut: &estOut, EstimatedIn: &estIn, ActualOut: &actOut, ActualIn: &actIn}

	want := timing.FlightTimes{ScheduledOut: out, ScheduledIn: in,
		EstimatedOut: &estOut, EstimatedIn: &estIn, ActualOut: &actOut, ActualIn: &actIn}
	if got := f.Times(); got != want {
		t.Errorf("Times() = %+v, want %+v", got, want)
	}
}
