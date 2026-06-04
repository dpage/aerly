package poller

import (
	"context"
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
