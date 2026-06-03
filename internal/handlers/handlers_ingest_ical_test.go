package handlers

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/dpage/aerly/internal/api"
)

// readICS loads a real TripIt fixture shared with the tripitics package.
func readICS(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("../tripitics/testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

// TestIngestICalPropose: a TripIt .ics pasted/uploaded is imported via the
// deterministic tripitics path — note setup wires NO extractor, so this also
// proves the iCal path needs no LLM provider. Historical (2016) flights survive
// (no ±2-year guard) and carry a terminal status so the poller ignores them.
func TestIngestICalPropose(t *testing.T) {
	e := setup(t, nil, nil) // no extractor
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")

	body := map[string]any{"text": readICS(t, "pgconfeu_2016.ics")}
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid), body, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("ical propose = %d: %s", w.Code, w.Body.String())
	}
	res := decodeBody[api.IngestResultDTO](t, w)

	byType := map[string]int{}
	var aFlight, aHotel *api.PlanPartDTO
	for i := range res.Proposals {
		p := res.Proposals[i]
		byType[p.Type]++
		switch p.Type {
		case "flight":
			if p.Parts[0].Flight != nil && p.Parts[0].Flight.Ident == "AY832" {
				aFlight = &res.Proposals[i].Parts[0]
			}
		case "hotel":
			aHotel = &res.Proposals[i].Parts[0]
		}
	}
	if byType["flight"] != 4 || byType["ground"] != 2 || byType["hotel"] != 1 {
		t.Fatalf("proposal types = %v, want 4 flight / 2 ground / 1 hotel", byType)
	}

	if aFlight == nil || aFlight.Flight == nil {
		t.Fatal("AY832 flight proposal missing")
	}
	fd := aFlight.Flight
	if fd.OriginIATA != "LHR" || fd.DestIATA != "HEL" {
		t.Errorf("AY832 route = %s→%s", fd.OriginIATA, fd.DestIATA)
	}
	if fd.FlightStatus != "Arrived" {
		t.Errorf("AY832 flight_status = %q, want Arrived", fd.FlightStatus)
	}
	if fd.ScheduledOut.Format("2006-01-02T15:04:05Z") != "2016-10-30T10:20:00Z" {
		t.Errorf("AY832 scheduled_out = %v", fd.ScheduledOut)
	}
	if aFlight.StartTZ != "Europe/London" {
		t.Errorf("AY832 start_tz = %q, want Europe/London", aFlight.StartTZ)
	}

	if aHotel == nil || aHotel.Hotel == nil {
		t.Fatal("hotel proposal missing")
	}
	if aHotel.StartsAt.Format("2006-01-02") != "2016-10-30" ||
		aHotel.EndsAt == nil || aHotel.EndsAt.Format("2006-01-02") != "2016-11-06" {
		t.Errorf("hotel span = %v → %v, want 2016-10-30 → 2016-11-06", aHotel.StartsAt, aHotel.EndsAt)
	}
}

// TestIngestICalConfirmKeepsArrived confirms an imported flight and asserts the
// created flight part keeps flight_status=Arrived end-to-end — the guarantee
// that the live poller won't try to track a historical leg.
func TestIngestICalConfirmKeepsArrived(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")

	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest", tid),
		map[string]any{"text": readICS(t, "pgconfeu_2016.ics")}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("propose = %d: %s", w.Code, w.Body.String())
	}
	res := decodeBody[api.IngestResultDTO](t, w)

	var fp *api.ProposedPlanDTO
	for i := range res.Proposals {
		if res.Proposals[i].Type == "flight" {
			fp = &res.Proposals[i]
			break
		}
	}
	if fp == nil {
		t.Fatal("no flight proposal to confirm")
	}
	part := fp.Parts[0]
	confirm := map[string]any{"plans": []map[string]any{{
		"type": "flight", "title": fp.Title, "source": "upload",
		"parts": []map[string]any{{
			"type":      "flight",
			"starts_at": part.StartsAt,
			"ends_at":   part.EndsAt,
			"start_tz":  part.StartTZ,
			"end_tz":    part.EndTZ,
			"flight": map[string]any{
				"ident":         part.Flight.Ident,
				"scheduled_out": part.Flight.ScheduledOut,
				"scheduled_in":  part.Flight.ScheduledIn,
				"origin_iata":   part.Flight.OriginIATA,
				"dest_iata":     part.Flight.DestIATA,
				"flight_status": part.Flight.FlightStatus,
			},
		}},
	}}}
	w = e.req(t, "POST", fmt.Sprintf("/api/trips/%d/ingest/confirm", tid), confirm, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("confirm = %d: %s", w.Code, w.Body.String())
	}
	plans := decodeBody[[]api.PlanDTO](t, w)
	if len(plans) != 1 || len(plans[0].Parts) != 1 || plans[0].Parts[0].Flight == nil {
		t.Fatalf("created flight plan missing: %+v", plans)
	}
	if got := plans[0].Parts[0].Flight.FlightStatus; got != "Arrived" {
		t.Errorf("created flight_status = %q, want Arrived (poller must skip it)", got)
	}
	if plans[0].Source != "upload" {
		t.Errorf("source = %q, want upload", plans[0].Source)
	}
}
