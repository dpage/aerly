package poller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/weather"
)

// fakeForecast records each forecast request and answers with days, or err.
type fakeForecast struct {
	mu    sync.Mutex
	days  []weather.Day
	err   error
	calls []forecastCall
}

type forecastCall struct {
	lat, lon float64
	tz       string
	from     time.Time
	days     int
}

func (f *fakeForecast) Forecast(_ context.Context, lat, lon float64, tz string, from time.Time, days int) ([]weather.Day, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, forecastCall{lat, lon, tz, from, days})
	if f.err != nil {
		return nil, f.err
	}
	return f.days, nil
}

func (f *fakeForecast) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fiveDays is a synthetic five-day forecast; the first day has a high chance
// of rain so the push weather line says so.
func fiveDays() []weather.Day {
	pct := func(v int) *int { return &v }
	return []weather.Day{
		{Date: "2026-10-15", Code: 61, MaxC: 18.4, MinC: 9.6, PrecipPct: pct(80)},
		{Date: "2026-10-16", Code: 2, MaxC: 17, MinC: 8, PrecipPct: pct(10)},
		{Date: "2026-10-17", Code: 0, MaxC: 19, MinC: 7},
		{Date: "2026-10-18", Code: 3, MaxC: 15, MinC: 6, PrecipPct: pct(30)},
		{Date: "2026-10-19", Code: 95, MaxC: 14, MinC: -0.3, PrecipPct: pct(90)},
	}
}

// depOut is the scheduled departure the departure tests share: 15:00 UTC
// (16:00 BST at Heathrow). The flight lands nine hours later, at 00:00 UTC on
// the 16th, which is still the evening of the 15th in Chicago, so a forecast
// starting on the 15th proves the date is taken in the destination's zone.
var depOut = time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)

// seedDepartureFlight seeds a flight from LHR to dest departing at depOut and
// returns its plan_part id and plan id.
func seedDepartureFlight(t *testing.T, s *store.Store, uid int64, ident, dest string) (int64, int64) {
	t.Helper()
	f, err := mkPart(context.Background(), s, partSeed{
		Ident:        ident,
		ScheduledOut: depOut,
		ScheduledIn:  depOut.Add(9 * time.Hour),
		OriginIATA:   "LHR",
		DestIATA:     dest,
	}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	var planID int64
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT plan_id FROM plan_parts WHERE id = $1`, f.ID).Scan(&planID); err != nil {
		t.Fatalf("plan lookup: %v", err)
	}
	return f.ID, planID
}

// optInToDepartures turns the user's departure-summary preference on, leaving
// the channel defaults (in-app + email) alone.
func optInToDepartures(t *testing.T, s *store.Store, uid int64) {
	t.Helper()
	prefs, err := s.AlertPrefsFor(context.Background(), uid)
	if err != nil {
		t.Fatalf("AlertPrefsFor: %v", err)
	}
	prefs.DepartureSummary = true
	if err := s.SetAlertPrefs(context.Background(), *prefs); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
}

// departureSent reports whether the (part, user) pair has been marked sent.
func departureSent(t *testing.T, s *store.Store, partID, uid int64) bool {
	t.Helper()
	var sent bool
	if err := s.Pool().QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM flight_departure_summary_sent WHERE plan_part_id = $1 AND user_id = $2)`,
		partID, uid).Scan(&sent); err != nil {
		t.Fatalf("sent lookup: %v", err)
	}
	return sent
}

// departureSetup wires a poller with a capturing mailer and a fake forecaster,
// and an opted-in traveller with a verified address holding one LHR → ORD
// flight.
func departureSetup(t *testing.T) (*Poller, *store.Store, *captureMailer, *fakeForecast, int64, int64) {
	t.Helper()
	p, s, _, cap := alertPoller(t)
	fc := &fakeForecast{days: fiveDays()}
	p.Weather = fc
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(context.Background(), owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToDepartures(t, s, owner)
	partID, _ := seedDepartureFlight(t, s, owner, "BA295", "ORD")
	return p, s, cap, fc, owner, partID
}

// TestRemindDepartures_FullSummary is the issue #130 happy path: live status,
// gate, arrival, supplier contact and a five-day destination forecast, on both
// channels, once.
func TestRemindDepartures_FullSummary(t *testing.T) {
	p, s, cap, fc, owner, partID := departureSetup(t)
	ctx := context.Background()
	setEstimatedOut(t, s, partID, depOut.Add(80*time.Minute))
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET origin_terminal = '5', origin_gate = 'B32' WHERE plan_part_id = $1`,
		partID); err != nil {
		t.Fatalf("set gate: %v", err)
	}
	if _, err := s.Pool().Exec(ctx, `
		UPDATE plans SET supplier_name = 'Example Air', contact_phone = '+44 20 7946 0000'
		WHERE id = (SELECT plan_id FROM plan_parts WHERE id = $1)`, partID); err != nil {
		t.Fatalf("set contact: %v", err)
	}

	p.remindDepartures(ctx, depOut.Add(-store.DepartureSummaryLead))

	if cap.count() != 1 {
		t.Fatalf("want 1 departure email, got %d", cap.count())
	}
	for _, want := range []string{
		"Subject: Departure summary: BA295 to Chicago",
		"BA295 (LHR → ORD) is delayed by 1 h 20 min, now departing terminal 5, gate B32 at Thu 15 Oct, 17:20 BST.",
		"Arrival at ORD is",
		"CDT",
		"For help or changes, contact Example Air: +44 20 7946 0000",
		"/trips/",
		"Weather in Chicago",
		"15 Oct",
		"19 Oct",
		"80% rain",
		"14°C / 0°C",
	} {
		if !strings.Contains(cap.sent[0], want) {
			t.Errorf("email missing %q\n---\n%s", want, cap.sent[0])
		}
	}
	if strings.Contains(cap.sent[0], "-0°C") {
		t.Errorf("negative zero rendered:\n%s", cap.sent[0])
	}

	if fc.count() != 1 {
		t.Fatalf("want one forecast fetch, got %d", fc.count())
	}
	c := fc.calls[0]
	if c.lat != 41.9742 || c.lon != -87.9073 || c.tz != "America/Chicago" || c.days != forecastDays {
		t.Errorf("forecast call = %+v", c)
	}
	// The scheduled arrival, 00:00 UTC on the 16th, is the evening of the 15th
	// in Chicago, so the forecast starts on the 15th.
	if want := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC); !c.from.Equal(want) {
		t.Errorf("forecast from = %v, want %v", c.from, want)
	}

	alerts, err := s.ListFlightAlerts(ctx, owner, 10)
	if err != nil {
		t.Fatalf("ListFlightAlerts: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Kind != "departure" || alerts[0].Status != "departure" ||
		alerts[0].PlanPartID != partID || !strings.HasPrefix(alerts[0].Message, "BA295 (LHR → ORD) is delayed") {
		t.Fatalf("want 1 in-app departure summary for part %d, got %+v", partID, alerts)
	}

	// A later tick must not re-send.
	cap.sent = nil
	p.remindDepartures(ctx, depOut.Add(-time.Hour))
	if cap.count() != 0 {
		t.Fatalf("re-sent on a later tick: %d", cap.count())
	}
	if alerts, _ = s.ListFlightAlerts(ctx, owner, 10); len(alerts) != 1 {
		t.Fatalf("in-app summary duplicated: %d", len(alerts))
	}
}

// TestRemindDepartures_OffByDefault: nobody gets a summary without opting in.
func TestRemindDepartures_OffByDefault(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	seedDepartureFlight(t, s, owner, "BA295", "ORD")

	p.remindDepartures(ctx, depOut.Add(-store.DepartureSummaryLead))

	if cap.count() != 0 {
		t.Fatalf("sent to a user who never opted in: %d", cap.count())
	}
	if alerts, _ := s.ListFlightAlerts(ctx, owner, 10); len(alerts) != 0 {
		t.Fatalf("in-app summary for a user who never opted in: %d", len(alerts))
	}
}

// TestRemindDepartures_OutsideWindow: nothing before the four-hour point, and
// nothing once the flight has gone.
func TestRemindDepartures_OutsideWindow(t *testing.T) {
	p, _, cap, _, _, _ := departureSetup(t)
	ctx := context.Background()

	p.remindDepartures(ctx, depOut.Add(-store.DepartureSummaryLead-time.Minute))
	if cap.count() != 0 {
		t.Fatalf("fired before the lead point: %d", cap.count())
	}
	p.remindDepartures(ctx, depOut.Add(time.Minute))
	if cap.count() != 0 {
		t.Fatalf("fired after departure: %d", cap.count())
	}
}

// TestRemindDepartures_HiddenPlanNotLeaked: an alert opt-in that outlived the
// user's access to the plan must not deliver.
func TestRemindDepartures_HiddenPlanNotLeaked(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	stranger := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, stranger, "stranger@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToDepartures(t, s, stranger)
	_, planID := seedDepartureFlight(t, s, owner, "BA295", "ORD")
	if err := s.AddPlanAlertOptin(ctx, planID, stranger); err != nil {
		t.Fatalf("AddPlanAlertOptin: %v", err)
	}

	p.remindDepartures(ctx, depOut.Add(-store.DepartureSummaryLead))

	if cap.count() != 0 {
		t.Fatalf("delivered to a user the plan is hidden from: %d", cap.count())
	}
	if alerts, _ := s.ListFlightAlerts(ctx, stranger, 10); len(alerts) != 0 {
		t.Fatalf("in-app row for a hidden plan: %d", len(alerts))
	}
}

// TestRemindDepartures_DueErrorReturns: a failed due query dispatches nothing.
func TestRemindDepartures_DueErrorReturns(t *testing.T) {
	p, _, _, cap := alertPoller(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.remindDepartures(ctx, time.Now())
	if cap.count() != 0 {
		t.Fatalf("dispatched on a failed query: %d", cap.count())
	}
}

// TestRemindDepartures_CancelledMidPass: a context cancelled after the due
// query still stops the dispatch.
func TestRemindDepartures_CancelledMidPass(t *testing.T) {
	p, s, cap, _, _, _ := departureSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	now := depOut.Add(-store.DepartureSummaryLead)
	if due, err := s.DueDepartureSummaries(ctx, now); err != nil || len(due) != 1 {
		t.Fatalf("setup: %d due (err %v)", len(due), err)
	}
	cancel()
	p.remindDepartures(ctx, now)
	if cap.count() != 0 {
		t.Fatalf("dispatched on a cancelled context: %d", cap.count())
	}
}

// TestRemindDepartures_WeatherNeverHoldsBack: no forecaster, a failing one, or
// a destination with no known coordinates all still send the summary, just
// without the weather block.
func TestRemindDepartures_WeatherNeverHoldsBack(t *testing.T) {
	for _, c := range []struct {
		name      string
		forecast  *fakeForecast
		dest      string
		wantCalls int
		wantLabel string
	}{
		{"no forecaster", nil, "ORD", 0, "Chicago"},
		{"provider error", &fakeForecast{err: errors.New("open-meteo: status 500")}, "ORD", 1, "Chicago"},
		// An off-table airport with no row coordinates: no forecast, and the
		// IATA code stands in for the city.
		{"no coordinates", &fakeForecast{days: fiveDays()}, "ZZQ", 0, "ZZQ"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, s, _, cap := alertPoller(t)
			if c.forecast != nil {
				p.Weather = c.forecast
			}
			ctx := context.Background()
			owner := seedUser(t, s)
			if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
				t.Fatalf("verify email: %v", err)
			}
			optInToDepartures(t, s, owner)
			partID, _ := seedDepartureFlight(t, s, owner, "BA295", c.dest)

			p.remindDepartures(ctx, depOut.Add(-store.DepartureSummaryLead))

			if cap.count() != 1 {
				t.Fatalf("want 1 email, got %d", cap.count())
			}
			if !strings.Contains(cap.sent[0], "Subject: Departure summary: BA295 to "+c.wantLabel) {
				t.Errorf("subject wrong:\n%s", cap.sent[0])
			}
			if strings.Contains(cap.sent[0], "Weather in") {
				t.Errorf("weather block present without a forecast:\n%s", cap.sent[0])
			}
			if c.forecast != nil && c.forecast.count() != c.wantCalls {
				t.Errorf("forecast calls = %d, want %d", c.forecast.count(), c.wantCalls)
			}
			if !departureSent(t, s, partID, owner) {
				t.Error("a summary sent without weather should still be marked")
			}
		})
	}
}

// TestRemindDepartures_RowCoordinatesFallback: an airport the table doesn't
// name is forecast from the coordinate the part row carries.
func TestRemindDepartures_RowCoordinatesFallback(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	fc := &fakeForecast{days: fiveDays()[:1]}
	p.Weather = fc
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToDepartures(t, s, owner)
	partID, _ := seedDepartureFlight(t, s, owner, "BA295", "ZZQ")
	if _, err := s.Pool().Exec(ctx,
		`UPDATE plan_parts SET end_lat = 51.5, end_lon = -0.12 WHERE id = $1`, partID); err != nil {
		t.Fatalf("set coords: %v", err)
	}

	p.remindDepartures(ctx, depOut.Add(-store.DepartureSummaryLead))

	if fc.count() != 1 || fc.calls[0].lat != 51.5 || fc.calls[0].lon != -0.12 {
		t.Fatalf("forecast calls = %+v, want one at the row coordinate", fc.calls)
	}
	if cap.count() != 1 || !strings.Contains(cap.sent[0], "Weather in ZZQ") {
		t.Fatalf("want the forecast headed by the IATA fallback label, got %d emails", cap.count())
	}
}

// TestRemindDepartures_BadForecastDateSkipped: a day the provider dated
// unreadably is dropped rather than rendered as year one.
func TestRemindDepartures_BadForecastDateSkipped(t *testing.T) {
	p, _, cap, fc, _, _ := departureSetup(t)
	fc.days = []weather.Day{{Date: "not-a-date", Code: 0}, {Date: "2026-10-16", Code: 0, MaxC: 12, MinC: 5}}

	p.remindDepartures(context.Background(), depOut.Add(-store.DepartureSummaryLead))

	if cap.count() != 1 {
		t.Fatalf("want 1 email, got %d", cap.count())
	}
	if !strings.Contains(cap.sent[0], "16 Oct") || strings.Contains(cap.sent[0], "1 Jan") {
		t.Errorf("forecast rows wrong:\n%s", cap.sent[0])
	}
}

// TestDispatchDeparture_LookupFailureSkipsMark: when the live flight or the
// plan can't be loaded nothing is sent and the pair stays due for next tick.
func TestDispatchDeparture_LookupFailureSkipsMark(t *testing.T) {
	p, s, cap, _, owner, partID := departureSetup(t)
	ctx := context.Background()
	due, err := s.DueDepartureSummaries(ctx, depOut.Add(-store.DepartureSummaryLead))
	if err != nil || len(due) != 1 {
		t.Fatalf("setup: %d due (err %v)", len(due), err)
	}

	missingPart := due[0]
	missingPart.PlanPartID = 1 << 40
	p.dispatchDeparture(ctx, missingPart)

	missingPlan := due[0]
	missingPlan.PlanID = 1 << 40
	p.dispatchDeparture(ctx, missingPlan)

	if cap.count() != 0 {
		t.Fatalf("sent despite a failed lookup: %d", cap.count())
	}
	if alerts, _ := s.ListFlightAlerts(ctx, owner, 10); len(alerts) != 0 {
		t.Fatalf("in-app row despite a failed lookup: %d", len(alerts))
	}
	if departureSent(t, s, partID, owner) {
		t.Fatal("marked sent despite a failed lookup")
	}
}

// TestDispatchDeparture_PublishErrorSkipsMark: a failed inbox insert bails
// before the email and the mark, so the next tick retries.
func TestDispatchDeparture_PublishErrorSkipsMark(t *testing.T) {
	p, s, cap, _, owner, partID := departureSetup(t)
	blockInserts(t, s, "flight_alerts")

	p.remindDepartures(context.Background(), depOut.Add(-store.DepartureSummaryLead))

	if cap.count() != 0 {
		t.Fatalf("emailed despite a failed inbox insert: %d", cap.count())
	}
	if departureSent(t, s, partID, owner) {
		t.Fatal("marked sent despite a failed inbox insert")
	}
}

// TestDispatchDeparture_SendErrorWithInAppStillMarks: the in-app row already
// reached the traveller, so a sendmail failure doesn't keep the pair due.
func TestDispatchDeparture_SendErrorWithInAppStillMarks(t *testing.T) {
	p, s, _, _, owner, partID := departureSetup(t)
	p.SendAlertEmail = func(context.Context, string, string, string) error {
		return errors.New("sendmail boom")
	}

	p.remindDepartures(context.Background(), depOut.Add(-store.DepartureSummaryLead))

	if !departureSent(t, s, partID, owner) {
		t.Fatal("a failed send alongside an in-app row should still mark the pair")
	}
}

// TestDispatchDeparture_EmailOnlyFailureRetries: with email as the only
// channel a failed send leaves the pair due, and a later success marks it.
func TestDispatchDeparture_EmailOnlyFailureRetries(t *testing.T) {
	p, s, _, _, owner, _ := departureSetup(t)
	ctx := context.Background()
	prefs, err := s.AlertPrefsFor(ctx, owner)
	if err != nil {
		t.Fatalf("AlertPrefsFor: %v", err)
	}
	prefs.InApp = false
	if err := s.SetAlertPrefs(ctx, *prefs); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
	now := depOut.Add(-store.DepartureSummaryLead)
	p.SendAlertEmail = func(context.Context, string, string, string) error {
		return errors.New("sendmail boom")
	}

	p.remindDepartures(ctx, now)
	if due, _ := s.DueDepartureSummaries(ctx, now); len(due) != 1 {
		t.Fatalf("an email-only failure must stay due; %d due", len(due))
	}

	var sent int
	p.SendAlertEmail = func(context.Context, string, string, string) error { sent++; return nil }
	p.remindDepartures(ctx, now)
	if sent != 1 {
		t.Fatalf("retry should have sent once, got %d", sent)
	}
	if due, _ := s.DueDepartureSummaries(ctx, now); len(due) != 0 {
		t.Fatalf("a successful retry must mark the pair; %d still due", len(due))
	}
	if alerts, _ := s.ListFlightAlerts(ctx, owner, 10); len(alerts) != 0 {
		t.Fatalf("in-app row written with the channel off: %d", len(alerts))
	}
}

// TestDispatchDeparture_DefaultMailerSend covers the nil SendAlertEmail
// fallback to mailer.Send, accepted by a no-op sendmail.
func TestDispatchDeparture_DefaultMailerSend(t *testing.T) {
	p, s, _, _, owner, partID := departureSetup(t)
	p.SendAlertEmail = nil

	p.remindDepartures(context.Background(), depOut.Add(-store.DepartureSummaryLead))

	if !departureSent(t, s, partID, owner) {
		t.Fatal("the default mailer path should deliver and mark")
	}
}

// TestPublishDeparture_MarshalErrorStillDurable: the inbox row is written
// before the SSE marshal, so a marshal failure still counts as delivered.
func TestPublishDeparture_MarshalErrorStillDurable(t *testing.T) {
	p, s, _, _, owner, partID := departureSetup(t)
	failMarshal(t)

	p.remindDepartures(context.Background(), depOut.Add(-store.DepartureSummaryLead))

	if alerts, _ := s.ListFlightAlerts(context.Background(), owner, 10); len(alerts) != 1 {
		t.Fatalf("expected the inbox row to persist, got %d", len(alerts))
	}
	if !departureSent(t, s, partID, owner) {
		t.Fatal("a marshal hiccup must not block the mark")
	}
}

// TestPushDeparture_Delivered: title is the subject, body the headline plus
// the arrival-day weather line, deep-linked to the trip.
func TestPushDeparture_Delivered(t *testing.T) {
	p, _, _, _, owner, _ := departureSetup(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp

	p.remindDepartures(context.Background(), depOut.Add(-store.DepartureSummaryLead))

	if fp.count() != 1 {
		t.Fatalf("want 1 push, got %d", fp.count())
	}
	got := fp.payloads[0]
	if got.Title != "Departure summary: BA295 to Chicago" {
		t.Errorf("title = %q", got.Title)
	}
	if !strings.HasPrefix(got.Body, "BA295 (LHR → ORD) is on time, departing at") ||
		!strings.HasSuffix(got.Body, " Chicago: 18°C / 10°C, rain likely.") {
		t.Errorf("body = %q", got.Body)
	}
	if got.Kind != "departure" || got.Tag != "departure-BA295" || !strings.HasPrefix(got.URL, "/trips/") {
		t.Errorf("payload = %+v", got)
	}
	if len(fp.users) != 1 || len(fp.users[0]) != 1 || fp.users[0][0] != owner {
		t.Errorf("recipients = %v, want [%d]", fp.users, owner)
	}
}

// TestPushDeparture_NoWeatherBodyIsHeadline: without a forecast the body is
// the headline alone.
func TestPushDeparture_NoWeatherBodyIsHeadline(t *testing.T) {
	p, _, _, _, _, _ := departureSetup(t)
	p.Weather = nil
	fp := &fakePusher{enabled: true}
	p.Push = fp

	p.remindDepartures(context.Background(), depOut.Add(-store.DepartureSummaryLead))

	if fp.count() != 1 || !strings.HasSuffix(fp.payloads[0].Body, "BST.") {
		t.Fatalf("payloads = %+v", fp.payloads)
	}
}

// TestPushDeparture_KindDisabled: the departure push kind off suppresses only
// the push.
func TestPushDeparture_KindDisabled(t *testing.T) {
	p, s, _, _, owner, _ := departureSetup(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	ctx := context.Background()
	if err := s.SetPushKindPref(ctx, owner, "departure", false); err != nil {
		t.Fatalf("SetPushKindPref: %v", err)
	}

	p.remindDepartures(ctx, depOut.Add(-store.DepartureSummaryLead))

	if fp.count() != 0 {
		t.Fatalf("pushed with the kind off: %d", fp.count())
	}
	if alerts, _ := s.ListFlightAlerts(ctx, owner, 10); len(alerts) != 1 {
		t.Fatalf("the in-app channel should be unaffected, got %d", len(alerts))
	}
}

// TestPushDeparture_DisabledSenderIsNoOp: a sender reporting itself disabled
// pushes nothing and the pair is still marked.
func TestPushDeparture_DisabledSenderIsNoOp(t *testing.T) {
	p, s, _, _, owner, partID := departureSetup(t)
	fp := &fakePusher{enabled: false}
	p.Push = fp

	p.remindDepartures(context.Background(), depOut.Add(-store.DepartureSummaryLead))

	if fp.count() != 0 {
		t.Fatalf("a disabled sender pushed %d times", fp.count())
	}
	if !departureSent(t, s, partID, owner) {
		t.Fatal("the summary should still be marked")
	}
}

// TestPushDeparture_MarkFailureSkipsPush: a failed mark leaves the pair due
// for a full re-dispatch, so pushing now would duplicate it.
func TestPushDeparture_MarkFailureSkipsPush(t *testing.T) {
	p, s, _, _, _, _ := departureSetup(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	now := depOut.Add(-store.DepartureSummaryLead)
	blockInserts(t, s, "flight_departure_summary_sent")

	p.remindDepartures(context.Background(), now)

	if fp.count() != 0 {
		t.Fatalf("pushed despite a failed mark: %d", fp.count())
	}
	if due, _ := s.DueDepartureSummaries(context.Background(), now); len(due) != 1 {
		t.Fatalf("a failed mark should leave the pair due, got %d", len(due))
	}
}

// TestEffectiveTimes: actual beats estimated beats the timetable, and an
// arrival with nothing known at all is nil.
func TestEffectiveTimes(t *testing.T) {
	sched := depOut
	est := sched.Add(20 * time.Minute)
	act := sched.Add(25 * time.Minute)
	in := sched.Add(9 * time.Hour)
	estIn := in.Add(15 * time.Minute)
	actIn := in.Add(10 * time.Minute)

	for _, c := range []struct {
		name        string
		f           store.Flight
		wantDep     time.Time
		wantArr     *time.Time
		wantArrNone bool
	}{
		{"timetable", store.Flight{ScheduledOut: sched, ScheduledIn: in}, sched, &in, false},
		{"estimated", store.Flight{ScheduledOut: sched, EstimatedOut: &est, ScheduledIn: in, EstimatedIn: &estIn}, est, &estIn, false},
		{"actual", store.Flight{ScheduledOut: sched, EstimatedOut: &est, ActualOut: &act,
			ScheduledIn: in, EstimatedIn: &estIn, ActualIn: &actIn}, act, &actIn, false},
		{"no arrival", store.Flight{ScheduledOut: sched}, sched, nil, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dep, arr := effectiveTimes(&c.f)
			if !dep.Equal(c.wantDep) {
				t.Errorf("departs = %v, want %v", dep, c.wantDep)
			}
			switch {
			case c.wantArrNone && arr != nil:
				t.Errorf("arrives = %v, want nil", *arr)
			case !c.wantArrNone && (arr == nil || !arr.Equal(*c.wantArr)):
				t.Errorf("arrives = %v, want %v", arr, *c.wantArr)
			}
		})
	}
}
