package timing

import (
	"testing"
	"time"
)

func TestEffective(t *testing.T) {
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := out.Add(2 * time.Hour)

	tests := []struct {
		name string
		f    FlightTimes
		want Times
	}{
		{
			name: "timetable only",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
			},
			want: Times{
				Departure:          End{At: out, Kind: Scheduled, Delay: 0},
				Arrival:            &End{At: in, Kind: Scheduled, Delay: 0},
				Revised:            false,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "landed with a stale estimate before the departure",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedIn:  ptrTime(out.Add(-time.Minute)),
				ActualIn:     ptrTime(in),
			},
			want: Times{
				Departure: End{At: out, Kind: Scheduled},
				Arrival:   &End{At: in, Kind: Actual},
			},
		},
		{
			name: "off-block estimate out+40m",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedOut: ptrTime(out.Add(40 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out.Add(40 * time.Minute), Kind: Estimated, Delay: 40 * time.Minute},
				Arrival:            &End{At: in.Add(40 * time.Minute), Kind: Derived, Delay: 40 * time.Minute},
				Revised:            true,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "estimate out+40m and actual wheels-off out+55m",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedOut: ptrTime(out.Add(40 * time.Minute)),
				ActualOut:    ptrTime(out.Add(55 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out.Add(55 * time.Minute), Kind: Actual, Delay: 40 * time.Minute},
				Arrival:            &End{At: in.Add(40 * time.Minute), Kind: Derived, Delay: 40 * time.Minute},
				Revised:            true,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "actual wheels-off out+15m, no estimate",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				ActualOut:    ptrTime(out.Add(15 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out.Add(15 * time.Minute), Kind: Actual, Delay: 0},
				Arrival:            &End{At: in, Kind: Scheduled, Delay: 0},
				Revised:            false,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "estimated arrival in+10m",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedIn:  ptrTime(in.Add(10 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out, Kind: Scheduled, Delay: 0},
				Arrival:            &End{At: in.Add(10 * time.Minute), Kind: Estimated, Delay: 10 * time.Minute},
				Revised:            false,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "actual arrival in−5m and estimate in+10m",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedIn:  ptrTime(in.Add(10 * time.Minute)),
				ActualIn:     ptrTime(in.Add(-5 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out, Kind: Scheduled, Delay: 0},
				Arrival:            &End{At: in.Add(-5 * time.Minute), Kind: Actual, Delay: -5 * time.Minute},
				Revised:            false,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "estimated arrival out−1m (before the departure), dropped",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedIn:  ptrTime(out.Add(-1 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out, Kind: Scheduled, Delay: 0},
				Arrival:            &End{At: in, Kind: Scheduled, Delay: 0},
				Revised:            false,
				DroppedEstimatedIn: true,
			},
		},
		{
			name: "estimated arrival before departure with off-block estimate out+30m",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedOut: ptrTime(out.Add(30 * time.Minute)),
				EstimatedIn:  ptrTime(out.Add(-1 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out.Add(30 * time.Minute), Kind: Estimated, Delay: 30 * time.Minute},
				Arrival:            &End{At: in.Add(30 * time.Minute), Kind: Derived, Delay: 30 * time.Minute},
				Revised:            true,
				DroppedEstimatedIn: true,
			},
		},
		{
			name: "departure-only (ScheduledIn == ScheduledOut)",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  out,
			},
			want: Times{
				Departure:          End{At: out, Kind: Scheduled, Delay: 0},
				Arrival:            nil,
				Revised:            false,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "departure-only with estimated arrival out+3h",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  out,
				EstimatedIn:  ptrTime(out.Add(3 * time.Hour)),
			},
			want: Times{
				Departure:          End{At: out, Kind: Scheduled, Delay: 0},
				Arrival:            &End{At: out.Add(3 * time.Hour), Kind: Estimated, Delay: 0},
				Revised:            false,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "zero ScheduledIn",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  time.Time{},
			},
			want: Times{
				Departure:          End{At: out, Kind: Scheduled, Delay: 0},
				Arrival:            nil,
				Revised:            false,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "early off-block estimate out−10m",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedOut: ptrTime(out.Add(-10 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out.Add(-10 * time.Minute), Kind: Estimated, Delay: -10 * time.Minute},
				Arrival:            &End{At: in.Add(-10 * time.Minute), Kind: Derived, Delay: -10 * time.Minute},
				Revised:            true,
				DroppedEstimatedIn: false,
			},
		},
		{
			name: "Duration for case with 2h flight",
			f: FlightTimes{
				ScheduledOut: out,
				ScheduledIn:  in,
				EstimatedOut: ptrTime(out.Add(40 * time.Minute)),
			},
			want: Times{
				Departure:          End{At: out.Add(40 * time.Minute), Kind: Estimated, Delay: 40 * time.Minute},
				Arrival:            &End{At: in.Add(40 * time.Minute), Kind: Derived, Delay: 40 * time.Minute},
				Revised:            true,
				DroppedEstimatedIn: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Effective(tt.f)

			// Compare Departure
			if got.Departure.At != tt.want.Departure.At {
				t.Errorf("Departure.At = %v, want %v", got.Departure.At, tt.want.Departure.At)
			}
			if got.Departure.Kind != tt.want.Departure.Kind {
				t.Errorf("Departure.Kind = %v, want %v", got.Departure.Kind, tt.want.Departure.Kind)
			}
			if got.Departure.Delay != tt.want.Departure.Delay {
				t.Errorf("Departure.Delay = %v, want %v", got.Departure.Delay, tt.want.Departure.Delay)
			}

			// Compare Arrival
			if (got.Arrival == nil) != (tt.want.Arrival == nil) {
				t.Errorf("Arrival nil mismatch: got %v, want %v", got.Arrival, tt.want.Arrival)
			}
			if got.Arrival != nil && tt.want.Arrival != nil {
				if got.Arrival.At != tt.want.Arrival.At {
					t.Errorf("Arrival.At = %v, want %v", got.Arrival.At, tt.want.Arrival.At)
				}
				if got.Arrival.Kind != tt.want.Arrival.Kind {
					t.Errorf("Arrival.Kind = %v, want %v", got.Arrival.Kind, tt.want.Arrival.Kind)
				}
				if got.Arrival.Delay != tt.want.Arrival.Delay {
					t.Errorf("Arrival.Delay = %v, want %v", got.Arrival.Delay, tt.want.Arrival.Delay)
				}
			}

			// Compare Revised and DroppedEstimatedIn
			if got.Revised != tt.want.Revised {
				t.Errorf("Revised = %v, want %v", got.Revised, tt.want.Revised)
			}
			if got.DroppedEstimatedIn != tt.want.DroppedEstimatedIn {
				t.Errorf("DroppedEstimatedIn = %v, want %v", got.DroppedEstimatedIn, tt.want.DroppedEstimatedIn)
			}

			// For test case 13, also check Duration
			if tt.name == "Duration for case with 2h flight" {
				dur, ok := got.Duration()
				if !ok {
					t.Errorf("Duration() ok = %v, want true", ok)
				}
				if dur != 2*time.Hour {
					t.Errorf("Duration() = %v, want %v", dur, 2*time.Hour)
				}
			}

			// For departure-only case, check Duration returns false
			if tt.name == "departure-only (ScheduledIn == ScheduledOut)" {
				_, ok := got.Duration()
				if ok {
					t.Errorf("Duration() ok = %v, want false for nil Arrival", ok)
				}
			}
		})
	}
}

// Helper function to create a pointer to time.Time
func ptrTime(t time.Time) *time.Time {
	return &t
}

// Test Duration method
func TestDuration(t *testing.T) {
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := out.Add(2 * time.Hour)

	tests := []struct {
		name    string
		times   Times
		wantDur time.Duration
		wantOk  bool
	}{
		{
			name: "with arrival",
			times: Times{
				Departure: End{At: out, Kind: Scheduled},
				Arrival:   &End{At: in, Kind: Scheduled},
			},
			wantDur: 2 * time.Hour,
			wantOk:  true,
		},
		{
			name: "without arrival",
			times: Times{
				Departure: End{At: out, Kind: Scheduled},
				Arrival:   nil,
			},
			wantDur: 0,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dur, ok := tt.times.Duration()
			if ok != tt.wantOk {
				t.Errorf("Duration() ok = %v, want %v", ok, tt.wantOk)
			}
			if dur != tt.wantDur {
				t.Errorf("Duration() = %v, want %v", dur, tt.wantDur)
			}
		})
	}
}

// A floating departure's timetable is wall-clock digits, so a real estimate
// must not be measured against it: no delay, no derived arrival, but the
// estimate is still returned as the instant it is.
func TestEffectiveFloatingDeparture(t *testing.T) {
	out := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	in := out.Add(2 * time.Hour)
	est := out.Add(-5 * time.Hour) // a real instant, nothing like the digits
	got := Effective(FlightTimes{ScheduledOut: out, ScheduledIn: in, EstimatedOut: &est, StartSource: Floating})
	if got.Revised || got.Departure.Delay != 0 || got.Departure.Kind != Estimated || !got.Departure.At.Equal(est) {
		t.Errorf("departure = %+v revised=%v", got.Departure, got.Revised)
	}
	if got.Arrival == nil || got.Arrival.Kind != Scheduled || !got.Arrival.At.Equal(in) {
		t.Errorf("arrival = %+v, want the timetable and no derived time", got.Arrival)
	}

	estIn := in.Add(time.Hour)
	got = Effective(FlightTimes{ScheduledOut: out, ScheduledIn: in, EstimatedIn: &estIn, StartSource: Floating})
	if got.Arrival == nil || got.Arrival.Kind != Estimated || got.Arrival.Delay != 0 {
		t.Errorf("estimated arrival = %+v, want delay 0", got.Arrival)
	}

	// The zero source is not floating, so the delay is still measured.
	got = Effective(FlightTimes{ScheduledOut: out, ScheduledIn: in, EstimatedOut: &est})
	if !got.Revised || got.Departure.Delay != -5*time.Hour {
		t.Errorf("non-floating = %+v", got.Departure)
	}
}
