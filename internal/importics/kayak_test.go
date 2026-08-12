package importics

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/planops"
)

// findTrip returns the mapped trip with the given name, or fails.
func findTrip(t *testing.T, trips []*MappedTrip, name string) *MappedTrip {
	t.Helper()
	for _, mt := range trips {
		if mt.Name == name {
			return mt
		}
	}
	t.Fatalf("no mapped trip named %q", name)
	return nil
}

// countPlans tallies a trip's plans by type.
func countPlans(mt *MappedTrip) map[string]int {
	out := map[string]int{}
	for _, p := range mt.Plans {
		out[p.Type]++
	}
	return out
}

// TestKayakDetect: the account feed is recognised as Kayak from its PRODID /
// calendar name, even though no event carries a TripIt marker.
func TestKayakDetect(t *testing.T) {
	cal := parseFixture(t, "kayak_trips.ics")
	if got := Detect(cal); got != SourceKayak {
		t.Fatalf("Detect = %q, want %q", got, SourceKayak)
	}
}

// TestKayakMapAllSplitsTrips: a single Kayak .ics holds many trips and each is
// mapped separately, named from its envelope and tagged with its source id.
func TestKayakMapAllSplitsTrips(t *testing.T) {
	cal := parseFixture(t, "kayak_trips.ics")
	trips, src, ok := MapAll(cal)
	if !ok || src != SourceKayak {
		t.Fatalf("MapAll ok=%v src=%q, want true/%q", ok, src, SourceKayak)
	}
	// The fixture carries 6 distinct trips (one date-only envelope each).
	if len(trips) != 6 {
		t.Fatalf("mapped %d trips, want 6", len(trips))
	}
	seen := map[string]bool{}
	for _, mt := range trips {
		if mt.Name == "" {
			t.Errorf("trip %q has empty name", mt.TripItID)
		}
		if mt.TripItID == "" {
			t.Errorf("trip %q has empty source id", mt.Name)
		}
		if seen[mt.TripItID] {
			t.Errorf("duplicate trip id %q", mt.TripItID)
		}
		seen[mt.TripItID] = true
		// Every imported plan must carry its source event UID for per-plan
		// re-import dedupe.
		for _, p := range mt.Plans {
			if p.TripItUID == "" {
				t.Errorf("trip %q plan %q has no source UID", mt.Name, p.Title)
			}
		}
	}
}

// TestKayakSouthAfricaTrip exercises the full set of mapped types on one trip:
// four flights, two coach transfers (ground), and a paired hotel stay.
func TestKayakSouthAfricaTrip(t *testing.T) {
	cal := parseFixture(t, "kayak_trips.ics")
	trips, _, _ := MapAll(cal)
	mt := findTrip(t, trips, "South Africa 2026")

	if mt.StartsOn == nil || mt.StartsOn.Format("2006-01-02") != "2026-04-01" {
		t.Errorf("StartsOn = %v, want 2026-04-01", mt.StartsOn)
	}
	if mt.EndsOn == nil || mt.EndsOn.Format("2006-01-02") != "2026-04-12" {
		t.Errorf("EndsOn = %v, want 2026-04-12 (inclusive)", mt.EndsOn)
	}

	got := countPlans(mt)
	if got["flight"] != 4 || got["ground"] != 2 || got["hotel"] != 1 {
		t.Fatalf("plan counts = %v, want 4 flight / 2 ground / 1 hotel", got)
	}

	// A specific leg: ident from SUMMARY, IATA route, future flight → tracked.
	var leg *planops.ConfirmPartInput
	for i := range mt.Plans {
		if mt.Plans[i].Type == "flight" {
			fd := mt.Plans[i].Parts[0].Flight
			if fd != nil && fd.Ident == "LX282" {
				leg = &mt.Plans[i].Parts[0]
			}
		}
	}
	if leg == nil {
		t.Fatal("did not find flight LX282")
	}
	if leg.Flight.OriginIATA != "ZRH" || leg.Flight.DestIATA != "JNB" {
		t.Errorf("LX282 route = %s→%s, want ZRH→JNB", leg.Flight.OriginIATA, leg.Flight.DestIATA)
	}
}

// TestKayakFlightStatus: a flight already arrived is marked terminal so the live
// poller skips it; a future one stays Scheduled so it is tracked.
func TestKayakFlightStatus(t *testing.T) {
	if got := kayakFlightStatus(time.Now().Add(-24 * time.Hour)); got != "Arrived" {
		t.Errorf("past flight status = %q, want Arrived", got)
	}
	if got := kayakFlightStatus(time.Now().Add(24 * time.Hour)); got != "Scheduled" {
		t.Errorf("future flight status = %q, want Scheduled", got)
	}
}

// TestKayakRailAndBus: rail and coach are mapped from the DESCRIPTION route +
// SUMMARY provider, into train and ground plans respectively.
func TestKayakRailAndBus(t *testing.T) {
	cal := parseFixture(t, "kayak_trips.ics")
	trips, _, _ := MapAll(cal)

	// The "Jeff Arcuri" trip opens with a train (RJ 1048) and a return coach.
	mt := findTrip(t, trips, "Jeff Arcuri")
	var train, bus *planops.ConfirmPlanInput
	for i := range mt.Plans {
		switch mt.Plans[i].Title {
		case "Train RJ 1048":
			train = &mt.Plans[i]
		case "Bus RegioJet":
			bus = &mt.Plans[i]
		}
	}
	if train == nil {
		t.Fatal("did not find Train RJ 1048")
	}
	if train.Type != "train" || train.Parts[0].Train == nil {
		t.Fatalf("train plan type=%q train=%v", train.Type, train.Parts[0].Train)
	}
	if train.Parts[0].Train.Operator != "RJ 1048" {
		t.Errorf("operator = %q, want %q", train.Parts[0].Train.Operator, "RJ 1048")
	}
	if train.Parts[0].StartLabel != "Bratislava, hl.n, Slovakia" {
		t.Errorf("train from = %q", train.Parts[0].StartLabel)
	}
	if train.Parts[0].EndLabel != "Praha, hl.n, Czechia" {
		t.Errorf("train to = %q", train.Parts[0].EndLabel)
	}

	if bus == nil {
		t.Fatal("did not find Bus RegioJet")
	}
	if bus.Type != "ground" || bus.Parts[0].Ground == nil {
		t.Fatalf("bus plan type=%q ground=%v", bus.Type, bus.Parts[0].Ground)
	}
	if bus.Parts[0].Ground.Provider != "RegioJet" {
		t.Errorf("bus provider = %q, want RegioJet", bus.Parts[0].Ground.Provider)
	}
}

// TestKayakCarHire: the fixture's "Car Pickup"/"Car Dropoff" pair maps to one
// vehicle_hire plan spanning the whole hire — the return instant comes from the
// dropoff event, not from the end of the half-hour collection appointment — with
// the agency on the plan's SupplierName and both branch addresses kept.
func TestKayakCarHire(t *testing.T) {
	cal := parseFixture(t, "kayak_trips.ics")
	trips, _, _ := MapAll(cal)
	mt := findTrip(t, trips, "HQ 2025.11")

	if got := countPlans(mt); got["vehicle_hire"] != 1 || got["ground"] != 0 {
		t.Fatalf("plan counts = %v, want 1 vehicle_hire / 0 ground", got)
	}
	var hire *planops.ConfirmPlanInput
	for i := range mt.Plans {
		if mt.Plans[i].Type == "vehicle_hire" {
			hire = &mt.Plans[i]
		}
	}
	if hire.SupplierName != "SampleRent" {
		t.Errorf("SupplierName = %q, want SampleRent", hire.SupplierName)
	}
	if hire.ConfirmationRef != "FFF666" {
		t.Errorf("ConfirmationRef = %q, want FFF666", hire.ConfirmationRef)
	}
	// The pickup event is the anchor, so a re-import still dedupes on its UID.
	if hire.TripItUID != "car-pickup@kayak.com" {
		t.Errorf("TripItUID = %q, want the pickup event's UID", hire.TripItUID)
	}
	if len(hire.Parts) != 1 {
		t.Fatalf("got %d parts, want 1", len(hire.Parts))
	}
	part := hire.Parts[0]
	if part.Type != "vehicle_hire" {
		t.Errorf("part type = %q, want vehicle_hire", part.Type)
	}
	assertInstant(t, "StartsAt", &part.StartsAt, "2025-11-05T07:00:00Z")
	assertInstant(t, "EndsAt", part.EndsAt, "2025-11-06T07:00:00Z")
	if part.StartAddress != "2 Example Road, Sampleton 000 00" {
		t.Errorf("StartAddress = %q", part.StartAddress)
	}
	if part.EndAddress != "2 Example Road, Sampleton 000 00" {
		t.Errorf("EndAddress = %q", part.EndAddress)
	}
	if part.StartLabel != "SampleRent" || part.EndLabel != "SampleRent" {
		t.Errorf("labels = %q / %q, want the agency on both edges", part.StartLabel, part.EndLabel)
	}
	// The satellite carries only what Kayak states: the car type, with its
	// trailing gearbox word lifted out.
	if part.VehicleHire == nil {
		t.Fatal("no vehicle hire detail")
	}
	if part.VehicleHire.Vehicle != "Sample Car or similar" {
		t.Errorf("Vehicle = %q, want %q", part.VehicleHire.Vehicle, "Sample Car or similar")
	}
	if part.VehicleHire.Transmission != "Automatic" {
		t.Errorf("Transmission = %q, want Automatic", part.VehicleHire.Transmission)
	}
	if part.VehicleHire.Category != "" || part.VehicleHire.ExcessAmount != nil {
		t.Errorf("satellite invented fields Kayak does not state: %+v", part.VehicleHire)
	}
	if part.Ground != nil {
		t.Errorf("hire still carries a ground detail: %+v", part.Ground)
	}
}

// TestKayakCarHireUnpairedPickup: a pickup with no return in the feed still
// yields a hire, falling back to its own DTEND rather than inventing a return.
func TestKayakCarHireUnpairedPickup(t *testing.T) {
	cal := parseSynthetic(t, carVEvent("hire-1@example.test", "Car Pickup (Agency: Testcar Hire)",
		"20260301T090000Z", "20260301T093000Z",
		`Pickup Address: 1 Test Way\, Exampleton\nConfirmation Number: TEST111`))

	plans := syntheticPlans(t, cal)
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1", len(plans))
	}
	if plans[0].Type != "vehicle_hire" || plans[0].SupplierName != "Testcar Hire" {
		t.Fatalf("plan = %q / %q", plans[0].Type, plans[0].SupplierName)
	}
	assertInstant(t, "EndsAt", plans[0].Parts[0].EndsAt, "2026-03-01T09:30:00Z")
	// With no dropoff stated, the pickup branch stands in for the return.
	if plans[0].Parts[0].EndAddress != "1 Test Way, Exampleton" {
		t.Errorf("EndAddress = %q", plans[0].Parts[0].EndAddress)
	}
	if plans[0].Parts[0].VehicleHire == nil || plans[0].Parts[0].VehicleHire.Vehicle != "" {
		t.Errorf("satellite = %+v, want an empty one", plans[0].Parts[0].VehicleHire)
	}
}

// TestKayakCarHireDropoffWithoutPickup: a stray return event on its own must not
// produce a plan, since it carries neither branch's full detail nor the UID the
// dedupe keys on.
func TestKayakCarHireDropoffWithoutPickup(t *testing.T) {
	cal := parseSynthetic(t, carVEvent("hire-orphan@example.test", "Car Dropoff (Agency: Testcar Hire)",
		"20260304T090000Z", "20260304T093000Z",
		`Dropoff Address: 1 Test Way\, Exampleton\nConfirmation Number: TEST111`))

	if plans := syntheticPlans(t, cal); len(plans) != 0 {
		t.Fatalf("got %d plans from a lone dropoff, want 0", len(plans))
	}
}

// TestKayakCarHireTwoRentals: two hires from the same agency in one calendar,
// written out of order, each pair with their own return rather than both
// claiming the first one. The second return omits the confirmation number, so it
// also exercises the agency-only fallback.
func TestKayakCarHireTwoRentals(t *testing.T) {
	cal := parseSynthetic(t,
		carVEvent("hire-b-drop@example.test", "Car Dropoff (Agency: Testcar Hire)",
			"20260321T080000Z", "20260321T083000Z",
			`Dropoff Address: 3 Sample Street\, Exampleton`),
		carVEvent("hire-a-pick@example.test", "Car Pickup (Agency: Testcar Hire)",
			"20260301T090000Z", "20260301T093000Z",
			`Pickup Address: 1 Test Way\, Exampleton\nDropoff Address: 2 Test Way\, Exampleton\nConfirmation Number: TEST111`),
		carVEvent("hire-b-pick@example.test", "Car Pickup (Agency: Testcar Hire)",
			"20260320T090000Z", "20260320T093000Z",
			`Pickup Address: 3 Sample Street\, Exampleton\nConfirmation Number: TEST222`),
		carVEvent("hire-a-drop@example.test", "Car Dropoff (Agency: Testcar Hire)",
			"20260305T080000Z", "20260305T083000Z",
			`Dropoff Address: 2 Test Way\, Exampleton\nConfirmation Number: TEST111`),
	)

	plans := syntheticPlans(t, cal)
	if len(plans) != 2 {
		t.Fatalf("got %d plans, want 2", len(plans))
	}
	byUID := map[string]planops.ConfirmPlanInput{}
	for _, p := range plans {
		if p.Type != "vehicle_hire" {
			t.Fatalf("plan %q type = %q", p.TripItUID, p.Type)
		}
		byUID[p.TripItUID] = p
	}
	first, ok := byUID["hire-a-pick@example.test"]
	if !ok {
		t.Fatal("first hire missing")
	}
	assertInstant(t, "first EndsAt", first.Parts[0].EndsAt, "2026-03-05T08:00:00Z")
	second, ok := byUID["hire-b-pick@example.test"]
	if !ok {
		t.Fatal("second hire missing")
	}
	assertInstant(t, "second EndsAt", second.Parts[0].EndsAt, "2026-03-21T08:00:00Z")
	// The second pickup states no return branch, so it is taken from the
	// return event it paired with.
	if second.Parts[0].EndAddress != "3 Sample Street, Exampleton" {
		t.Errorf("second EndAddress = %q", second.Parts[0].EndAddress)
	}
}

// TestKayakCarHireOtherAgencyNotPaired: a return from a different hire company
// is never claimed, so the pickup falls back to its own end instant.
func TestKayakCarHireOtherAgencyNotPaired(t *testing.T) {
	cal := parseSynthetic(t,
		carVEvent("hire-x-pick@example.test", "Car Pickup (Agency: Testcar Hire)",
			"20260301T090000Z", "20260301T093000Z",
			`Pickup Address: 1 Test Way\, Exampleton\nConfirmation Number: TEST111`),
		carVEvent("hire-y-drop@example.test", "Car Dropoff (Agency: Sample Vans)",
			"20260305T080000Z", "20260305T083000Z",
			`Dropoff Address: 9 Other Lane\, Exampleton\nConfirmation Number: TEST999`),
	)

	plans := syntheticPlans(t, cal)
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1", len(plans))
	}
	assertInstant(t, "EndsAt", plans[0].Parts[0].EndsAt, "2026-03-01T09:30:00Z")
}

// TestKayakVehicleHireManualGearbox: a manual car type is split the same way,
// and a car type with no gearbox word is left whole.
func TestKayakVehicleHireManualGearbox(t *testing.T) {
	d := kayakVehicleHire("Car Type: Sample Hatchback or similar Manual\n")
	if d.Vehicle != "Sample Hatchback or similar" || d.Transmission != "Manual" {
		t.Errorf("manual split = %q / %q", d.Vehicle, d.Transmission)
	}
	d = kayakVehicleHire("Car Type: Sample Estate\n")
	if d.Vehicle != "Sample Estate" || d.Transmission != "" {
		t.Errorf("no-gearbox split = %q / %q", d.Vehicle, d.Transmission)
	}
}

// assertInstant checks an optional instant against an RFC 3339 want.
func assertInstant(t *testing.T, what string, got *time.Time, want string) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s is nil, want %s", what, want)
		return
	}
	if g := got.UTC().Format(time.RFC3339); g != want {
		t.Errorf("%s = %s, want %s", what, g, want)
	}
}

// carVEvent renders one synthetic Kayak car VEVENT. Every value here is
// invented: no real agency, address or booking reference appears in these tests.
func carVEvent(uid, summary, start, end, desc string) string {
	return "BEGIN:VEVENT\n" +
		"UID:" + uid + "\n" +
		"DTSTART:" + start + "\n" +
		"DTEND:" + end + "\n" +
		"SUMMARY:" + summary + "\n" +
		"DESCRIPTION:" + desc +
		`\n\nView trip: https://www.kayak.com/trips/!TESTCAR?ref=calendar` + "\n" +
		"END:VEVENT\n"
}

// parseSynthetic parses a hand-built Kayak calendar made of the given VEVENTs.
func parseSynthetic(t *testing.T, vevents ...string) *Calendar {
	t.Helper()
	body := "BEGIN:VCALENDAR\nPRODID:-//Test//Synthetic//EN\n" +
		strings.Join(vevents, "") + "END:VCALENDAR\n"
	cal, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse synthetic calendar: %v", err)
	}
	return cal
}

// syntheticPlans maps a synthetic single-trip Kayak calendar and returns its
// plans, tolerating a calendar that maps to no trip at all.
func syntheticPlans(t *testing.T, cal *Calendar) []planops.ConfirmPlanInput {
	t.Helper()
	trips := mapKayak(cal)
	if len(trips) == 0 {
		return nil
	}
	if len(trips) != 1 {
		t.Fatalf("got %d trips, want 1", len(trips))
	}
	return trips[0].Plans
}
