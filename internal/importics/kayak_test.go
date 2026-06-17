package importics

import (
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
