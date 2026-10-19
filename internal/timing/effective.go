package timing

import "time"

// Kind says where an effective time came from.
type Kind string

const (
	Actual    Kind = "actual"    // observed: wheels-off or touchdown
	Estimated Kind = "estimated" // the provider's revised time
	Derived   Kind = "derived"   // the timetabled arrival moved by the departure delay
	Scheduled Kind = "scheduled" // the timetable
)

// FlightTimes is a flight's timetable and provider times. EstimatedOut is the
// revised off-block time, ActualOut wheels-off, EstimatedIn the revised
// arrival and ActualIn touchdown. A zero ScheduledIn, or one not after
// ScheduledOut, means there is no timetabled arrival.
type FlightTimes struct {
	ScheduledOut, ScheduledIn time.Time
	EstimatedOut, EstimatedIn *time.Time
	ActualOut, ActualIn       *time.Time
}

// End is one end's effective time. Delay is measured against the timetable
// and is zero when there is nothing to measure against.
type End struct {
	At    time.Time
	Kind  Kind
	Delay time.Duration
}

// Times is a flight's effective departure and arrival. Arrival is nil when no
// arrival time is known at all. Revised reports an off-block estimate, the only
// source of a departure delay. DroppedEstimatedIn reports a provider arrival
// estimate discarded for falling at or before the departure.
type Times struct {
	Departure          End
	Arrival            *End
	Revised            bool
	DroppedEstimatedIn bool
}

// Duration is the effective arrival minus the effective departure.
func (t Times) Duration() (time.Duration, bool) {
	if t.Arrival == nil {
		return 0, false
	}
	return t.Arrival.At.Sub(t.Departure.At), true
}

// Effective applies the one rule for a flight's times: actual, then estimated,
// then (arrival only) the timetable moved by the departure delay, then the
// timetable. The departure delay is always the off-block estimate's, never
// wheels-off, which would count taxi time as delay.
func Effective(f FlightTimes) Times {
	var t Times
	var delay time.Duration
	if f.EstimatedOut != nil {
		t.Revised = true
		delay = f.EstimatedOut.Sub(f.ScheduledOut)
	}
	switch {
	case f.ActualOut != nil:
		t.Departure = End{At: *f.ActualOut, Kind: Actual, Delay: delay}
	case f.EstimatedOut != nil:
		t.Departure = End{At: *f.EstimatedOut, Kind: Estimated, Delay: delay}
	default:
		t.Departure = End{At: f.ScheduledOut, Kind: Scheduled}
	}

	timetabled := f.ScheduledIn.After(f.ScheduledOut)
	against := func(at time.Time) time.Duration {
		if !timetabled {
			return 0
		}
		return at.Sub(f.ScheduledIn)
	}
	estIn := f.EstimatedIn
	// Once the flight has landed the estimate is moot, so it is not reported.
	if f.ActualIn == nil && estIn != nil && !estIn.After(t.Departure.At) {
		estIn = nil
		t.DroppedEstimatedIn = true
	}
	switch {
	case f.ActualIn != nil:
		t.Arrival = &End{At: *f.ActualIn, Kind: Actual, Delay: against(*f.ActualIn)}
	case estIn != nil:
		t.Arrival = &End{At: *estIn, Kind: Estimated, Delay: against(*estIn)}
	case timetabled && delay != 0:
		t.Arrival = &End{At: f.ScheduledIn.Add(delay), Kind: Derived, Delay: delay}
	case timetabled:
		t.Arrival = &End{At: f.ScheduledIn, Kind: Scheduled}
	}
	return t
}
