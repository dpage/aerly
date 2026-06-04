package store

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/testsupport"
)

// seedTrip inserts a trip owned by createdBy and returns its id.
func seedTrip(t *testing.T, s *Store, createdBy int64) int64 {
	t.Helper()
	ctx := context.Background()
	var tripID int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('t', $1) RETURNING id`, createdBy).Scan(&tripID); err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, 'owner')`, tripID, createdBy); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return tripID
}

// seedPlanWithPart inserts a plan of planType in tripID with a single part
// starting at startsAt, and returns (planID, partID).
func seedPlanWithPart(t *testing.T, s *Store, tripID, createdBy int64, planType string, startsAt time.Time) (planID, partID int64) {
	t.Helper()
	ctx := context.Background()
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type, created_by) VALUES ($1, $2, $3) RETURNING id`,
		tripID, planType, createdBy).Scan(&planID); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, status) VALUES ($1, $2, 'planned') RETURNING id`,
		planID, startsAt).Scan(&partID); err != nil {
		t.Fatalf("seed part: %v", err)
	}
	return planID, partID
}

func TestTripReminder_UpsertAndRemove(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	tid := seedTrip(t, s, uid)

	got, err := s.TripReminderFor(ctx, tid, uid)
	if err != nil {
		t.Fatalf("TripReminderFor: %v", err)
	}
	if got.OptedIn {
		t.Fatalf("default OptedIn = true, want false")
	}
	if err := s.SetTripReminder(ctx, tid, uid, 12); err != nil {
		t.Fatalf("SetTripReminder: %v", err)
	}
	got, _ = s.TripReminderFor(ctx, tid, uid)
	if !got.OptedIn || got.LeadHours != 12 {
		t.Fatalf("after set = %+v, want {true 12}", got)
	}
	if err := s.RemoveTripReminder(ctx, tid, uid); err != nil {
		t.Fatalf("RemoveTripReminder: %v", err)
	}
	got, _ = s.TripReminderFor(ctx, tid, uid)
	if got.OptedIn {
		t.Fatalf("after remove OptedIn = true, want false")
	}
}

func TestPlanReminder_OverrideStates(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	tid := seedTrip(t, s, uid)
	pid, _ := seedPlanWithPart(t, s, tid, uid, "flight", time.Now().Add(time.Hour))

	got, _ := s.PlanReminderFor(ctx, pid, uid)
	if got.Override != "inherit" {
		t.Fatalf("default override = %q, want inherit", got.Override)
	}
	if err := s.SetPlanReminder(ctx, pid, uid, false, 24); err != nil {
		t.Fatalf("SetPlanReminder off: %v", err)
	}
	got, _ = s.PlanReminderFor(ctx, pid, uid)
	if got.Override != "off" {
		t.Fatalf("override = %q, want off", got.Override)
	}
	if err := s.SetPlanReminder(ctx, pid, uid, true, 6); err != nil {
		t.Fatalf("SetPlanReminder on: %v", err)
	}
	got, _ = s.PlanReminderFor(ctx, pid, uid)
	if got.Override != "on" || got.LeadHours != 6 {
		t.Fatalf("override = %+v, want {on 6}", got)
	}
	if err := s.RemovePlanReminder(ctx, pid, uid); err != nil {
		t.Fatalf("RemovePlanReminder: %v", err)
	}
	got, _ = s.PlanReminderFor(ctx, pid, uid)
	if got.Override != "inherit" {
		t.Fatalf("after remove override = %q, want inherit", got.Override)
	}
}

func TestDueReminders_Resolution(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	tid := seedTrip(t, s, uid)
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	// Plan A: part starts in 10h. Trip opt-in lead 24h → inside window → fires.
	planA, partA := seedPlanWithPart(t, s, tid, uid, "flight", now.Add(10*time.Hour))
	// Plan B: part starts in 40h. Trip lead 24h → outside window → no fire.
	planB, partB := seedPlanWithPart(t, s, tid, uid, "hotel", now.Add(40*time.Hour))

	if err := s.SetTripReminder(ctx, tid, uid, 24); err != nil {
		t.Fatal(err)
	}
	due, err := s.DueReminders(ctx, now)
	if err != nil {
		t.Fatalf("DueReminders: %v", err)
	}
	if !hasReminder(due, partA, uid) {
		t.Errorf("partA (10h, lead 24) not due")
	}
	if hasReminder(due, partB, uid) {
		t.Errorf("partB (40h, lead 24) due, want not due")
	}

	// Plan A off-override suppresses despite the trip opt-in.
	if err := s.SetPlanReminder(ctx, planA, uid, false, 24); err != nil {
		t.Fatal(err)
	}
	due, _ = s.DueReminders(ctx, now)
	if hasReminder(due, partA, uid) {
		t.Errorf("partA with off-override still due")
	}

	// Plan B on-override lead 48h pulls it into the window.
	if err := s.SetPlanReminder(ctx, planB, uid, true, 48); err != nil {
		t.Fatal(err)
	}
	due, _ = s.DueReminders(ctx, now)
	if !hasReminder(due, partB, uid) {
		t.Errorf("partB with on-override lead 48 not due")
	}

	// Once marked sent, partB stops firing.
	if err := s.MarkReminderSent(ctx, partB, uid); err != nil {
		t.Fatal(err)
	}
	due, _ = s.DueReminders(ctx, now)
	if hasReminder(due, partB, uid) {
		t.Errorf("partB still due after MarkReminderSent")
	}
}

func TestDueReminders_ExcludesCancelledAndStarted(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	tid := seedTrip(t, s, uid)
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)

	// Already started (1h ago) → never reminded.
	_, started := seedPlanWithPart(t, s, tid, uid, "flight", now.Add(-time.Hour))
	// Upcoming but cancelled → excluded.
	_, cancelled := seedPlanWithPart(t, s, tid, uid, "flight", now.Add(5*time.Hour))
	if _, err := s.pool.Exec(ctx, `UPDATE plan_parts SET status = 'cancelled' WHERE id = $1`, cancelled); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTripReminder(ctx, tid, uid, 24); err != nil {
		t.Fatal(err)
	}
	due, _ := s.DueReminders(ctx, now)
	if hasReminder(due, started, uid) {
		t.Errorf("already-started part is due, want excluded")
	}
	if hasReminder(due, cancelled, uid) {
		t.Errorf("cancelled part is due, want excluded")
	}
}

func hasReminder(due []DueReminder, partID, userID int64) bool {
	for _, d := range due {
		if d.PlanPartID == partID && d.UserID == userID {
			return true
		}
	}
	return false
}
