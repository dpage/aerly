package poller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/push"
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

// TestCheckinRoute: the route line is omitted when either end is unknown, so a
// flight with half a route reads cleanly rather than trailing an arrow.
func TestCheckinRoute(t *testing.T) {
	if got := checkinRoute(store.DueCheckin{OriginIATA: "LHR", DestIATA: "LIS"}); got != "LHR → LIS" {
		t.Errorf("route = %q", got)
	}
	if got := checkinRoute(store.DueCheckin{OriginIATA: "LHR"}); got != "" {
		t.Errorf("half a route should render nothing, got %q", got)
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

// checkinPushSetup wires a check-in poller with a fake pusher and an opted-in
// traveller holding one upcoming flight, returning what the push tests need.
func checkinPushSetup(t *testing.T) (*Poller, *store.Store, *fakePusher, int64, time.Time) {
	t.Helper()
	p, s, _, _ := alertPoller(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	owner := seedUser(t, s)
	optInToCheckins(t, s, owner)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)
	return p, s, fp, owner, out
}

// TestPushCheckin_DeliveredWhenKindEnabled: with push configured and the kind
// left at its default (on), the reminder reaches the traveller's devices.
func TestPushCheckin_DeliveredWhenKindEnabled(t *testing.T) {
	p, _, fp, owner, out := checkinPushSetup(t)

	p.remindCheckins(context.Background(), out.Add(-store.CheckinLead))

	if fp.count() != 1 {
		t.Fatalf("want 1 push, got %d", fp.count())
	}
	got := fp.payloads[0]
	if got.Kind != "checkin" {
		t.Errorf("kind = %q, want checkin", got.Kind)
	}
	if got.Title != "Check-in opens soon: BA286" {
		t.Errorf("title = %q", got.Title)
	}
	if !strings.Contains(got.Body, "opens in five minutes") || !strings.Contains(got.Body, "LHR → LIS") {
		t.Errorf("body = %q", got.Body)
	}
	if got.Tag != "checkin-BA286" {
		t.Errorf("tag = %q, want checkin-BA286", got.Tag)
	}
	if len(fp.users) != 1 || len(fp.users[0]) != 1 || fp.users[0][0] != owner {
		t.Errorf("recipients = %v, want [%d]", fp.users, owner)
	}
}

// TestPushCheckin_SkippedWhenKindDisabled: a traveller who wants check-in
// reminders but not as a push gets the other channels and no push.
func TestPushCheckin_SkippedWhenKindDisabled(t *testing.T) {
	p, s, fp, owner, out := checkinPushSetup(t)
	ctx := context.Background()
	if err := s.SetPushKindPref(ctx, owner, "checkin", false); err != nil {
		t.Fatalf("SetPushKindPref: %v", err)
	}

	p.remindCheckins(ctx, out.Add(-store.CheckinLead))

	if fp.count() != 0 {
		t.Fatalf("pushed with the kind off: %d", fp.count())
	}
	alerts, _ := s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 1 {
		t.Fatalf("the in-app channel should be unaffected, got %d", len(alerts))
	}
}

// TestPushCheckin_NoSenderIsNoOp: with no Sender wired, or one reporting itself
// disabled, the pass completes without pushing and still marks the pair sent.
func TestPushCheckin_NoSenderIsNoOp(t *testing.T) {
	for _, c := range []struct {
		name  string
		pushr pusher
	}{
		{"nil sender", nil},
		{"disabled sender", &fakePusher{enabled: false}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, s, _, _ := alertPoller(t)
			if c.pushr != nil {
				p.Push = c.pushr
			}
			ctx := context.Background()
			owner := seedUser(t, s)
			optInToCheckins(t, s, owner)
			out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
			seedCheckinFlight(t, s, owner, "BA286", out)
			now := out.Add(-store.CheckinLead)

			p.remindCheckins(ctx, now)

			if fp, ok := c.pushr.(*fakePusher); ok && fp.count() != 0 {
				t.Fatalf("a disabled sender pushed %d times", fp.count())
			}
			if due, _ := s.DueCheckins(ctx, now); len(due) != 0 {
				t.Fatalf("the reminder should still be marked sent; %d still due", len(due))
			}
		})
	}
}

// TestDispatchCheckin_EmailOnlyFailureRetries: a recipient whose only channel
// is email, and whose send failed, has had nothing at all, so the pair must
// stay unmarked and come round again on the next tick.
func TestDispatchCheckin_EmailOnlyFailureRetries(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	prefs, err := s.AlertPrefsFor(ctx, owner)
	if err != nil {
		t.Fatalf("AlertPrefsFor: %v", err)
	}
	prefs.Checkin, prefs.InApp = true, false // email is the only channel
	if err := s.SetAlertPrefs(ctx, *prefs); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)
	now := out.Add(-store.CheckinLead)
	p.SendAlertEmail = func(context.Context, string, string, string) error {
		return errors.New("sendmail boom")
	}

	p.remindCheckins(ctx, now)

	if due, _ := s.DueCheckins(ctx, now); len(due) != 1 {
		t.Fatalf("an email-only failure must stay due for retry; %d due", len(due))
	}

	// Once the send succeeds the pair is marked and stops recurring.
	var sent int
	p.SendAlertEmail = func(context.Context, string, string, string) error { sent++; return nil }
	p.remindCheckins(ctx, now)
	if sent != 1 {
		t.Fatalf("retry should have sent once, got %d", sent)
	}
	if due, _ := s.DueCheckins(ctx, now); len(due) != 0 {
		t.Fatalf("a successful retry must mark the pair sent; %d still due", len(due))
	}
}

// TestDispatchCheckin_NoChannelsStillMarked: a recipient with every channel off
// has nothing to retry, so the pair is marked rather than reconsidered forever.
func TestDispatchCheckin_NoChannelsStillMarked(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	prefs, err := s.AlertPrefsFor(ctx, owner)
	if err != nil {
		t.Fatalf("AlertPrefsFor: %v", err)
	}
	prefs.Checkin, prefs.InApp, prefs.Email = true, false, false
	if err := s.SetAlertPrefs(ctx, *prefs); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)
	now := out.Add(-store.CheckinLead)

	p.remindCheckins(ctx, now)

	if due, _ := s.DueCheckins(ctx, now); len(due) != 0 {
		t.Fatalf("nothing to retry, so the pair should be marked; %d still due", len(due))
	}
}

// blockingPusher stalls in Send until released, standing in for a push service
// that accepts the connection and then sits on it.
type blockingPusher struct {
	release chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (b *blockingPusher) Enabled() bool { return true }

func (b *blockingPusher) Send(context.Context, []int64, push.Payload) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
}

// TestDispatchCheckin_SlowPushDoesNotDelayMark: the push is a best-effort echo
// of a reminder already delivered, so it must not sit in front of the mark. If
// it did, a stalled push service would hold the pair open and a restart in that
// gap would re-send the in-app and email reminders the traveller already had.
func TestDispatchCheckin_SlowPushDoesNotDelayMark(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	optInToCheckins(t, s, owner)
	bp := &blockingPusher{release: make(chan struct{}), entered: make(chan struct{})}
	p.Push = bp
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)
	now := out.Add(-store.CheckinLead)

	done := make(chan struct{})
	go func() {
		p.remindCheckins(ctx, now)
		close(done)
	}()

	// Once the push has been entered, the mark must already be durable.
	select {
	case <-bp.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the push was never attempted")
	}
	var sent bool
	if err := s.Pool().QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM flight_checkin_sent WHERE user_id = $1)`, owner).Scan(&sent); err != nil {
		t.Fatalf("sent lookup: %v", err)
	}
	if !sent {
		t.Fatal("a stalled push delayed marking the reminder sent")
	}

	close(bp.release)
	<-done
}

// TestPushCheckin_MarkFailureSkipsPush: as with plan reminders, a failed
// sent-marker write leaves the pair due for a full re-dispatch, so pushing here
// would duplicate the notification.
func TestPushCheckin_MarkFailureSkipsPush(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	fp := &fakePusher{enabled: true}
	p.Push = fp
	ctx := context.Background()
	owner := seedUser(t, s)
	optInToCheckins(t, s, owner)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlight(t, s, owner, "BA286", out)
	now := out.Add(-store.CheckinLead)
	blockInserts(t, s, "flight_checkin_sent")

	p.remindCheckins(ctx, now)

	if fp.count() != 0 {
		t.Fatalf("pushed despite a failed mark: %d", fp.count())
	}
	if due, _ := s.DueCheckins(ctx, now); len(due) != 1 {
		t.Fatalf("a failed mark should leave the pair due, got %d", len(due))
	}
}
