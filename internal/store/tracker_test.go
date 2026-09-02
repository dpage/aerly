package store

import (
	"testing"
	"time"
)

// TestActiveFlightParts: only non-terminal flight parts at/after departure are
// returned, keyed on plan_part_id, mirroring the legacy ActiveFlights filter.
func TestActiveFlightParts(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// Active: departed an hour ago, arrives in an hour, Enroute.
	active := mkFlightPartInTrip(t, s, trip, owner, "ACT1",
		now.Add(-time.Hour), now.Add(time.Hour), "Enroute", 51.47, -0.46, 40.64, -73.78)
	// Terminal: Arrived → excluded.
	mkFlightPartInTrip(t, s, trip, owner, "DONE1",
		now.Add(-3*time.Hour), now.Add(-time.Hour), "Arrived", 51.47, -0.46, 40.64, -73.78)
	// Far future: departs in 2 days → excluded by the departure bound.
	mkFlightPartInTrip(t, s, trip, owner, "FUT1",
		now.Add(48*time.Hour), now.Add(50*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)

	parts, err := s.ActiveFlightParts(ctx, now)
	if err != nil {
		t.Fatalf("ActiveFlightParts: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != active {
		t.Fatalf("expected only the active part %d, got %d: %+v", active, len(parts), parts)
	}
	// The carrier must be populated for the providers: id is the plan_part_id,
	// coords from the part, schedule/status from flight_details.
	got := parts[0]
	if got.Ident != "ACT1" || got.Status != "Enroute" || got.OriginIATA != "LHR" {
		t.Errorf("carrier not populated from join: %+v", got)
	}
	if got.OriginLat == nil || got.DestLat == nil {
		t.Errorf("coords should come from the plan_part: %+v", got)
	}
}

// TestRefreshStatusUnknownArrival guards the "Arrived before takeoff" bug: a
// manually-entered flight with no arrival time stores scheduled_in ==
// scheduled_out, so deriving Arrived from "now > scheduled_in" flips it to
// Arrived a minute after its scheduled departure even though it never landed.
// Status must only become Arrived when a real arrival time exists
// (scheduled_in strictly after scheduled_out).
func TestRefreshStatusUnknownArrival(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// No arrival time → scheduled_in == scheduled_out, half an hour past now.
	degenerate := mkFlightPart(t, s, owner, "VL1939", now.Add(-30*time.Minute), now.Add(-30*time.Minute))
	if err := s.RefreshFlightPartStatus(ctx, degenerate); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	df, _ := s.FlightPartByID(ctx, degenerate)
	if df.Status == "Arrived" {
		t.Errorf("a flight with no real arrival time must not be Arrived, got %q", df.Status)
	}
	if df.Status != "Enroute" {
		t.Errorf("past-departure flight with unknown arrival should be Enroute, got %q", df.Status)
	}

	// Control: a genuine flight (arrival strictly after departure, both past)
	// still derives Arrived.
	real := mkFlightPart(t, s, owner, "BA286", now.Add(-3*time.Hour), now.Add(-time.Hour))
	if err := s.RefreshFlightPartStatus(ctx, real); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	rf, _ := s.FlightPartByID(ctx, real)
	if rf.Status != "Arrived" {
		t.Errorf("a genuinely-landed flight should still be Arrived, got %q", rf.Status)
	}
}

// TestRefreshStatusLateArrival: a flight whose arrival has been revised later
// than the timetable must stay Enroute past its scheduled arrival, because
// otherwise it is declared Arrived — and so dropped from the poll set — whilst
// it is still in the air. Once the revised time itself passes, it arrives.
func TestRefreshStatusLateArrival(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// Timetabled to land an hour ago, revised to land in an hour: still flying.
	late := mkFlightPart(t, s, owner, "OS967", now.Add(-4*time.Hour), now.Add(-time.Hour))
	revised := now.Add(time.Hour)
	if err := s.RefreshFlightPartArrival(ctx, late, &revised, nil); err != nil {
		t.Fatalf("RefreshFlightPartArrival: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, late); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	lf, _ := s.FlightPartByID(ctx, late)
	if lf.Status != "Enroute" {
		t.Errorf("a flight revised an hour late should still be Enroute, got %q", lf.Status)
	}
	// The point of staying Enroute: the poller keeps seeing it, so the belt and
	// any further revision still reach the traveller.
	active, err := s.ActiveFlightParts(ctx, now)
	if err != nil {
		t.Fatalf("ActiveFlightParts: %v", err)
	}
	var found bool
	for _, p := range active {
		if p.ID == late {
			found = true
		}
	}
	if !found {
		t.Error("a late flight dropped out of the poll set whilst still airborne")
	}

	// An observed touchdown beats the estimate, even one still in the future.
	landed := now.Add(-10 * time.Minute)
	if err := s.RefreshFlightPartArrival(ctx, late, nil, &landed); err != nil {
		t.Fatalf("RefreshFlightPartArrival: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, late); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	lf, _ = s.FlightPartByID(ctx, late)
	if lf.Status != "Arrived" {
		t.Errorf("an observed touchdown should give Arrived, got %q", lf.Status)
	}
}

// TestRefreshStatusStaleRevisionCapped: a revision the provider quoted and then
// stopped updating must not hold a part non-terminal (and so in the poll set)
// forever. Beyond arrivalSlipCap past the timetable, the part arrives anyway.
func TestRefreshStatusStaleRevisionCapped(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// Landed by the timetable two days ago, with a revision quoting an arrival
	// a day after that — well beyond the cap, and never updated since.
	stale := mkFlightPart(t, s, owner, "OS967", now.Add(-50*time.Hour), now.Add(-48*time.Hour))
	revised := now.Add(-24 * time.Hour)
	if err := s.RefreshFlightPartArrival(ctx, stale, &revised, nil); err != nil {
		t.Fatalf("RefreshFlightPartArrival: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, stale); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	sf, _ := s.FlightPartByID(ctx, stale)
	if sf.Status != "Arrived" {
		t.Errorf("a stale revision should not hold a part open, got %q", sf.Status)
	}
}

// TestTrackerPartTitlePerLeg guards the mislabelled-return-leg bug: a round
// trip is one plan holding two legs, and its title names only the outbound
// flight ("OS962 ARN ↔ VIE"), so titling every leg from the plan captions the
// homebound flight with the outbound's number. Each leg of a multi-leg plan is
// titled by its own flight number; a single-leg plan still shows its title, so
// a hand-edited one is not thrown away.
func TestTrackerPartTitlePerLeg(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	mkLeg := func(planID int64, ident string, out, in time.Time) int64 {
		t.Helper()
		var partID int64
		if err := s.pool.QueryRow(ctx, `
			INSERT INTO plan_parts (plan_id, starts_at, ends_at, status)
			VALUES ($1, $2, $3, 'confirmed') RETURNING id`,
			planID, out, in).Scan(&partID); err != nil {
			t.Fatalf("insert leg: %v", err)
		}
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in,
				origin_iata, dest_iata, flight_status)
			VALUES ($1, $2, $3, $4, 'ARN', 'VIE', 'Scheduled')`,
			partID, ident, out, in); err != nil {
			t.Fatalf("insert flight_details: %v", err)
		}
		return partID
	}
	mkPlan := func(title string) int64 {
		t.Helper()
		var planID int64
		if err := s.pool.QueryRow(ctx,
			`INSERT INTO plans (trip_id, type, created_by, title) VALUES ($1, 'flight', $2, $3) RETURNING id`,
			trip, owner, title).Scan(&planID); err != nil {
			t.Fatalf("insert plan: %v", err)
		}
		return planID
	}

	round := mkPlan("OS962 ARN ↔ VIE")
	outbound := mkLeg(round, "OS962", now.Add(-48*time.Hour), now.Add(-46*time.Hour))
	homebound := mkLeg(round, "OS967", now.Add(-time.Hour), now.Add(time.Hour))

	for _, tc := range []struct {
		part int64
		want string
	}{
		{outbound, "OS962"},
		{homebound, "OS967"},
	} {
		got, err := s.TrackerPartRow(ctx, tc.part)
		if err != nil {
			t.Fatalf("TrackerPartRow(%d): %v", tc.part, err)
		}
		if got.Title != tc.want {
			t.Errorf("leg %d titled %q, want its own flight number %q", tc.part, got.Title, tc.want)
		}
	}

	// A single-leg plan keeps its title, hand-edited or otherwise.
	single := mkPlan("Flight home for the wedding")
	solo := mkLeg(single, "BA286", now.Add(-time.Hour), now.Add(time.Hour))
	got, err := s.TrackerPartRow(ctx, solo)
	if err != nil {
		t.Fatalf("TrackerPartRow(%d): %v", solo, err)
	}
	if got.Title != "Flight home for the wedding" {
		t.Errorf("single-leg plan title = %q, want the plan's own title", got.Title)
	}

	// An untitled single-leg plan still falls back to the flight number.
	bare := mkPlan("")
	bareLeg := mkLeg(bare, "LH493", now.Add(-time.Hour), now.Add(time.Hour))
	got, err = s.TrackerPartRow(ctx, bareLeg)
	if err != nil {
		t.Fatalf("TrackerPartRow(%d): %v", bareLeg, err)
	}
	if got.Title != "LH493" {
		t.Errorf("untitled plan title = %q, want the flight number", got.Title)
	}
}

// TestSetFlightPartTerminalStatus: a cancellation cannot be derived from times,
// so the provider's verdict is written directly and then survives the derived
// refresh. Without this a cancelled flight keeps being judged on its timetable
// and is declared Arrived the moment its estimated arrival passes, which tells
// the traveller their cancelled flight landed.
func TestSetFlightPartTerminalStatus(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// Timetabled out two hours ago, in an hour ago: the derivation would have
	// this Arrived on the next refresh.
	part := mkFlightPart(t, s, owner, "ZZ967", now.Add(-2*time.Hour), now.Add(-time.Hour))
	if err := s.SetFlightPartTerminalStatus(ctx, part, "Cancelled"); err != nil {
		t.Fatalf("SetFlightPartTerminalStatus: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, part); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	got, _ := s.FlightPartByID(ctx, part)
	if got.Status != "Cancelled" {
		t.Errorf("a cancelled flight was re-derived to %q", got.Status)
	}

	// A flight with an observed touchdown is never contradicted: an aircraft
	// that demonstrably landed cannot be cancelled.
	landed := mkFlightPart(t, s, owner, "ZZ962", now.Add(-5*time.Hour), now.Add(-3*time.Hour))
	touchdown := now.Add(-3 * time.Hour)
	if err := s.RefreshFlightPartArrival(ctx, landed, nil, &touchdown); err != nil {
		t.Fatalf("RefreshFlightPartArrival: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, landed); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	if err := s.SetFlightPartTerminalStatus(ctx, landed, "Cancelled"); err != nil {
		t.Fatalf("SetFlightPartTerminalStatus (landed): %v", err)
	}
	lf, _ := s.FlightPartByID(ctx, landed)
	if lf.Status != "Arrived" {
		t.Errorf("a flight that demonstrably landed was cancelled: %q", lf.Status)
	}

	// But an Arrived that was only ever derived from an estimate IS
	// correctable, which is the whole point: a cancellation published after
	// that derivation ran must not leave the flight reported as landed.
	derived := mkFlightPart(t, s, owner, "ZZ963", now.Add(-5*time.Hour), now.Add(-3*time.Hour))
	if err := s.RefreshFlightPartStatus(ctx, derived); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	if df, _ := s.FlightPartByID(ctx, derived); df.Status != "Arrived" {
		t.Fatalf("precondition: expected a derived Arrived, got %q", df.Status)
	}
	if err := s.SetFlightPartTerminalStatus(ctx, derived, "Cancelled"); err != nil {
		t.Fatalf("SetFlightPartTerminalStatus (derived): %v", err)
	}
	if df, _ := s.FlightPartByID(ctx, derived); df.Status != "Cancelled" {
		t.Errorf("a derived Arrived was not corrected to Cancelled, got %q", df.Status)
	}

	// Only the two terminal statuses are accepted; anything else is the
	// derivation's business and is refused rather than silently written.
	if err := s.SetFlightPartTerminalStatus(ctx, part, "Enroute"); err == nil {
		t.Error("a derived status was accepted as a terminal one")
	}
}

// TestRefreshStatusHeldOnStand is the departure-side mirror of
// TestRefreshStatusLateArrival, and guards the "airborne in a thunderstorm"
// bug: deriving Enroute from the timetable declares a flight in the air the
// moment its scheduled departure passes, however long it actually sits at the
// gate — so a flight held through a rolling weather delay reads as airborne
// for as long as the delay runs.
func TestRefreshStatusHeldOnStand(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// Timetabled out an hour ago, revised to leave in half an hour: still on
	// stand, and the timetabled arrival is still ahead so no Arrived arm bites.
	held := mkFlightPart(t, s, owner, "OS967", now.Add(-time.Hour), now.Add(time.Hour))
	revised := now.Add(30 * time.Minute)
	if err := s.RefreshFlightPartDeparture(ctx, held, &revised, nil); err != nil {
		t.Fatalf("RefreshFlightPartDeparture: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, held); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	hf, _ := s.FlightPartByID(ctx, held)
	if hf.Status != "Scheduled" {
		t.Errorf("a flight still on stand should be Scheduled, got %q", hf.Status)
	}

	// An observed wheels-off settles it, even whilst the revised off-block time
	// it beat is still in the future.
	off := now.Add(-5 * time.Minute)
	if err := s.RefreshFlightPartDeparture(ctx, held, nil, &off); err != nil {
		t.Fatalf("RefreshFlightPartDeparture (wheels-off): %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, held); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	hf, _ = s.FlightPartByID(ctx, held)
	if hf.Status != "Enroute" {
		t.Errorf("an observed wheels-off should give Enroute, got %q", hf.Status)
	}

	// Control: with no live departure times at all the expression collapses to
	// the timetable, which is exactly the behaviour it replaced.
	plain := mkFlightPart(t, s, owner, "OS962", now.Add(-time.Hour), now.Add(time.Hour))
	if err := s.RefreshFlightPartStatus(ctx, plain); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	pf, _ := s.FlightPartByID(ctx, plain)
	if pf.Status != "Enroute" {
		t.Errorf("a timetable-only flight past its departure should be Enroute, got %q", pf.Status)
	}
}

// TestRefreshDepartureKeepsKnownTimes: the departure mirror of
// TestRefreshArrivalKeepsKnownTimes — a poll that comes back without live
// coverage must leave an earlier revision standing rather than blanking it.
func TestRefreshDepartureKeepsKnownTimes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	part := mkFlightPart(t, s, owner, "OS962", now.Add(-3*time.Hour), now.Add(-time.Hour))

	est, act := now.Add(-170*time.Minute), now.Add(-165*time.Minute)
	if err := s.RefreshFlightPartDeparture(ctx, part, &est, &act); err != nil {
		t.Fatalf("RefreshFlightPartDeparture: %v", err)
	}
	if err := s.RefreshFlightPartDeparture(ctx, part, nil, nil); err != nil {
		t.Fatalf("RefreshFlightPartDeparture (omitted): %v", err)
	}
	got, _ := s.FlightPartByID(ctx, part)
	if got.EstimatedOut == nil || !got.EstimatedOut.Truncate(time.Second).Equal(est.UTC().Truncate(time.Second)) {
		t.Errorf("estimated_out wiped by an omission: %v", got.EstimatedOut)
	}
	if got.ActualOut == nil || !got.ActualOut.Truncate(time.Second).Equal(act.UTC().Truncate(time.Second)) {
		t.Errorf("actual_out wiped by an omission: %v", got.ActualOut)
	}
}

// TestRefreshArrivalKeepsKnownTimes: the resolver omits the live times until it
// has coverage, and an omission (nil) must leave a time we already know alone
// rather than wiping it — the same overwrite-when-supplied rule the belt uses.
func TestRefreshArrivalKeepsKnownTimes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	part := mkFlightPart(t, s, owner, "OS962", now.Add(-3*time.Hour), now.Add(-time.Hour))

	est, act := now.Add(-70*time.Minute), now.Add(-75*time.Minute)
	if err := s.RefreshFlightPartArrival(ctx, part, &est, &act); err != nil {
		t.Fatalf("RefreshFlightPartArrival: %v", err)
	}
	if err := s.RefreshFlightPartArrival(ctx, part, nil, nil); err != nil {
		t.Fatalf("RefreshFlightPartArrival (omitted): %v", err)
	}
	got, _ := s.FlightPartByID(ctx, part)
	if got.EstimatedIn == nil || !got.EstimatedIn.Truncate(time.Second).Equal(est.UTC().Truncate(time.Second)) {
		t.Errorf("estimated_in wiped by an omission: %v", got.EstimatedIn)
	}
	if got.ActualIn == nil || !got.ActualIn.Truncate(time.Second).Equal(act.UTC().Truncate(time.Second)) {
		t.Errorf("actual_in wiped by an omission: %v", got.ActualIn)
	}
}

// TestFlightPartsRecentlyArrived: only parts that landed inside
// postArrivalWindow come back, which is the band in which the belt is still
// worth asking for. History, flights still in the air, and terminal statuses
// whose story is over are all excluded, because re-resolving those would spend
// resolver quota to learn nothing.
func TestFlightPartsRecentlyArrived(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	justLanded := mkFlightPart(t, s, owner, "JL1", now.Add(-3*time.Hour), now.Add(-10*time.Minute))
	longAgo := mkFlightPart(t, s, owner, "LA1", now.Add(-30*time.Hour), now.Add(-28*time.Hour))
	stillFlying := mkFlightPart(t, s, owner, "SF1", now.Add(-time.Hour), now.Add(time.Hour))
	for _, id := range []int64{justLanded, longAgo, stillFlying} {
		if err := s.RefreshFlightPartStatus(ctx, id); err != nil {
			t.Fatalf("RefreshFlightPartStatus: %v", err)
		}
	}

	got, err := s.FlightPartsRecentlyArrived(ctx, now)
	if err != nil {
		t.Fatalf("FlightPartsRecentlyArrived: %v", err)
	}
	if len(got) != 1 || got[0].ID != justLanded {
		t.Fatalf("expected only the just-landed part %d, got %+v", justLanded, got)
	}

	// A late flight gets its window from when it really landed, not from the
	// timetable: this one was due hours ago but only touched down minutes ago.
	late := mkFlightPart(t, s, owner, "LT1", now.Add(-8*time.Hour), now.Add(-5*time.Hour))
	touchdown := now.Add(-5 * time.Minute)
	if err := s.RefreshFlightPartArrival(ctx, late, nil, &touchdown); err != nil {
		t.Fatalf("RefreshFlightPartArrival: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, late); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	got, err = s.FlightPartsRecentlyArrived(ctx, now)
	if err != nil {
		t.Fatalf("FlightPartsRecentlyArrived: %v", err)
	}
	var foundLate bool
	for _, f := range got {
		if f.ID == late {
			foundLate = true
		}
	}
	if !foundLate {
		t.Error("a late flight's post-arrival window should run from its actual touchdown")
	}
}

// TestFlightPartsNeedingMetadata: only non-terminal parts in the 12h–30min
// pre-departure band are returned — the window for resolving gate/airframe
// ahead of the position-tracking window.
func TestFlightPartsNeedingMetadata(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)

	// In band: departs in 6h.
	inBand := mkFlightPartInTrip(t, s, trip, owner, "BAND1",
		now.Add(6*time.Hour), now.Add(7*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)
	// Too soon (10 min out): owned by ActiveFlightParts, excluded here.
	mkFlightPartInTrip(t, s, trip, owner, "SOON1",
		now.Add(10*time.Minute), now.Add(2*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)
	// Too far (13h out): beyond the 12h window.
	mkFlightPartInTrip(t, s, trip, owner, "FAR1",
		now.Add(13*time.Hour), now.Add(14*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)
	// Terminal in-band: excluded.
	mkFlightPartInTrip(t, s, trip, owner, "DONE1",
		now.Add(6*time.Hour), now.Add(7*time.Hour), "Cancelled", 51.47, -0.46, 40.64, -73.78)

	parts, err := s.FlightPartsNeedingMetadata(ctx, now)
	if err != nil {
		t.Fatalf("FlightPartsNeedingMetadata: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != inBand {
		t.Fatalf("expected only the in-band part %d, got %d: %+v", inBand, len(parts), parts)
	}
}

// TestRefreshFlightPartSchedule: an unconfirmed (resolved=false) flight has its
// schedule overwritten by the resolver, even a complete one (so a provisional
// email/manual schedule is corrected); a provider-confirmed (resolved=true)
// schedule is frozen and left untouched.
func TestRefreshFlightPartSchedule(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now().Truncate(time.Second)
	owner := mkUser(t, s)

	// Unconfirmed flight with a COMPLETE provisional schedule (arrival after
	// departure). Under the old degenerate-only guard this was protected; now
	// it must be overwritten because the provider is authoritative.
	prov := mkFlightPart(t, s, owner, "TK1986", now.Add(2*time.Hour), now.Add(5*time.Hour))
	wantOut, wantIn := now.Add(3*time.Hour), now.Add(7*time.Hour)
	if err := s.RefreshFlightPartSchedule(ctx, prov, wantOut, wantIn); err != nil {
		t.Fatalf("RefreshFlightPartSchedule: %v", err)
	}
	pf, _ := s.FlightPartByID(ctx, prov)
	if d := pf.ScheduledOut.Sub(wantOut); d > time.Second || d < -time.Second {
		t.Errorf("provisional schedule not corrected: out=%v want≈%v", pf.ScheduledOut, wantOut)
	}

	// Confirmed flight (resolved=true): schedule is frozen, must NOT change.
	confirmed := mkFlightPart(t, s, owner, "BA286", now.Add(2*time.Hour), now.Add(4*time.Hour))
	if _, err := s.pool.Exec(ctx,
		`UPDATE flight_details SET resolved = true WHERE plan_part_id = $1`, confirmed); err != nil {
		t.Fatalf("mark resolved: %v", err)
	}
	wantFrozen := now.Add(4 * time.Hour)
	if err := s.RefreshFlightPartSchedule(ctx, confirmed, now, now.Add(10*time.Hour)); err != nil {
		t.Fatalf("RefreshFlightPartSchedule (frozen): %v", err)
	}
	cf, _ := s.FlightPartByID(ctx, confirmed)
	if d := cf.ScheduledIn.Sub(wantFrozen); d > time.Second || d < -time.Second {
		t.Errorf("confirmed schedule was overwritten: in=%v want≈%v", cf.ScheduledIn, wantFrozen)
	}
}

// TestFlightPartWriteHelpers exercises the part-keyed status / airframe /
// backfill writers — the mechanical counterparts the poller now calls.
func TestFlightPartWriteHelpers(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	// Departed already → status should derive to Enroute on refresh.
	part := mkFlightPart(t, s, owner, "BA286", now.Add(-time.Hour), now.Add(time.Hour))

	if err := s.RefreshFlightPartStatus(ctx, part); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ := s.FlightPartByID(ctx, part)
	if f.Status != "Enroute" {
		t.Errorf("status should derive to Enroute, got %q", f.Status)
	}
	if f.LastPolledAt == nil {
		t.Error("RefreshFlightPartStatus should set last_polled_at")
	}

	// Backfill protects user-typed values: notes already set, so it's kept;
	// blank airframe gets filled.
	if err := s.BackfillFlightPart(ctx, part, BackfillPayload{
		ICAO24: "406B05", Callsign: "BAW286",
	}); err != nil {
		t.Fatalf("BackfillFlightPart: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, part)
	if f.ICAO24 == nil || *f.ICAO24 != "406b05" {
		t.Errorf("icao24 not backfilled: %v", f.ICAO24)
	}
	if f.LastResolvedAt != nil {
		t.Error("BackfillFlightPart must NOT bump last_resolved_at")
	}

	// RefreshFlightPartAirframe overwrites and stamps last_resolved_at.
	if err := s.RefreshFlightPartAirframe(ctx, part, "3c4a8c", "DLH1"); err != nil {
		t.Fatalf("RefreshFlightPartAirframe: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, part)
	if f.ICAO24 == nil || *f.ICAO24 != "3c4a8c" {
		t.Errorf("airframe should be overwritten, got %v", f.ICAO24)
	}
	if f.LastResolvedAt == nil {
		t.Error("RefreshFlightPartAirframe should bump last_resolved_at")
	}

	// Terminal is backfilled only-fill-empty; gate is captured via the
	// dedicated overwrite-when-non-empty writer.
	if err := s.BackfillFlightPart(ctx, part, BackfillPayload{
		OriginTerminal: "5", DestTerminal: "I",
	}); err != nil {
		t.Fatalf("BackfillFlightPart terminal: %v", err)
	}
	if err := s.RefreshFlightPartGate(ctx, part, "B32", "A12"); err != nil {
		t.Fatalf("RefreshFlightPartGate: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, part)
	if f.OriginTerminal != "5" || f.DestTerminal != "I" {
		t.Errorf("terminal not captured: %q/%q", f.OriginTerminal, f.DestTerminal)
	}
	if f.OriginGate != "B32" || f.DestGate != "A12" {
		t.Errorf("gate not captured: %q/%q", f.OriginGate, f.DestGate)
	}

	// Terminal is only-fill-empty: a second backfill must NOT overwrite it.
	if err := s.BackfillFlightPart(ctx, part, BackfillPayload{OriginTerminal: "2"}); err != nil {
		t.Fatalf("BackfillFlightPart terminal again: %v", err)
	}
	// Gate IS updatable: a non-empty value overwrites; an empty value preserves.
	if err := s.RefreshFlightPartGate(ctx, part, "B40", ""); err != nil {
		t.Fatalf("RefreshFlightPartGate update: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, part)
	if f.OriginTerminal != "5" {
		t.Errorf("terminal should be only-fill-empty, got %q", f.OriginTerminal)
	}
	if f.OriginGate != "B40" {
		t.Errorf("gate should be overwritten to B40, got %q", f.OriginGate)
	}
	if f.DestGate != "A12" {
		t.Errorf("empty gate should preserve existing A12, got %q", f.DestGate)
	}
}

// TestConvergenceWindowAndVisibility: the convergence query respects the §4
// gate and the arrival window. A plan hidden from a trip member must not appear
// in that member's results.
func TestConvergenceWindowAndVisibility(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	member := mkUser(t, s)
	stranger := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	addMember(t, s, trip, member, "viewer")
	befriendStore(t, s, owner, member) // friend gate: a trip member must be an accepted friend (spec §4)

	in := mkFlightPartInTrip(t, s, trip, owner, "VIS1",
		now.Add(-time.Hour), now.Add(2*time.Hour), "Enroute", 51.47, -0.46, 40.64, -73.78)
	// Out of the window.
	mkFlightPartInTrip(t, s, trip, owner, "FAR1",
		now.Add(20*24*time.Hour), now.Add(21*24*time.Hour), "Scheduled", 51.47, -0.46, 40.64, -73.78)

	from, to := now.Add(-7*24*time.Hour), now.Add(7*24*time.Hour)

	// Owner sees the in-window part, not the far one.
	parts, err := s.ConvergenceParts(ctx, owner, from, to, "")
	if err != nil {
		t.Fatalf("ConvergenceParts owner: %v", err)
	}
	if len(parts) != 1 || parts[0].PlanPartID != in {
		t.Fatalf("owner: expected the in-window part, got %d: %+v", len(parts), parts)
	}

	// A non-member sees nothing.
	parts, _ = s.ConvergenceParts(ctx, stranger, from, to, "")
	if len(parts) != 0 {
		t.Errorf("stranger should see no parts, got %d", len(parts))
	}

	// Hide the plan from member → member must not see it.
	planID := planOf(t, s, in)
	setVisibility(t, s, planID, "hidden_from", member)
	parts, _ = s.ConvergenceParts(ctx, member, from, to, "")
	for _, p := range parts {
		if p.PlanPartID == in {
			t.Fatalf("hidden part leaked to member: %+v", parts)
		}
	}

	// TrackerPartByID enforces the same gate.
	if _, err := s.TrackerPartByID(ctx, member, in); err != ErrNotFound {
		t.Errorf("TrackerPartByID for a hidden viewer should be ErrNotFound, got %v", err)
	}
	if _, err := s.TrackerPartByID(ctx, owner, in); err != nil {
		t.Errorf("owner should see their own part via TrackerPartByID: %v", err)
	}
}

// TestTaggedTripSpan: the derived span covers the tagged trip's parts.
func TestTaggedTripSpan(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now().Truncate(time.Second)
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	tagTrip(t, s, trip, "ski")
	start := now.Add(10 * 24 * time.Hour)
	end := now.Add(12 * 24 * time.Hour)
	mkFlightPartInTrip(t, s, trip, owner, "SKI1", start, end, "Scheduled", 51.47, -0.46, 40.64, -73.78)

	from, to, ok, err := s.TaggedTripSpan(ctx, owner, "ski")
	if err != nil {
		t.Fatalf("TaggedTripSpan: %v", err)
	}
	if !ok {
		t.Fatal("expected a span for the tagged trip")
	}
	if from.Unix() != start.Unix() || to.Unix() != end.Unix() {
		t.Errorf("span = [%v, %v], want [%v, %v]", from, to, start, end)
	}

	// An untagged / unknown tag yields no span.
	if _, _, ok, _ := s.TaggedTripSpan(ctx, owner, "beach"); ok {
		t.Error("unknown tag should yield no span")
	}
}

// TestProvisionalFlightParts: only live, non-terminal, unconfirmed parts are
// returned — the candidates for the sweep's provisional re-resolution pass.
func TestProvisionalFlightParts(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// Unconfirmed, future, non-terminal → returned.
	want := mkFlightPart(t, s, owner, "TK1986", now.Add(48*time.Hour), now.Add(50*time.Hour))

	// Confirmed → excluded.
	confirmed := mkFlightPart(t, s, owner, "BA286", now.Add(48*time.Hour), now.Add(50*time.Hour))
	if _, err := s.pool.Exec(ctx,
		`UPDATE flight_details SET resolved = true WHERE plan_part_id = $1`, confirmed); err != nil {
		t.Fatalf("mark resolved: %v", err)
	}

	// Terminal status → excluded (don't burn quota re-resolving history).
	arrived := mkFlightPart(t, s, owner, "LH441", now.Add(-50*time.Hour), now.Add(-48*time.Hour))
	if _, err := s.pool.Exec(ctx,
		`UPDATE flight_details SET flight_status = 'Arrived' WHERE plan_part_id = $1`, arrived); err != nil {
		t.Fatalf("mark arrived: %v", err)
	}

	parts, err := s.ProvisionalFlightParts(ctx)
	if err != nil {
		t.Fatalf("ProvisionalFlightParts: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != want {
		t.Fatalf("expected only the unconfirmed future part %d, got %d: %+v", want, len(parts), parts)
	}
}

// TestRefreshFlightPartTerminal: a non-empty terminal overwrites the stored
// value (so a reassignment is captured); an empty value preserves the existing
// terminal (the provider omits it until assigned).
func TestRefreshFlightPartTerminal(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	id := mkFlightPart(t, s, owner, "BA286", now.Add(2*time.Hour), now.Add(4*time.Hour))

	if err := s.RefreshFlightPartTerminal(ctx, id, "5", "B"); err != nil {
		t.Fatalf("RefreshFlightPartTerminal: %v", err)
	}
	got, _ := s.FlightPartByID(ctx, id)
	if got.OriginTerminal != "5" || got.DestTerminal != "B" {
		t.Fatalf("terminal not written: o=%q d=%q", got.OriginTerminal, got.DestTerminal)
	}

	// A reassignment overwrites; an empty value preserves.
	if err := s.RefreshFlightPartTerminal(ctx, id, "3", ""); err != nil {
		t.Fatalf("RefreshFlightPartTerminal (2): %v", err)
	}
	got, _ = s.FlightPartByID(ctx, id)
	if got.OriginTerminal != "3" {
		t.Errorf("origin terminal not overwritten: %q", got.OriginTerminal)
	}
	if got.DestTerminal != "B" {
		t.Errorf("empty dest terminal should preserve existing: %q", got.DestTerminal)
	}
}

func planOf(t *testing.T, s *Store, partID int64) int64 {
	t.Helper()
	var planID int64
	if err := s.pool.QueryRow(ctx,
		`SELECT plan_id FROM plan_parts WHERE id = $1`, partID).Scan(&planID); err != nil {
		t.Fatalf("planOf: %v", err)
	}
	return planID
}

func tagTrip(t *testing.T, s *Store, tripID int64, label string) {
	t.Helper()
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_tags (trip_id, label_norm, label_display) VALUES ($1, $2, $2)`,
		tripID, label); err != nil {
		t.Fatalf("tagTrip: %v", err)
	}
}

// TestRefreshStatusUnknownArrivalTerminates is the other half of
// TestRefreshStatusUnknownArrival. That test pins the rule that a flight with
// no real arrival time must not be declared Arrived the moment its departure
// passes; this one pins that it does not stay Enroute for ever either. Left
// unbounded it sits in the poll set indefinitely, and a flight the provider has
// no record of then costs a resolver call on every tick — which is how one
// unresolvable manual add drank a month of AeroDataBox quota in three days.
func TestRefreshStatusUnknownArrivalTerminates(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// Just inside the cap: still Enroute, because the flight may genuinely be
	// in the air (a long-haul leg can run most of a day).
	inside := mkFlightPart(t, s, owner, "G31850", now.Add(-20*time.Hour), now.Add(-20*time.Hour))
	if err := s.RefreshFlightPartStatus(ctx, inside); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ := s.FlightPartByID(ctx, inside)
	if f.Status != "Enroute" {
		t.Errorf("20h past departure should still be Enroute, got %q", f.Status)
	}

	// Past the cap: whatever happened, it is over.
	beyond := mkFlightPart(t, s, owner, "G31851", now.Add(-25*time.Hour), now.Add(-25*time.Hour))
	if err := s.RefreshFlightPartStatus(ctx, beyond); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, beyond)
	if f.Status != "Arrived" {
		t.Errorf("past the no-arrival cap the part should be terminal, got %q", f.Status)
	}

	// And so it leaves the poll set, which is the point.
	parts, err := s.ActiveFlightParts(ctx, now)
	if err != nil {
		t.Fatalf("ActiveFlightParts: %v", err)
	}
	for _, p := range parts {
		if p.ID == beyond {
			t.Fatal("a terminated part must not still be in the active poll set")
		}
	}
}

// TestRefreshStatusCancelledSurvivesTheCap: the new cap must not overwrite a
// terminal status the provider gave us, or a cancelled flight would be quietly
// relabelled as having arrived.
func TestRefreshStatusCancelledSurvivesTheCap(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)
	part := mkFlightPart(t, s, owner, "G31852", now.Add(-25*time.Hour), now.Add(-25*time.Hour))
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET flight_status = 'Cancelled' WHERE plan_part_id = $1`, part); err != nil {
		t.Fatalf("set cancelled: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, part); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ := s.FlightPartByID(ctx, part)
	if f.Status != "Cancelled" {
		t.Errorf("a cancelled flight must stay cancelled, got %q", f.Status)
	}
}

// TestRefreshStatusUnknownArrivalHonoursDelayedDeparture: the no-arrival cap is
// counted from the departure we actually expect, so a part held on stand
// through a long delay is not declared over before it has left — the same rule
// the departure branch below it already applies. The cap on that slip stops a
// revision that is never updated again holding the part open indefinitely,
// which is the whole point of having a cap.
func TestRefreshStatusUnknownArrivalHonoursDelayedDeparture(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// Timetabled 25h ago (past the cap), but the airline has it leaving in an
	// hour. It has not departed, so it certainly has not arrived.
	delayed := mkFlightPart(t, s, owner, "G31853", now.Add(-25*time.Hour), now.Add(-25*time.Hour))
	eta := now.Add(time.Hour)
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET estimated_out = $2 WHERE plan_part_id = $1`, delayed, eta); err != nil {
		t.Fatalf("set estimated_out: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, delayed); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ := s.FlightPartByID(ctx, delayed)
	if f.Status == "Arrived" {
		t.Errorf("a flight that has not departed must not be Arrived, got %q", f.Status)
	}

	// A revised departure that stopped being updated must not defeat the cap:
	// the slip allowance is 12h, so 12h + 24h past the timetable it is over
	// regardless of what the stale estimate still claims.
	stale := mkFlightPart(t, s, owner, "G31854", now.Add(-40*time.Hour), now.Add(-40*time.Hour))
	staleETA := now.Add(-38 * time.Hour).Add(100 * time.Hour) // never updated again
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET estimated_out = $2 WHERE plan_part_id = $1`, stale, staleETA); err != nil {
		t.Fatalf("set stale estimated_out: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, stale); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, stale)
	if f.Status != "Arrived" {
		t.Errorf("a stale revision must not hold the part open for ever, got %q", f.Status)
	}
}

// TestRefreshStatusUnknownArrivalObservedDeparture: an observed wheels-off is a
// fact, not a revision that might go stale, so it is never capped — a flight
// that really did leave twenty hours late gets its full day from when it
// actually left, rather than being declared over sixteen hours after takeoff.
func TestRefreshStatusUnknownArrivalObservedDeparture(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// Timetabled 36h ago, actually departed 20h late (so 16h ago). Past the
	// timetable + slip + cap, but only 16h past the real departure.
	late := mkFlightPart(t, s, owner, "G31855", now.Add(-36*time.Hour), now.Add(-36*time.Hour))
	departed := now.Add(-16 * time.Hour)
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET actual_out = $2 WHERE plan_part_id = $1`, late, departed); err != nil {
		t.Fatalf("set actual_out: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, late); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ := s.FlightPartByID(ctx, late)
	if f.Status == "Arrived" {
		t.Errorf("16h after a real departure is inside the cap, got %q", f.Status)
	}

	// The same flight a full day past its real departure is over.
	over := mkFlightPart(t, s, owner, "G31856", now.Add(-48*time.Hour), now.Add(-48*time.Hour))
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET actual_out = $2 WHERE plan_part_id = $1`,
		over, now.Add(-25*time.Hour)); err != nil {
		t.Fatalf("set actual_out: %v", err)
	}
	if err := s.RefreshFlightPartStatus(ctx, over); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ = s.FlightPartByID(ctx, over)
	if f.Status != "Arrived" {
		t.Errorf("25h after a real departure should be terminal, got %q", f.Status)
	}
}

// TestRefreshStatusUnknownArrivalNoLiveTimes guards the LEAST/NULL trap: with
// neither an observed nor an estimated departure the cap must count from the
// timetable, not from the capped arm. Postgres's LEAST ignores NULLs, so
// without the CASE around it a part with no estimate at all would quietly wait
// departureSlipCap longer than intended — which is exactly the shape of part
// this whole change exists to terminate.
func TestRefreshStatusUnknownArrivalNoLiveTimes(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	now := time.Now()
	owner := mkUser(t, s)

	// 25h past a timetabled departure, no live times at all: over.
	bare := mkFlightPart(t, s, owner, "G31857", now.Add(-25*time.Hour), now.Add(-25*time.Hour))
	if err := s.RefreshFlightPartStatus(ctx, bare); err != nil {
		t.Fatalf("RefreshFlightPartStatus: %v", err)
	}
	f, _ := s.FlightPartByID(ctx, bare)
	if f.Status != "Arrived" {
		t.Errorf("with no live times the cap counts from the timetable, got %q", f.Status)
	}
}
