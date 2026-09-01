package store

import (
	"context"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/testsupport"
)

// seedFlightPart inserts a flight plan in tripID with one part departing at
// out, plus its flight_details row, and returns (planID, partID).
func seedCheckinFlightPart(t *testing.T, s *Store, tripID, createdBy int64, ident string, out time.Time) (planID, partID int64) {
	t.Helper()
	ctx := context.Background()
	planID, partID = seedPlanWithPart(t, s, tripID, createdBy, "flight", out)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in,
			origin_iata, dest_iata, flight_status)
		VALUES ($1, $2, $3, $4, 'LHR', 'LIS', 'Scheduled')`,
		partID, ident, out, out.Add(3*time.Hour)); err != nil {
		t.Fatalf("seed flight_details: %v", err)
	}
	return planID, partID
}

// enableCheckins turns a user's check-in preference on.
func enableCheckins(t *testing.T, s *Store, uid int64) {
	t.Helper()
	if err := s.SetAlertPrefs(context.Background(), AlertPrefs{
		UserID: uid, InApp: true, Email: true, MinDelayMin: 15, Checkin: true,
	}); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
}

// TestDueCheckins_Window: a candidate appears only once `now` has reached the
// check-in lead point and only while the flight is still upcoming.
func TestDueCheckins_Window(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	enableCheckins(t, s, uid)
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	_, partID := seedCheckinFlightPart(t, s, tid, uid, "BA286", out)

	for _, c := range []struct {
		name string
		now  time.Time
		want int
	}{
		{"a minute early", out.Add(-CheckinLead - time.Minute), 0},
		{"exactly at the lead point", out.Add(-CheckinLead), 1},
		{"an hour before departure", out.Add(-time.Hour), 1},
		{"departed", out, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			due, err := s.DueCheckins(ctx, c.now)
			if err != nil {
				t.Fatalf("DueCheckins: %v", err)
			}
			if len(due) != c.want {
				t.Fatalf("got %d due, want %d: %+v", len(due), c.want, due)
			}
			if c.want == 1 {
				d := due[0]
				if d.PlanPartID != partID || d.UserID != uid || d.Ident != "BA286" {
					t.Fatalf("due row = %+v", d)
				}
				if d.OriginIATA != "LHR" || d.DestIATA != "LIS" {
					t.Fatalf("route = %s→%s", d.OriginIATA, d.DestIATA)
				}
				if !d.InApp || !d.Email {
					t.Fatalf("channel flags = %+v", d)
				}
			}
		})
	}
}

// TestDueCheckins_RequiresOptIn: the preference is what gates the whole thing,
// so a user without it (row absent or checkin FALSE) is never a candidate.
func TestDueCheckins_RequiresOptIn(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlightPart(t, s, tid, uid, "BA286", out)
	now := out.Add(-CheckinLead)

	// No alert_prefs row at all.
	if due, err := s.DueCheckins(ctx, now); err != nil || len(due) != 0 {
		t.Fatalf("no prefs row: %d due (err %v)", len(due), err)
	}
	// A row with checkin explicitly off.
	if err := s.SetAlertPrefs(ctx, AlertPrefs{UserID: uid, InApp: true, Email: true, MinDelayMin: 15}); err != nil {
		t.Fatalf("SetAlertPrefs: %v", err)
	}
	if due, err := s.DueCheckins(ctx, now); err != nil || len(due) != 0 {
		t.Fatalf("checkin off: %d due (err %v)", len(due), err)
	}
	// On.
	enableCheckins(t, s, uid)
	if due, err := s.DueCheckins(ctx, now); err != nil || len(due) != 1 {
		t.Fatalf("checkin on: %d due (err %v)", len(due), err)
	}
}

// TestDueCheckins_ExcludesDeadFlights: a cancelled, dismissed or diverted leg
// is not something to go and check in for.
func TestDueCheckins_ExcludesDeadFlights(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	enableCheckins(t, s, uid)
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	now := out.Add(-CheckinLead)

	_, cancelled := seedCheckinFlightPart(t, s, tid, uid, "BA1", out)
	if _, err := s.pool.Exec(ctx, `UPDATE plan_parts SET status = 'cancelled' WHERE id = $1`, cancelled); err != nil {
		t.Fatalf("cancel part: %v", err)
	}
	_, dismissed := seedCheckinFlightPart(t, s, tid, uid, "BA2", out)
	if _, err := s.pool.Exec(ctx, `UPDATE plan_parts SET dismissed_at = NOW() WHERE id = $1`, dismissed); err != nil {
		t.Fatalf("dismiss part: %v", err)
	}
	_, diverted := seedCheckinFlightPart(t, s, tid, uid, "BA3", out)
	if _, err := s.pool.Exec(ctx,
		`UPDATE flight_details SET flight_status = 'Cancelled' WHERE plan_part_id = $1`, diverted); err != nil {
		t.Fatalf("cancel flight: %v", err)
	}

	due, err := s.DueCheckins(ctx, now)
	if err != nil {
		t.Fatalf("DueCheckins: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("got %d due, want 0: %+v", len(due), due)
	}
}

// TestDueCheckins_RecipientSet: the plan's owner, its passengers and viewers
// who opted in to its alerts are all candidates; an unrelated user is not.
func TestDueCheckins_RecipientSet(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	owner := testsupport.InsertUser(t, s.pool, "owner", false, true)
	passenger := testsupport.InsertUser(t, s.pool, "passenger", false, true)
	viewer := testsupport.InsertUser(t, s.pool, "viewer", false, true)
	bystander := testsupport.InsertUser(t, s.pool, "bystander", false, true)
	for _, u := range []int64{owner, passenger, viewer, bystander} {
		enableCheckins(t, s, u)
	}
	tid := seedTrip(t, s, owner)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	planID, _ := seedCheckinFlightPart(t, s, tid, owner, "BA286", out)

	if _, err := s.pool.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id) VALUES ($1, $2)`, planID, passenger); err != nil {
		t.Fatalf("add passenger: %v", err)
	}
	if err := s.AddPlanAlertOptin(ctx, planID, viewer); err != nil {
		t.Fatalf("AddPlanAlertOptin: %v", err)
	}

	due, err := s.DueCheckins(ctx, out.Add(-CheckinLead))
	if err != nil {
		t.Fatalf("DueCheckins: %v", err)
	}
	got := map[int64]bool{}
	for _, d := range due {
		got[d.UserID] = true
	}
	for _, u := range []int64{owner, passenger, viewer} {
		if !got[u] {
			t.Errorf("user %d should be a candidate", u)
		}
	}
	if got[bystander] {
		t.Error("an unrelated user should not be a candidate")
	}
}

// TestDueCheckins_NonFlightPlansIgnored: check-in is a flight thing; a hotel
// part inside the same window must not produce a candidate.
func TestDueCheckins_NonFlightPlansIgnored(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	enableCheckins(t, s, uid)
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedPlanWithPart(t, s, tid, uid, "hotel", out)

	due, err := s.DueCheckins(ctx, out.Add(-CheckinLead))
	if err != nil {
		t.Fatalf("DueCheckins: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("got %d due for a hotel, want 0: %+v", len(due), due)
	}
}

// TestMarkCheckinSent_Dedupes: marking a pair removes it from the due set, and
// marking it twice is a no-op rather than an error.
func TestMarkCheckinSent_Dedupes(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	enableCheckins(t, s, uid)
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	_, partID := seedCheckinFlightPart(t, s, tid, uid, "BA286", out)
	now := out.Add(-CheckinLead)

	if due, _ := s.DueCheckins(ctx, now); len(due) != 1 {
		t.Fatalf("before marking: %d due, want 1", len(due))
	}
	if err := s.MarkCheckinSent(ctx, partID, uid); err != nil {
		t.Fatalf("MarkCheckinSent: %v", err)
	}
	if err := s.MarkCheckinSent(ctx, partID, uid); err != nil {
		t.Fatalf("MarkCheckinSent (again): %v", err)
	}
	if due, _ := s.DueCheckins(ctx, now); len(due) != 0 {
		t.Fatalf("after marking: %d due, want 0", len(due))
	}
}

// TestDueCheckins_EmailAddress: the newest verified address is folded in, and
// a user with none gets an empty string rather than being dropped (they can
// still receive the in-app reminder).
func TestDueCheckins_EmailAddress(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx := context.Background()
	uid := testsupport.InsertUser(t, s.pool, "alice", false, true)
	enableCheckins(t, s, uid)
	tid := seedTrip(t, s, uid)
	out := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	seedCheckinFlightPart(t, s, tid, uid, "BA286", out)
	now := out.Add(-CheckinLead)

	due, _ := s.DueCheckins(ctx, now)
	if len(due) != 1 || due[0].EmailAddr != "" {
		t.Fatalf("unverified user should carry no address: %+v", due)
	}
	if err := s.UpsertVerifiedEmail(ctx, uid, "alice@aerly.test"); err != nil {
		t.Fatalf("UpsertVerifiedEmail: %v", err)
	}
	due, _ = s.DueCheckins(ctx, now)
	if len(due) != 1 || due[0].EmailAddr != "alice@aerly.test" {
		t.Fatalf("verified address not folded in: %+v", due)
	}
}

// TestDueCheckins_QueryError: a cancelled context surfaces the query error
// rather than an empty, quietly-wrong candidate list.
func TestDueCheckins_QueryError(t *testing.T) {
	s := New(testsupport.NewPool(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.DueCheckins(ctx, time.Now()); err == nil {
		t.Fatal("want an error from a cancelled context")
	}
}
