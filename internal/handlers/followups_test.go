package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// subscribe attaches a non-superuser subscriber for viewerID and returns its
// event channel plus an unsubscribe func. Mirrors how /api/events builds the
// Subscription for a regular signed-in user.
func subscribe(e *testEnv, viewerID int64) (<-chan sse.Event, func()) {
	return e.hub.Subscribe(sse.Subscription{ViewerID: viewerID})
}

// flightPlanBody is the create-plan body used by the live-edit tests: a single
// LHR→SFO flight part.
func flightPlanBody() map[string]any {
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	return map[string]any{
		"type": "flight", "title": "BA286", "confirmation_ref": "X1",
		"parts": []map[string]any{{
			"type": "flight", "starts_at": out, "ends_at": in,
			"start_label": "LHR", "end_label": "SFO",
			"flight": map[string]any{
				"ident": "BA286", "scheduled_out": out, "scheduled_in": in,
				"origin_iata": "LHR", "dest_iata": "SFO",
			},
		}},
	}
}

func eventTypes(evs []sse.Event) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.Type
	}
	return out
}

func findEvent(evs []sse.Event, typ string) (sse.Event, bool) {
	for _, ev := range evs {
		if ev.Type == typ {
			return ev, true
		}
	}
	return sse.Event{}, false
}

// TestSSEPublishedOnPlanMutation asserts a plan mutation publishes a
// plan.updated event scoped to the plan's visibility set (VisibleTo includes
// the trip owner) and that delete publishes plan.deleted.
func TestSSEPublishedOnPlanMutation(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")

	ch, unsub := subscribe(e, owner)
	defer unsub()

	// Create a plan: expect a plan.updated event the owner can see.
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), flightPlanBody(), owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", w.Code, w.Body.String())
	}
	pid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	evs := drainEvents(ch)
	ev, ok := findEvent(evs, "plan.updated")
	if !ok {
		t.Fatalf("no plan.updated published on create; got %v", eventTypes(evs))
	}
	// VisibleTo must include the owner (the §4 visibility set).
	var sawOwner bool
	for _, id := range ev.VisibleTo {
		if id == owner {
			sawOwner = true
		}
	}
	if !sawOwner {
		t.Errorf("plan.updated VisibleTo = %v, want to include owner %d", ev.VisibleTo, owner)
	}
	var payload struct {
		TripID int64 `json:"trip_id"`
		PlanID int64 `json:"plan_id"`
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.TripID != tid || payload.PlanID != pid {
		t.Errorf("payload = %+v, want trip %d plan %d", payload, tid, pid)
	}

	// Delete the plan: expect plan.deleted scoped to the (pre-delete) set.
	if w := e.req(t, "DELETE", fmt.Sprintf("/api/plans/%d", pid), nil, owner); w.Code != http.StatusNoContent {
		t.Fatalf("delete plan = %d %s", w.Code, w.Body.String())
	}
	evs = drainEvents(ch)
	if ev, ok := findEvent(evs, "plan.deleted"); !ok {
		t.Errorf("no plan.deleted published on delete; got %v", eventTypes(evs))
	} else {
		sawOwner = false
		for _, id := range ev.VisibleTo {
			if id == owner {
				sawOwner = true
			}
		}
		if !sawOwner {
			t.Errorf("plan.deleted VisibleTo = %v, want to include owner %d", ev.VisibleTo, owner)
		}
	}
}

// TestSSEPublishedOnTripMutation asserts trip create/update publishes
// trip.updated scoped to the trip's members.
func TestSSEPublishedOnTripMutation(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)

	ch, unsub := subscribe(e, owner)
	defer unsub()

	tid := newTrip(t, e, owner, "Trip")
	evs := drainEvents(ch)
	ev, ok := findEvent(evs, "trip.updated")
	if !ok {
		t.Fatalf("no trip.updated on create; got %v", eventTypes(evs))
	}
	var sawOwner bool
	for _, id := range ev.VisibleTo {
		if id == owner {
			sawOwner = true
		}
	}
	if !sawOwner {
		t.Errorf("trip.updated VisibleTo = %v, want to include owner %d", ev.VisibleTo, owner)
	}
	var payload struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil || payload.ID != tid {
		t.Errorf("trip.updated payload = %s (err %v), want id %d", string(ev.Data), err, tid)
	}
}

// TestPlanAlertOptedInDTO asserts alert_opted_in on PlanDTO reflects the
// requesting viewer's opt-in state.
func TestPlanAlertOptedInDTO(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), flightPlanBody(), owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", w.Code, w.Body.String())
	}
	pid := int64(decodeBody[map[string]any](t, w)["id"].(float64))

	// Before opt-in: false on the trip-detail read.
	if got := planAlertOptedIn(t, e, tid, pid, owner); got {
		t.Errorf("alert_opted_in before opt-in = true, want false")
	}

	// Opt in, then re-read.
	if w := e.req(t, "POST", fmt.Sprintf("/api/plans/%d/alerts/optin", pid), nil, owner); w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("optin = %d %s", w.Code, w.Body.String())
	}
	if got := planAlertOptedIn(t, e, tid, pid, owner); !got {
		t.Errorf("alert_opted_in after opt-in = false, want true")
	}
}

// planAlertOptedIn reads the trip detail as viewer and returns the named plan's
// alert_opted_in flag.
func planAlertOptedIn(t *testing.T, e *testEnv, tripID, planID, viewer int64) bool {
	t.Helper()
	w := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", tripID), nil, viewer)
	if w.Code != http.StatusOK {
		t.Fatalf("get trip = %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Plans []api.PlanDTO `json:"plans"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trip: %v", err)
	}
	for _, p := range body.Plans {
		if p.ID == planID {
			return p.AlertOptedIn
		}
	}
	t.Fatalf("plan %d not found in trip %d", planID, tripID)
	return false
}

// TestTrackerPartIncludesTrack asserts the single-part tracker endpoint returns
// a PlanPartDTO whose flight detail carries the flown track.
func TestTrackerPartIncludesTrack(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")
	w := e.req(t, "POST", fmt.Sprintf("/api/trips/%d/plans", tid), flightPlanBody(), owner)
	if w.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", w.Code, w.Body.String())
	}
	plan := decodeBody[map[string]any](t, w)
	part0 := plan["parts"].([]any)[0].(map[string]any)
	partID := int64(part0["id"].(float64))

	// Seed two positions for the flight part.
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	for i, p := range []struct{ lat, lon float64 }{{51.4, -0.4}, {45.0, -30.0}} {
		if err := e.store.InsertPartPosition(ctx, store.Position{
			FlightID: partID, Ts: base.Add(time.Duration(i) * time.Minute),
			Lat: p.lat, Lon: p.lon,
		}); err != nil {
			t.Fatalf("seed position: %v", err)
		}
	}

	w = e.req(t, "GET", fmt.Sprintf("/api/tracker/part/%d", partID), nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("get tracker part = %d %s", w.Code, w.Body.String())
	}
	dto := decodeBody[api.PlanPartDTO](t, w)
	if dto.Flight == nil {
		t.Fatalf("flight detail missing: %s", w.Body.String())
	}
	if len(dto.Flight.Track) != 2 {
		t.Errorf("track len = %d, want 2 (%s)", len(dto.Flight.Track), w.Body.String())
	}
	if dto.Flight.LatestPosition == nil {
		t.Errorf("latest_position missing on single-part view")
	}

	// A stranger can't see it → 404 (not 403), no existence leak.
	stranger := e.user(t, "stranger", false)
	if w := e.req(t, "GET", fmt.Sprintf("/api/tracker/part/%d", partID), nil, stranger); w.Code != http.StatusNotFound {
		t.Errorf("stranger tracker part = %d, want 404", w.Code)
	}
}

// capturingExtractor records the documents handed to ExtractPlans so the
// multipart-ingest test can assert the uploaded file reached the extractor.
type capturingExtractor struct {
	gotText string
	gotDocs []planops.Document
	plans   []planops.ExtractedPlan
}

func (c *capturingExtractor) ExtractPlans(_ context.Context, body string, docs []planops.Document) ([]planops.ExtractedPlan, error) {
	c.gotText = body
	c.gotDocs = docs
	return c.plans, nil
}

// TestMultipartIngestReachesExtractor posts a multipart/form-data ingest with a
// PDF file and asserts the document (bytes + media type) reached the extractor.
func TestMultipartIngestReachesExtractor(t *testing.T) {
	e := setup(t, nil, nil)
	ext := &capturingExtractor{plans: []planops.ExtractedPlan{{
		Type: "hotel", Title: "Hotel Plaza",
		Parts: []planops.ExtractedPart{{
			Type: "hotel", Confidence: "high",
			StartDate: "2026-06-01", EndDate: "2026-06-05", HotelName: "Hotel Plaza",
		}},
	}}}
	e.api.Extractor = ext
	owner := e.user(t, "owner", false)
	tid := newTrip(t, e, owner, "Trip")

	pdfBytes := []byte("%PDF-1.4 fake ticket bytes")
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("source", "upload")
	fw, err := mw.CreateFormFile("file", "ticket.pdf")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(pdfBytes); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_ = mw.Close()

	r := httptest.NewRequest("POST", fmt.Sprintf("/api/trips/%d/ingest", tid), &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.AddCookie(&http.Cookie{
		Name:  auth.SessionCookie,
		Value: auth.SignSession(sessKey, owner, 0, time.Now().Add(time.Hour)),
	})
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("multipart ingest = %d %s", rec.Code, rec.Body.String())
	}

	if len(ext.gotDocs) != 1 {
		t.Fatalf("extractor got %d docs, want 1", len(ext.gotDocs))
	}
	doc := ext.gotDocs[0]
	if !bytes.Equal(doc.Data, pdfBytes) {
		t.Errorf("doc bytes = %q, want %q", doc.Data, pdfBytes)
	}
	if doc.MediaType != "application/pdf" {
		t.Errorf("doc media type = %q, want application/pdf", doc.MediaType)
	}
	if doc.Filename != "ticket.pdf" {
		t.Errorf("doc filename = %q, want ticket.pdf", doc.Filename)
	}
}
