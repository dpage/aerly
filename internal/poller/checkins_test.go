package poller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// seedCheckinFlight seeds a flight departing at out, owned by uid, and returns
// its plan_part id. The part deliberately stores no timezone, which is the
// usual shape for a provider-resolved flight.
func seedCheckinFlight(t *testing.T, s *store.Store, uid int64, ident string, out time.Time) int64 {
	t.Helper()
	f, err := mkPart(context.Background(), s, partSeed{
		Ident:        ident,
		ScheduledOut: out,
		ScheduledIn:  out.Add(3 * time.Hour),
		OriginIATA:   "LHR",
		DestIATA:     "LIS",
	}, uid)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}
	return f.ID
}

// optInToCheckins turns the user's check-in preference on, leaving the channel
// defaults (in-app + email) alone.
func optInToCheckins(t *testing.T, s *store.Store, uid int64) {
	t.Helper()
	prefs, err := s.AlertPrefsFor(context.Background(), uid)
	if err != nil {
		t.Fatalf("AlertPrefsFor: %v", err)
	}
	prefs.Checkin = true
	if err := s.SetAlertPrefs(context.Background(), *prefs); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
}

// TestRemindCheckins_EmailAndInApp is the issue #119 happy path: an opted-in
// traveller gets one reminder, five minutes before check-in opens, on both
// channels, and the pass never re-sends it.
func TestRemindCheckins_EmailAndInApp(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToCheckins(t, s, owner)
	// 09:00 UTC at Heathrow is 10:00 BST, so the reminder must say BST.
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	partID := seedCheckinFlight(t, s, owner, "BA286", out)

	p.remindCheckins(ctx, out.Add(-store.CheckinLead))

	if cap.count() != 1 {
		t.Fatalf("want 1 check-in email, got %d", cap.count())
	}
	for _, want := range []string{
		"Subject: Check-in opens soon: BA286",
		"Online check-in for Flight BA286 (LHR",
		"Mon 31 Aug, 10:00 BST",
	} {
		if !strings.Contains(cap.sent[0], want) {
			t.Errorf("email missing %q\n---\n%s", want, cap.sent[0])
		}
	}
	alerts, err := s.ListFlightAlerts(ctx, owner, 10)
	if err != nil {
		t.Fatalf("ListFlightAlerts: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Kind != "checkin" || alerts[0].PlanPartID != partID {
		t.Fatalf("want 1 in-app check-in reminder for part %d, got %+v", partID, alerts)
	}

	// Second tick must not re-send (marked sent).
	cap.sent = nil
	p.remindCheckins(ctx, out.Add(-23*time.Hour))
	if cap.count() != 0 {
		t.Fatalf("re-sent on a later tick: %d", cap.count())
	}
	alerts, _ = s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 1 {
		t.Fatalf("in-app reminder duplicated: %d", len(alerts))
	}
}

// TestRemindCheckins_OffByDefault: a user who has never touched the preference
// gets nothing, which is the whole point of defaulting it off.
func TestRemindCheckins_OffByDefault(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)

	p.remindCheckins(ctx, out.Add(-store.CheckinLead))

	if cap.count() != 0 {
		t.Fatalf("sent to a user who never opted in: %d", cap.count())
	}
	alerts, _ := s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 0 {
		t.Fatalf("in-app reminder for a user who never opted in: %d", len(alerts))
	}
}

// TestRemindCheckins_OutsideWindow: nothing fires before the lead point is
// reached, and nothing fires once the flight has departed.
func TestRemindCheckins_OutsideWindow(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToCheckins(t, s, owner)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)

	// A minute too early.
	p.remindCheckins(ctx, out.Add(-store.CheckinLead-time.Minute))
	if cap.count() != 0 {
		t.Fatalf("fired before the lead point: %d", cap.count())
	}
	// Already departed.
	p.remindCheckins(ctx, out.Add(time.Minute))
	if cap.count() != 0 {
		t.Fatalf("fired after departure: %d", cap.count())
	}
}

// TestRemindCheckins_ChannelsRespected: with the in-app channel off, only the
// email goes; the pair is still marked sent so it doesn't retry forever.
func TestRemindCheckins_ChannelsRespected(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	prefs, err := s.AlertPrefsFor(ctx, owner)
	if err != nil {
		t.Fatalf("AlertPrefsFor: %v", err)
	}
	prefs.Checkin, prefs.InApp = true, false
	if err := s.SetAlertPrefs(ctx, *prefs); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)

	p.remindCheckins(ctx, out.Add(-store.CheckinLead))

	if cap.count() != 1 {
		t.Fatalf("want 1 email, got %d", cap.count())
	}
	alerts, _ := s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 0 {
		t.Fatalf("in-app row written with the channel off: %d", len(alerts))
	}
	// Marked sent despite the in-app channel being off.
	cap.sent = nil
	p.remindCheckins(ctx, out.Add(-23*time.Hour))
	if cap.count() != 0 {
		t.Fatalf("re-sent after a successful email-only delivery: %d", cap.count())
	}
}

// TestRemindCheckins_HiddenPlanNotLeaked: an opt-in that outlived the viewer's
// access to the plan must not deliver, mirroring the reminder pass's guard.
func TestRemindCheckins_HiddenPlanNotLeaked(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	stranger := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, stranger, "stranger@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToCheckins(t, s, stranger)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	partID := seedCheckinFlight(t, s, owner, "BA286", out)

	// The stranger has an alert opt-in row but no visibility of the plan.
	var planID int64
	if err := s.Pool().QueryRow(ctx,
		`SELECT plan_id FROM plan_parts WHERE id = $1`, partID).Scan(&planID); err != nil {
		t.Fatalf("plan lookup: %v", err)
	}
	if err := s.AddPlanAlertOptin(ctx, planID, stranger); err != nil {
		t.Fatalf("AddPlanAlertOptin: %v", err)
	}

	p.remindCheckins(ctx, out.Add(-store.CheckinLead))

	if cap.count() != 0 {
		t.Fatalf("delivered to a user the plan is hidden from: %d", cap.count())
	}
	alerts, _ := s.ListFlightAlerts(ctx, stranger, 10)
	if len(alerts) != 0 {
		t.Fatalf("in-app row for a hidden plan: %d", len(alerts))
	}
}

// TestRemindCheckins_DueCheckinsErrorReturns: a failed query logs and returns
// rather than dispatching anything.
func TestRemindCheckins_DueCheckinsErrorReturns(t *testing.T) {
	p, _, _, cap := alertPoller(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // a cancelled context fails the query
	p.remindCheckins(ctx, time.Now())
	if cap.count() != 0 {
		t.Fatalf("dispatched on a failed query: %d", cap.count())
	}
}

// TestDispatchCheckin_PublishErrorSkipsMark: when the in-app insert fails we
// must not mark the pair sent, so the next tick retries it.
func TestDispatchCheckin_PublishErrorSkipsMark(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)

	// A part id that doesn't exist fails the insert's foreign key.
	d := store.DueCheckin{
		PlanPartID: 1 << 40, PlanID: 1 << 40, TripID: 1 << 40, UserID: owner,
		Ident: "BA286", StartsAt: out, InApp: true,
	}
	p.dispatchCheckin(ctx, d)

	if cap.count() != 0 {
		t.Fatalf("emailed despite a failed inbox insert: %d", cap.count())
	}
	var sent bool
	if err := s.Pool().QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM flight_checkin_sent WHERE user_id = $1)`, owner).Scan(&sent); err != nil {
		t.Fatalf("sent lookup: %v", err)
	}
	if sent {
		t.Fatal("marked sent despite a failed inbox insert")
	}
}

// TestDispatchCheckin_SendErrorIsLogged: a mailer failure must not stop the
// pair being marked sent, or a permanently bad address would retry every tick.
func TestDispatchCheckin_SendErrorIsLogged(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToCheckins(t, s, owner)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	partID := seedCheckinFlight(t, s, owner, "BA286", out)
	p.SendAlertEmail = func(context.Context, string, string, string) error {
		return errors.New("sendmail boom")
	}

	p.remindCheckins(ctx, out.Add(-store.CheckinLead))

	var sent bool
	if err := s.Pool().QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM flight_checkin_sent WHERE plan_part_id = $1 AND user_id = $2)`,
		partID, owner).Scan(&sent); err != nil {
		t.Fatalf("sent lookup: %v", err)
	}
	if !sent {
		t.Fatal("a failed send should still mark the pair sent")
	}
}

// TestCheckinRouteAndZone covers the two small rendering helpers: the route
// line is omitted when either end is unknown, and the zone falls back through
// the airport table to the part's coordinate.
func TestCheckinRouteAndZone(t *testing.T) {
	if got := checkinRoute(store.DueCheckin{OriginIATA: "LHR", DestIATA: "LIS"}); got != "LHR → LIS" {
		t.Errorf("route = %q", got)
	}
	if got := checkinRoute(store.DueCheckin{OriginIATA: "LHR"}); got != "" {
		t.Errorf("half a route should render nothing, got %q", got)
	}

	if got := checkinZone(store.DueCheckin{StartTZ: "America/Denver", OriginIATA: "LHR"}); got != "America/Denver" {
		t.Errorf("stored zone should win, got %q", got)
	}
	if got := checkinZone(store.DueCheckin{OriginIATA: "LHR"}); got != "Europe/London" {
		t.Errorf("airport-table zone = %q, want Europe/London", got)
	}
	lat, lon := 50.4406, -4.9954 // Newquay, deliberately off the embedded table
	if got := checkinZone(store.DueCheckin{OriginIATA: "NQY", StartLat: &lat, StartLon: &lon}); got != "Europe/London" {
		t.Errorf("coordinate zone = %q, want Europe/London", got)
	}
	if got := checkinZone(store.DueCheckin{}); got != "" {
		t.Errorf("nothing to resolve should give \"\", got %q", got)
	}
}

// TestDispatchCheckin_DefaultMailerSend covers the nil-SendAlertEmail branch:
// with no test seam installed the dispatch falls back to mailer.Send, which a
// no-op sendmail accepts.
func TestDispatchCheckin_DefaultMailerSend(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.MailFromAddress = "alerts@aerly.test"
	p.SendmailPath = "/bin/true" // no-op sendmail: accepts, exits 0
	p.PublicURL = "http://localhost:8080"
	// SendAlertEmail left nil → dispatchCheckin defaults to mailer.Send.
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToCheckins(t, s, owner)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)

	p.remindCheckins(ctx, out.Add(-store.CheckinLead))

	alerts, _ := s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 1 || alerts[0].Kind != "checkin" {
		t.Fatalf("expected one in-app reminder alongside the default-mailer email, got %+v", alerts)
	}
}

// TestPublishCheckin_MarshalErrorStillDurable: the inbox row is inserted before
// the SSE marshal, so a marshal hiccup is logged and swallowed and the reminder
// is still treated as delivered.
func TestPublishCheckin_MarshalErrorStillDurable(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	optInToCheckins(t, s, owner)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	partID := seedCheckinFlight(t, s, owner, "BA286", out)
	now := out.Add(-store.CheckinLead)

	failMarshal(t)
	p.remindCheckins(ctx, now)

	var n int
	if err := s.Pool().QueryRow(ctx,
		`SELECT COUNT(*) FROM flight_alerts WHERE plan_part_id = $1 AND kind = 'checkin'`,
		partID).Scan(&n); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the inbox row to persist despite the marshal error, got %d", n)
	}
	stillDue, _ := s.DueCheckins(ctx, now)
	if len(stillDue) != 0 {
		t.Fatalf("a marshal hiccup must not block mark-sent; %d still due", len(stillDue))
	}
}

// TestRemindCheckins_CancelledMidPass: a context cancelled between candidates
// stops the pass rather than pressing on.
func TestRemindCheckins_CancelledMidPass(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	base := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(base, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	optInToCheckins(t, s, owner)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)

	// The candidate list is fetched before the loop's cancellation check, so a
	// context cancelled after the query still stops the dispatch.
	ctx, cancel := context.WithCancel(base)
	due, err := s.DueCheckins(ctx, out.Add(-store.CheckinLead))
	if err != nil || len(due) != 1 {
		t.Fatalf("setup: %d due (err %v)", len(due), err)
	}
	cancel()
	p.remindCheckins(ctx, out.Add(-store.CheckinLead))
	if cap.count() != 0 {
		t.Fatalf("dispatched on a cancelled context: %d", cap.count())
	}
}
