// Integration tests for the kayak-ics-convert tool.
//
// These tests exercise the full pipeline: load backup JSON from testdata/,
// emit an .ics via buildICS, parse it back with importics.Parse, then map it
// with importics.MapAll — and assert that the result matches the expected
// structure of the synthetic trips.
//
// Synthetic fixture files in testdata/:
//
//	SA0001.txt  Sample Flights 2026  – 4 flights (2-leg outbound + 2-leg return),
//	                                    2 buses, 1 hotel.  2026-04-01 → 2026-04-12.
//	JA0002.txt  Sample Rail 2025     – train (BTS→PRG), excursion, bus (PRG→BTS).
//	                                    2025-06-18 → 2025-06-19.
//	HQ0003.txt  Sample City 2025     – car rental, meeting (Other), restaurant,
//	                                    hotel.  2025-11-05 → 2025-11-06.
//	ST0004.txt  Sample Straddle 2025 – outbound flight Dec 2025, return Jan 2026;
//	                                    verifies boundary-overlap inclusion.
//	FU0005.txt  Sample Future 2027   – entirely in 2027; must be excluded from
//	                                    2025/2026 filters.
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/importics"
	"github.com/dpage/aerly/internal/planops"
)

const testdataFolder = "testdata"

// ── helpers ──────────────────────────────────────────────────────────────────

func mustGenerate(t *testing.T, period string) []*importics.MappedTrip {
	t.Helper()
	start, end, err := parsePeriod(period)
	if err != nil {
		t.Fatalf("parsePeriod(%q): %v", period, err)
	}
	trips, err := loadTrips(testdataFolder)
	if err != nil {
		t.Fatalf("loadTrips: %v", err)
	}
	var sel []tripFile
	for _, tf := range trips {
		if overlaps(tf.tripStart, tf.tripEnd, start, end) {
			sel = append(sel, tf)
		}
	}
	ics := buildICS(sel)

	cal, err := importics.Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mapped, src, ok := importics.MapAll(cal)
	if !ok {
		t.Fatal("MapAll: not recognised as Kayak calendar")
	}
	if src != importics.SourceKayak {
		t.Fatalf("MapAll source = %q, want %q", src, importics.SourceKayak)
	}
	return mapped
}

func findTrip(t *testing.T, trips []*importics.MappedTrip, name string) *importics.MappedTrip {
	t.Helper()
	for _, mt := range trips {
		if mt.Name == name {
			return mt
		}
	}
	var names []string
	for _, mt := range trips {
		names = append(names, mt.Name)
	}
	t.Fatalf("no mapped trip named %q; got: %v", name, names)
	return nil
}

func countByType(mt *importics.MappedTrip) map[string]int {
	out := map[string]int{}
	for _, p := range mt.Plans {
		out[p.Type]++
	}
	return out
}

// ── Detect ───────────────────────────────────────────────────────────────────

// TestConvertDetect verifies that an .ics produced by the converter is
// recognised as SourceKayak by the importics detector.
func TestConvertDetect(t *testing.T) {
	trips, err := loadTrips(testdataFolder)
	if err != nil {
		t.Fatalf("loadTrips: %v", err)
	}
	ics := buildICS(trips)
	cal, err := importics.Parse(strings.NewReader(ics))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := importics.Detect(cal); got != importics.SourceKayak {
		t.Fatalf("Detect = %q, want %q", got, importics.SourceKayak)
	}
}

// ── Period filtering ─────────────────────────────────────────────────────────

// TestConvertPeriodYear verifies that year filtering selects the correct trips
// from the fixture set:
//   - 2026 must include SA0001 (Apr 2026) but exclude JA0002/HQ0003 (2025) and
//     FU0005 (2027).
//   - Every returned trip's envelope must overlap 2026.
func TestConvertPeriodYear(t *testing.T) {
	trips2026 := mustGenerate(t, "2026")

	// SA0001 (Sample Flights 2026) and ST0004 (straddles Dec 2025 – Jan 2026)
	// must both appear; FU0005 (2027) must not.
	findTrip(t, trips2026, "Sample Flights 2026")
	findTrip(t, trips2026, "Sample Straddle 2025")
	for _, mt := range trips2026 {
		if mt.Name == "Sample Future 2027" {
			t.Errorf("trip %q should have been filtered out for 2026", mt.Name)
		}
	}

	// Every trip's StartsOn or EndsOn must touch 2026.
	y2026start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	y2026end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, mt := range trips2026 {
		if mt.StartsOn == nil {
			continue
		}
		endsAfterWindowStart := mt.EndsOn != nil && mt.EndsOn.After(y2026start.AddDate(0, 0, -1))
		startsBeforeWindowEnd := mt.StartsOn.Before(y2026end)
		if !startsBeforeWindowEnd && !endsAfterWindowStart {
			t.Errorf("trip %q (start=%v end=%v) does not overlap 2026", mt.Name, mt.StartsOn, mt.EndsOn)
		}
	}
}

// TestConvertPeriodMonth verifies that month filtering is finer than year
// filtering: 2025-06 contains JA0002 but not HQ0003 (Nov 2025) or SA0001.
func TestConvertPeriodMonth(t *testing.T) {
	trips2025 := mustGenerate(t, "2025")
	trips2025_06 := mustGenerate(t, "2025-06")
	trips2025_11 := mustGenerate(t, "2025-11")

	if len(trips2025_06) >= len(trips2025) {
		t.Errorf("2025-06 returned %d trips but full-year 2025 returned %d; expected fewer",
			len(trips2025_06), len(trips2025))
	}

	findTrip(t, trips2025_06, "Sample Rail 2025") // JA0002 is in June
	findTrip(t, trips2025_11, "Sample City 2025") // HQ0003 is in November

	// HQ0003 must not appear in June.
	for _, mt := range trips2025_06 {
		if mt.Name == "Sample City 2025" {
			t.Errorf("Sample City 2025 (Nov) should not appear in 2025-06 filter")
		}
	}
	// JA0002 must not appear in November.
	for _, mt := range trips2025_11 {
		if mt.Name == "Sample Rail 2025" {
			t.Errorf("Sample Rail 2025 (Jun) should not appear in 2025-11 filter")
		}
	}
}

// TestConvertBoundaryOverlap verifies that ST0004 ("Sample Straddle 2025"),
// which departs Dec 2025 and returns Jan 2026, appears in BOTH the 2025 and
// the 2026 windows.
func TestConvertBoundaryOverlap(t *testing.T) {
	trips2025 := mustGenerate(t, "2025")
	trips2026 := mustGenerate(t, "2026")

	findTrip(t, trips2025, "Sample Straddle 2025")
	findTrip(t, trips2026, "Sample Straddle 2025")
}

// TestConvertExcludeOutOfWindow verifies that FU0005 ("Sample Future 2027")
// never appears in 2025 or 2026 filters.
func TestConvertExcludeOutOfWindow(t *testing.T) {
	for _, period := range []string{"2025", "2026", "2025-06", "2026-04"} {
		for _, mt := range mustGenerate(t, period) {
			if mt.Name == "Sample Future 2027" {
				t.Errorf("period %q: Sample Future 2027 should be excluded", period)
			}
		}
	}
}

// ── Sample Flights 2026 (SA0001) ─────────────────────────────────────────────

// TestConvertFlightsTrip exercises multi-leg flights, buses and hotel from SA0001.
func TestConvertFlightsTrip(t *testing.T) {
	trips := mustGenerate(t, "2026")
	mt := findTrip(t, trips, "Sample Flights 2026")

	if mt.StartsOn == nil || mt.StartsOn.Format("2006-01-02") != "2026-04-01" {
		t.Errorf("StartsOn = %v, want 2026-04-01", mt.StartsOn)
	}
	if mt.EndsOn == nil {
		t.Error("EndsOn is nil")
	}

	got := countByType(mt)
	// 4 flight segments (LX3537, LX282, LH579, LH6326), 2 buses, 1 hotel.
	if got["flight"] != 4 {
		t.Errorf("flight count = %d, want 4", got["flight"])
	}
	if got["ground"] != 2 {
		t.Errorf("ground count = %d, want 2", got["ground"])
	}
	if got["hotel"] != 1 {
		t.Errorf("hotel count = %d, want 1", got["hotel"])
	}

	// LX282: ZRH → JNB.
	var lx282Found bool
	for _, p := range mt.Plans {
		if p.Type != "flight" {
			continue
		}
		for _, part := range p.Parts {
			if part.Flight != nil && part.Flight.Ident == "LX282" {
				lx282Found = true
				if part.Flight.OriginIATA != "ZRH" || part.Flight.DestIATA != "JNB" {
					t.Errorf("LX282 route = %s→%s, want ZRH→JNB",
						part.Flight.OriginIATA, part.Flight.DestIATA)
				}
				// Both airports are in the table → coords must be set.
				if part.StartLat == nil || part.StartLon == nil {
					t.Error("LX282 origin has no coordinates")
				}
				if part.EndLat == nil || part.EndLon == nil {
					t.Error("LX282 destination has no coordinates")
				}
			}
		}
	}
	if !lx282Found {
		t.Error("did not find flight plan for LX282")
	}
}

// ── Sample Rail 2025 (JA0002) ────────────────────────────────────────────────

// TestConvertRailTrip verifies the train (RJ 1048 BTS→PRG) and bus (RegioJet)
// from JA0002, checking departure/arrival labels and plan types.
func TestConvertRailTrip(t *testing.T) {
	trips := mustGenerate(t, "2025-06")
	mt := findTrip(t, trips, "Sample Rail 2025")

	counts := countByType(mt)
	if counts["train"] != 1 {
		t.Fatalf("train count = %d, want 1", counts["train"])
	}
	if counts["ground"] != 1 {
		t.Fatalf("ground count = %d, want 1", counts["ground"])
	}
	if counts["excursion"] != 1 {
		t.Fatalf("excursion count = %d, want 1", counts["excursion"])
	}

	var train, bus *planops.ConfirmPlanInput
	for i := range mt.Plans {
		switch mt.Plans[i].Title {
		case "Train RJ 1048":
			p := mt.Plans[i]
			train = &p
		case "Bus RegioJet":
			p := mt.Plans[i]
			bus = &p
		}
	}

	if train == nil {
		t.Fatal("did not find plan titled 'Train RJ 1048'")
	}
	if train.Type != "train" || train.Parts[0].Train == nil {
		t.Fatalf("train plan type=%q, train detail=%v", train.Type, train.Parts[0].Train)
	}
	if train.Parts[0].Train.Operator != "RJ 1048" {
		t.Errorf("train operator = %q, want %q", train.Parts[0].Train.Operator, "RJ 1048")
	}
	if train.Parts[0].StartLabel != "Bratislava, hl.n, Slovakia" {
		t.Errorf("train from = %q, want %q", train.Parts[0].StartLabel, "Bratislava, hl.n, Slovakia")
	}
	if train.Parts[0].EndLabel != "Praha, hl.n, Czechia" {
		t.Errorf("train to = %q, want %q", train.Parts[0].EndLabel, "Praha, hl.n, Czechia")
	}

	if bus == nil {
		t.Fatal("did not find plan titled 'Bus RegioJet'")
	}
	if bus.Type != "ground" || bus.Parts[0].Ground == nil {
		t.Fatalf("bus plan type=%q, ground detail=%v", bus.Type, bus.Parts[0].Ground)
	}
	if bus.Parts[0].Ground.Provider != "RegioJet" {
		t.Errorf("bus provider = %q, want RegioJet", bus.Parts[0].Ground.Provider)
	}
	if bus.Parts[0].StartLabel != "Praha, \u00daAN Florenc, Czechia" {
		t.Errorf("bus from = %q, want %q", bus.Parts[0].StartLabel, "Praha, \u00daAN Florenc, Czechia")
	}
}

// ── Sample City 2025 (HQ0003) ────────────────────────────────────────────────

// TestConvertCityTrip verifies car rental, restaurant (dining) and hotel from HQ0003.
func TestConvertCityTrip(t *testing.T) {
	trips := mustGenerate(t, "2025-11")
	mt := findTrip(t, trips, "Sample City 2025")

	counts := countByType(mt)
	if counts["ground"] != 1 {
		t.Errorf("ground (car) count = %d, want 1", counts["ground"])
	}
	if counts["dining"] != 1 {
		t.Errorf("dining count = %d, want 1", counts["dining"])
	}
	if counts["hotel"] != 1 {
		t.Errorf("hotel count = %d, want 1", counts["hotel"])
	}
	if counts["excursion"] != 1 {
		t.Errorf("excursion count = %d, want 1", counts["excursion"])
	}

	// Car: pickup summary must carry the agency name.
	var carPlan *planops.ConfirmPlanInput
	for i := range mt.Plans {
		if mt.Plans[i].Type == "ground" {
			p := mt.Plans[i]
			carPlan = &p
			break
		}
	}
	if carPlan == nil {
		t.Fatal("no ground plan found")
	}
	if !strings.Contains(carPlan.Title, "SampleRent") {
		t.Errorf("car plan title %q does not contain agency name", carPlan.Title)
	}
	if carPlan.Parts[0].Ground == nil || carPlan.Parts[0].Ground.Provider != "SampleRent" {
		t.Errorf("car provider = %v, want SampleRent", carPlan.Parts[0].Ground)
	}

	// Hotel: paired check-in/out must produce a single plan named after the property.
	var hotelPlan *planops.ConfirmPlanInput
	for i := range mt.Plans {
		if mt.Plans[i].Type == "hotel" {
			p := mt.Plans[i]
			hotelPlan = &p
			break
		}
	}
	if hotelPlan == nil {
		t.Fatal("no hotel plan found")
	}
	if hotelPlan.Title != "Sample City Hotel" {
		t.Errorf("hotel title = %q, want Sample City Hotel", hotelPlan.Title)
	}
	if hotelPlan.ConfirmationRef != "GGG777" {
		t.Errorf("hotel confirmation = %q, want GGG777", hotelPlan.ConfirmationRef)
	}
}

// ── UID uniqueness ────────────────────────────────────────────────────────────

// TestConvertUIDUniqueness verifies that every plan across the whole fixture set
// carries a non-empty TripItUID and that no two plans share one (UIDs are used
// for per-plan re-import deduplication).
func TestConvertUIDUniqueness(t *testing.T) {
	trips, err := loadTrips(testdataFolder)
	if err != nil {
		t.Fatalf("loadTrips: %v", err)
	}
	cal, err := importics.Parse(strings.NewReader(buildICS(trips)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	mapped, _, ok := importics.MapAll(cal)
	if !ok {
		t.Fatal("MapAll not ok")
	}

	seen := map[string]string{} // uid → trip name
	for _, mt := range mapped {
		for _, p := range mt.Plans {
			if p.TripItUID == "" {
				t.Errorf("trip %q plan %q has empty TripItUID", mt.Name, p.Title)
				continue
			}
			if prev, ok := seen[p.TripItUID]; ok {
				t.Errorf("duplicate TripItUID %q in trips %q and %q", p.TripItUID, prev, mt.Name)
			}
			seen[p.TripItUID] = mt.Name
		}
	}
}

// ── parsePeriod ───────────────────────────────────────────────────────────────

func TestParsePeriod(t *testing.T) {
	tests := []struct {
		input     string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{"2025", "2025-01-01", "2026-01-01", false},
		{"2025-06", "2025-06-01", "2025-07-01", false},
		{"2025-12", "2025-12-01", "2026-01-01", false},
		{"2025-1", "", "", true},
		{"25", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range tests {
		s, e, err := parsePeriod(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parsePeriod(%q): expected error, got nil", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePeriod(%q): %v", tc.input, err)
			continue
		}
		if s.Format("2006-01-02") != tc.wantStart {
			t.Errorf("parsePeriod(%q) start = %q, want %q", tc.input, s.Format("2006-01-02"), tc.wantStart)
		}
		if e.Format("2006-01-02") != tc.wantEnd {
			t.Errorf("parsePeriod(%q) end = %q, want %q", tc.input, e.Format("2006-01-02"), tc.wantEnd)
		}
	}
}
