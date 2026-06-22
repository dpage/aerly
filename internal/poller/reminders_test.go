package poller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// seedReminderPlan inserts a trip (owned by uid) + a plan of planType + one
// part starting at startsAt, returning the part id. Used by the reminder tests
// (mkPart only seeds flights).
func seedReminderPlan(t *testing.T, s *store.Store, uid int64, planType string, startsAt time.Time) (tripID, partID int64) {
	t.Helper()
	ctx := context.Background()
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('t', $1) RETURNING id`, uid).Scan(&tripID); err != nil {
		t.Fatalf("trip: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, tripID, uid); err != nil {
		t.Fatalf("member: %v", err)
	}
	var planID int64
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, title, created_by) VALUES ($1, $2, 'Hilton Vienna', $3) RETURNING id`,
		tripID, planType, uid).Scan(&planID); err != nil {
		t.Fatalf("plan: %v", err)
	}
	if err := s.Pool().QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, status) VALUES ($1, $2, 'planned') RETURNING id`,
		planID, startsAt).Scan(&partID); err != nil {
		t.Fatalf("part: %v", err)
	}
	return tripID, partID
}

func TestRemindUpcoming_EmailAndInApp(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	tripID, partID := seedReminderPlan(t, s, owner, "hotel", now.Add(2*time.Hour))
	if err := s.SetTripReminder(ctx, tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}

	p.remindUpcoming(ctx, now)

	if cap.count() != 1 {
		t.Fatalf("want 1 reminder email, got %d", cap.count())
	}
	if !strings.Contains(cap.sent[0], "Upcoming: Hilton Vienna") {
		t.Fatalf("email subject wrong:\n%s", cap.sent[0])
	}
	alerts, err := s.ListFlightAlerts(ctx, owner, 10)
	if err != nil {
		t.Fatalf("ListFlightAlerts: %v", err)
	}
	if len(alerts) != 1 || alerts[0].Kind != "reminder" || alerts[0].PlanPartID != partID {
		t.Fatalf("want 1 in-app reminder for part %d, got %+v", partID, alerts)
	}

	// Second tick must not re-send (marked sent).
	cap.sent = nil
	p.remindUpcoming(ctx, now)
	if cap.count() != 0 {
		t.Fatalf("re-sent on second tick: %d", cap.count())
	}
	alerts, _ = s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 1 {
		t.Fatalf("in-app reminder duplicated on second tick: %d", len(alerts))
	}
}

// TestRemindUpcoming_DueRemindersErrorReturns covers the list-failed branch: a
// cancelled context makes DueReminders error, so remindUpcoming logs and returns
// without dispatching.
func TestRemindUpcoming_DueRemindersErrorReturns(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	owner := seedUser(t, s)
	now := time.Now()
	tripID, _ := seedReminderPlan(t, s, owner, "hotel", now.Add(2*time.Hour))
	if err := s.SetTripReminder(context.Background(), tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // DueReminders runs on a cancelled ctx → query error → return

	p.remindUpcoming(ctx, now)

	if cap.count() != 0 {
		t.Fatalf("no reminder should be sent when the due-list query fails, got %d", cap.count())
	}
}

// TestRemindUpcoming_HiddenPlanNotLeaked covers the visibility filter: a user
// with a trip-level reminder opt-in who is NOT in the plan's visible set must
// not receive a reminder (a trip-level opt-in must not leak a plan hidden from
// that viewer). We seed an opt-in row directly for an unrelated user.
func TestRemindUpcoming_HiddenPlanNotLeaked(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	stranger := seedUser(t, s) // not a friend, not a member → plan not visible
	if err := s.UpsertVerifiedEmail(ctx, stranger, "stranger@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	tripID, _ := seedReminderPlan(t, s, owner, "hotel", now.Add(2*time.Hour))
	// Give the stranger a trip-level opt-in directly: DueReminders will surface
	// the (plan, stranger) pair, but VisiblePlanUserIDs won't include them.
	if err := s.SetTripReminder(ctx, tripID, stranger, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}

	p.remindUpcoming(ctx, now)

	if cap.count() != 0 {
		t.Fatalf("a reminder leaked to a viewer who can't see the plan, got %d", cap.count())
	}
	alerts, _ := s.ListFlightAlerts(ctx, stranger, 10)
	if len(alerts) != 0 {
		t.Fatalf("an in-app reminder leaked to a non-visible viewer: %+v", alerts)
	}
}

// TestDispatchReminder_PublishErrorSkipsMark covers the publishReminder failure
// path in dispatchReminder (and the matching error return inside
// publishReminder): a cancelled context fails the in-app insert, so we bail
// before marking sent, leaving the reminder to retry next tick.
func TestDispatchReminder_PublishErrorSkipsMark(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	owner := seedUser(t, s)
	now := time.Now()
	tripID, partID := seedReminderPlan(t, s, owner, "hotel", now.Add(2*time.Hour))
	if err := s.SetTripReminder(context.Background(), tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}
	due, err := s.DueReminders(context.Background(), now)
	if err != nil || len(due) != 1 {
		t.Fatalf("DueReminders = %d, %v", len(due), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // InsertFlightAlert (in publishReminder) errors → dispatchReminder returns

	p.dispatchReminder(ctx, due[0])

	if cap.count() != 0 {
		t.Fatalf("email should not be sent when the in-app insert fails, got %d", cap.count())
	}
	// Not marked sent: a fresh (uncancelled) pass still sees it as due.
	stillDue, err := s.DueReminders(context.Background(), now)
	_ = partID
	if err != nil || len(stillDue) != 1 {
		t.Fatalf("reminder should remain due after a failed dispatch: %d, %v", len(stillDue), err)
	}
}

// TestDispatchReminder_DefaultMailerSend covers the send==nil fall-back to
// mailer.Send: with an address on file and a no-op sendmail, the email channel
// runs end-to-end through the real mailer rather than a captured stub.
func TestDispatchReminder_DefaultMailerSend(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.MailFromAddress = "alerts@aerly.test"
	p.SendmailPath = "/bin/true" // no-op sendmail: accepts, exits 0
	p.PublicURL = "http://localhost:8080"
	// SendAlertEmail left nil → dispatchReminder defaults to mailer.Send.
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	tripID, _ := seedReminderPlan(t, s, owner, "hotel", now.Add(2*time.Hour))
	if err := s.SetTripReminder(ctx, tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}

	p.remindUpcoming(ctx, now) // must run the default mailer.Send path cleanly

	alerts, _ := s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 1 || alerts[0].Kind != "reminder" {
		t.Fatalf("expected one in-app reminder alongside the default-mailer email, got %+v", alerts)
	}
}

// TestDispatchReminder_SendErrorIsLogged covers the email send-error branch: a
// failing SendAlertEmail is logged and swallowed, and the reminder is still
// marked sent (a flaky sendmail must not wedge the reminder forever).
func TestDispatchReminder_SendErrorIsLogged(t *testing.T) {
	p, s, _ := newPoller(t, &mockTracker{}, time.Minute)
	p.MailFromAddress = "alerts@aerly.test"
	p.SendmailPath = "/bin/true"
	p.PublicURL = "http://localhost:8080"
	p.SendAlertEmail = func(context.Context, string, string, string) error {
		return errors.New("sendmail pipe broke")
	}
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	tripID, _ := seedReminderPlan(t, s, owner, "hotel", now.Add(2*time.Hour))
	if err := s.SetTripReminder(ctx, tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}

	p.remindUpcoming(ctx, now)

	// Despite the send error, the in-app row exists and the reminder is marked
	// sent: a second pass does not re-fire.
	alerts, _ := s.ListFlightAlerts(ctx, owner, 10)
	if len(alerts) != 1 {
		t.Fatalf("expected the in-app reminder to persist despite the send error, got %d", len(alerts))
	}
	stillDue, _ := s.DueReminders(ctx, now)
	if len(stillDue) != 0 {
		t.Fatalf("a send error must still mark the reminder sent; %d still due", len(stillDue))
	}
}

// TestPublishReminder_MarshalErrorStillDurable covers the marshal-failure branch
// in publishReminder: the in-app row is inserted before the marshal, so a
// marshal hiccup is logged but swallowed (returns nil) and the reminder is still
// treated as delivered — the row is durable, and MarkReminderSent runs.
func TestPublishReminder_MarshalErrorStillDurable(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	now := time.Now()
	tripID, partID := seedReminderPlan(t, s, owner, "hotel", now.Add(2*time.Hour))
	if err := s.SetTripReminder(ctx, tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}

	failMarshal(t)
	p.remindUpcoming(ctx, now)

	// The in-app row persisted despite the marshal failure.
	var n int
	if err := s.Pool().QueryRow(ctx,
		`SELECT COUNT(*) FROM flight_alerts WHERE plan_part_id = $1 AND kind = 'reminder'`,
		partID).Scan(&n); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the in-app reminder row to persist despite the marshal error, got %d", n)
	}
	// And the reminder was marked sent (no email harness assertion here; cap is
	// unused for the in-app-only durability check).
	_ = cap
	stillDue, _ := s.DueReminders(ctx, now)
	if len(stillDue) != 0 {
		t.Fatalf("a marshal hiccup must not block mark-sent; %d still due", len(stillDue))
	}
}

func TestRemindUpcoming_OutsideWindowNoSend(t *testing.T) {
	p, s, _, cap := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	if err := s.UpsertVerifiedEmail(ctx, owner, "owner@aerly.test"); err != nil {
		t.Fatalf("verify email: %v", err)
	}
	now := time.Now()
	// Part 40h out, lead 24h → outside the window.
	tripID, _ := seedReminderPlan(t, s, owner, "hotel", now.Add(40*time.Hour))
	if err := s.SetTripReminder(ctx, tripID, owner, 24); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}

	p.remindUpcoming(ctx, now)

	if cap.count() != 0 {
		t.Fatalf("reminder sent outside lead window: %d", cap.count())
	}
}
